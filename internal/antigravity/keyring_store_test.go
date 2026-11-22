package antigravity

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"composer/internal/domain"
)

type mockKeyring struct {
	mu    sync.Mutex
	items map[string]map[string]string
}

func newMockKeyring() *mockKeyring {
	return &mockKeyring{
		items: make(map[string]map[string]string),
	}
}

func (m *mockKeyring) Get(service, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if accs, ok := m.items[service]; ok {
		if sec, ok := accs[account]; ok {
			return sec, nil
		}
	}
	return "", errors.New("not found")
}

func (m *mockKeyring) Set(service, account, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[service]; !ok {
		m.items[service] = make(map[string]string)
	}
	m.items[service][account] = secret
	return nil
}

func (m *mockKeyring) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if accs, ok := m.items[service]; ok {
		delete(accs, account)
	}
	return nil
}

func TestKeyringStoreCaptureProfile(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, KeyringAccount, "default-active-token")

	store := NewKeyringStore(mock)

	// Capture "work" profile
	if err := store.CaptureProfile("work"); err != nil {
		t.Fatalf("CaptureProfile failed: %v", err)
	}

	// Verify work profile was saved
	workSec, err := mock.Get(KeyringService, ProfileAccountPrefix+"work")
	if err != nil || workSec != "default-active-token" {
		t.Fatalf("expected profile work to be 'default-active-token', got %q, err %v", workSec, err)
	}

	// Verify default profile was also preserved automatically
	defSec, err := mock.Get(KeyringService, ProfileAccountPrefix+"default")
	if err != nil || defSec != "default-active-token" {
		t.Fatalf("expected profile default to be preserved as 'default-active-token', got %q, err %v", defSec, err)
	}

	// Capture non-existent should fail
	emptyMock := newMockKeyring()
	emptyStore := NewKeyringStore(emptyMock)
	if err := emptyStore.CaptureProfile("work"); !errors.Is(err, ErrNoActiveProfile) {
		t.Fatalf("expected ErrNoActiveProfile, got %v", err)
	}
}

func TestKeyringStoreActivateProfile(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, KeyringAccount, "initial-token")
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"work", "work-secret-token")
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"personal", "personal-secret-token")

	store := NewKeyringStore(mock)

	// Activate work
	if err := store.ActivateProfile("work"); err != nil {
		t.Fatalf("ActivateProfile(work) failed: %v", err)
	}
	active, _ := mock.Get(KeyringService, KeyringAccount)
	if active != "work-secret-token" {
		t.Fatalf("expected active token 'work-secret-token', got %q", active)
	}

	// Verify default was saved from initial-token
	defToken, _ := mock.Get(KeyringService, ProfileAccountPrefix+"default")
	if defToken != "initial-token" {
		t.Fatalf("expected default profile 'initial-token', got %q", defToken)
	}

	// Activate personal
	if err := store.ActivateProfile("personal"); err != nil {
		t.Fatalf("ActivateProfile(personal) failed: %v", err)
	}
	active, _ = mock.Get(KeyringService, KeyringAccount)
	if active != "personal-secret-token" {
		t.Fatalf("expected active token 'personal-secret-token', got %q", active)
	}

	// Activate default
	if err := store.ActivateProfile(domain.DefaultAccountID); err != nil {
		t.Fatalf("ActivateProfile(default) failed: %v", err)
	}
	active, _ = mock.Get(KeyringService, KeyringAccount)
	if active != "initial-token" {
		t.Fatalf("expected active token to be restored to 'initial-token', got %q", active)
	}

	// Activate unknown profile should fail
	if err := store.ActivateProfile("non-existent"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("expected ErrProfileNotFound, got %v", err)
	}
}

func TestKeyringStoreDeleteProfile(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"work", "work-token")

	store := NewKeyringStore(mock)
	if !store.HasProfile("work") {
		t.Fatalf("expected HasProfile(work) = true")
	}

	if err := store.DeleteProfile("work"); err != nil {
		t.Fatalf("DeleteProfile failed: %v", err)
	}

	if store.HasProfile("work") {
		t.Fatalf("expected HasProfile(work) = false after delete")
	}
}

func TestSpawnMutexSerialization(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, KeyringAccount, "token-default")
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"acc-1", "token-acc-1")
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"acc-2", "token-acc-2")
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"acc-3", "token-acc-3")

	store := NewKeyringStore(mock)
	var activeSpawns atomic.Int32
	var maxConcurrent atomic.Int32

	var wg sync.WaitGroup
	accounts := []string{"acc-1", "acc-2", "acc-3", "acc-1", "acc-2"}

	for _, acc := range accounts {
		wg.Add(1)
		go func(accountID string) {
			defer wg.Done()

			SpawnMutex.Lock()
			current := activeSpawns.Add(1)
			for {
				prev := maxConcurrent.Load()
				if current <= prev || maxConcurrent.CompareAndSwap(prev, current) {
					break
				}
			}

			// Activate profile inside mutex
			if err := store.ActivateProfile(accountID); err != nil {
				t.Errorf("ActivateProfile failed: %v", err)
			}

			// Verify active token matches account
			active, _ := mock.Get(KeyringService, KeyringAccount)
			expected := fmt.Sprintf("token-%s", accountID)
			if active != expected {
				t.Errorf("active token = %q, want %q", active, expected)
			}

			// Simulate process init time
			time.Sleep(10 * time.Millisecond)

			activeSpawns.Add(-1)
			SpawnMutex.Unlock()
		}(acc)
	}

	wg.Wait()

	if max := maxConcurrent.Load(); max != 1 {
		t.Fatalf("expected max concurrent spawns holding mutex to be 1, got %d", max)
	}
}

func TestExtractEmail(t *testing.T) {
	cases := []struct {
		json     string
		expected string
	}{
		{`{"id_token_email":"user1@gmail.com","token":"xyz"}`, "user1@gmail.com"},
		{`{"email":"user2@google.com","token":"xyz"}`, "user2@google.com"},
		{`{"user":"user3@company.com"}`, "user3@company.com"},
		{`{"token":"xyz"}`, ""},
		{`not a json`, ""},
	}
	for _, c := range cases {
		got := ExtractEmail(c.json)
		if got != c.expected {
			t.Errorf("ExtractEmail(%q) = %q, want %q", c.json, got, c.expected)
		}
	}
}

func TestPrepareSignInAndCheckActiveSecret(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, KeyringAccount, `{"id_token_email":"default@gmail.com","token":"def"}`)

	store := NewKeyringStore(mock)

	// Prepare sign in for new account "work"
	if err := store.PrepareSignIn("work"); err != nil {
		t.Fatalf("PrepareSignIn failed: %v", err)
	}

	// Active slot should be cleared
	active, _ := mock.Get(KeyringService, KeyringAccount)
	if active != "" {
		t.Fatalf("expected active slot to be empty, got %q", active)
	}

	// Default profile should be backed up
	defSec, err := mock.Get(KeyringService, ProfileAccountPrefix+"default")
	if err != nil || !errors.Is(err, nil) && defSec == "" {
		t.Fatalf("expected default profile to be backed up, got %q", defSec)
	}
	if email := ExtractEmail(defSec); email != "default@gmail.com" {
		t.Fatalf("expected default email default@gmail.com, got %q", email)
	}

	// While user is logging in, CheckActiveSecret should return false
	captured, email, err := store.CheckActiveSecret("work")
	if err != nil || captured {
		t.Fatalf("expected not captured yet, got %v, %v, %v", captured, email, err)
	}

	// User completes sign in in agy, writing new credentials to active slot
	_ = mock.Set(KeyringService, KeyringAccount, `{"id_token_email":"work@gmail.com","token":"work-tok"}`)

	// Next check should capture
	captured, email, err = store.CheckActiveSecret("work")
	if err != nil || !captured || email != "work@gmail.com" {
		t.Fatalf("expected captured=true, email=work@gmail.com, got %v, %q, %v", captured, email, err)
	}

	// Check work profile in store
	workEmail, err := store.GetProfileEmail("work")
	if err != nil || workEmail != "work@gmail.com" {
		t.Fatalf("GetProfileEmail(work) = %q, err %v", workEmail, err)
	}

	// Default email should remain intact
	defaultEmail, err := store.GetProfileEmail("default")
	if err != nil || defaultEmail != "default@gmail.com" {
		t.Fatalf("GetProfileEmail(default) = %q, err %v", defaultEmail, err)
	}
}

func TestRestoreDefault(t *testing.T) {
	mock := newMockKeyring()
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"default", `{"id_token_email":"default@gmail.com"}`)

	store := NewKeyringStore(mock)
	// Active slot is empty
	if err := store.RestoreDefault(); err != nil {
		t.Fatalf("RestoreDefault failed: %v", err)
	}

	active, err := mock.Get(KeyringService, KeyringAccount)
	if err != nil || active != `{"id_token_email":"default@gmail.com"}` {
		t.Fatalf("expected active slot restored, got %q, err %v", active, err)
	}
}

func TestDecodeGoKeyringSecretAndJWTEmailAndPendingGuard(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"jwt@example.com"}`))
	jwt := "hdr." + payload + ".sig"
	rawJSON := fmt.Sprintf(`{"token":{"access_token":"tok-1","id_token":%q}}`, jwt)

	b64 := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(rawJSON))
	hx := "go-keyring-encoded:" + hex.EncodeToString([]byte(rawJSON))

	if got := decodeGoKeyringSecret(b64); got != rawJSON {
		t.Fatalf("decode base64 = %q, want %q", got, rawJSON)
	}
	if got := decodeGoKeyringSecret(hx); got != rawJSON {
		t.Fatalf("decode hex = %q, want %q", got, rawJSON)
	}
	if got := ExtractEmail(b64); got != "jwt@example.com" {
		t.Fatalf("ExtractEmail(base64 JWT) = %q, want jwt@example.com", got)
	}

	mock := newMockKeyring()
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"work", b64)
	_ = mock.Set(KeyringService, KeyringAccount, "active-unchanged")
	store := NewKeyringStore(mock)

	sec, err := store.GetProfileSecret("work")
	if err != nil || sec != rawJSON {
		t.Fatalf("GetProfileSecret(work) = %q, err %v", sec, err)
	}
	if active, _ := mock.Get(KeyringService, KeyringAccount); active != "active-unchanged" {
		t.Fatalf("GetProfileSecret mutated active slot to %q", active)
	}

	// Cross-account theft guard: CheckActiveSecret must NOT capture when pendingSignIn does not match.
	if err := store.PrepareSignIn("work"); err != nil {
		t.Fatal(err)
	}
	_ = mock.Set(KeyringService, KeyringAccount, rawJSON)
	if captured, _, _ := store.CheckActiveSecret("other"); captured {
		t.Fatal("CheckActiveSecret(other) stole credentials intended for work")
	}
	if captured, email, err := store.CheckActiveSecret("work"); err != nil || !captured || email != "jwt@example.com" {
		t.Fatalf("CheckActiveSecret(work) = %v, %q, %v", captured, email, err)
	}
	// Second check after capture must not re-capture since pendingSignIn was cleared.
	if captured, _, _ := store.CheckActiveSecret("work"); captured {
		t.Fatal("CheckActiveSecret(work) re-captured after pendingSignIn was cleared")
	}
}
