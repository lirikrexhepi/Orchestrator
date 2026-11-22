package antigravity

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"composer/internal/domain"
)

const (
	KeyringService       = "gemini"
	KeyringAccount       = "antigravity"
	ProfileAccountPrefix = "antigravity-profile-"
)

var (
	ErrNoActiveProfile = errors.New("no active antigravity credentials found in keyring")
	ErrProfileNotFound = errors.New("profile not found in keyring")
)

// Keyring abstracts system keychain/credential operations.
type Keyring interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// KeyringStore manages Antigravity credential snapshots under profile slots.
type KeyringStore struct {
	mu            sync.Mutex
	keyring       Keyring
	pendingSignIn string
}

func NewKeyringStore(k Keyring) *KeyringStore {
	return &KeyringStore{keyring: k}
}

var (
	DefaultKeyringStore = NewKeyringStore(newOSKeyring())
	// SpawnMutex serializes agy process initialization when using keyring profiles,
	// ensuring concurrent sessions do not race on the single gemini:antigravity keyring slot.
	SpawnMutex sync.Mutex
)

// SetKeyring overrides the backing keyring implementation (useful for tests).
func (s *KeyringStore) SetKeyring(k Keyring) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyring = k
}

// HasProfile checks if a saved profile exists for the given accountID.
func (s *KeyringStore) HasProfile(accountID string) bool {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return false
	}
	if accountID == domain.DefaultAccountID {
		sec, err := s.keyring.Get(KeyringService, KeyringAccount)
		return err == nil && sec != ""
	}
	sec, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+accountID)
	return err == nil && sec != ""
}

// CaptureProfile reads the active gemini:antigravity secret and saves a copy under gemini:antigravity-profile-<accountID>.
func (s *KeyringStore) CaptureProfile(accountID string) error {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return errors.New("keyring not available")
	}

	secret, err := s.keyring.Get(KeyringService, KeyringAccount)
	if err != nil || secret == "" {
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNoActiveProfile, err)
		}
		return ErrNoActiveProfile
	}

	// Always ensure the default profile copy is preserved if not yet saved
	if accountID != domain.DefaultAccountID {
		if _, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID); err != nil {
			_ = s.keyring.Set(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID, secret)
		}
	}

	return s.keyring.Set(KeyringService, ProfileAccountPrefix+accountID, secret)
}

// ActivateProfile writes the saved secret for accountID back into the active gemini:antigravity slot.
func (s *KeyringStore) ActivateProfile(accountID string) error {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return errors.New("keyring not available")
	}

	if accountID == domain.DefaultAccountID {
		// If saved default exists, restore it. If not, current slot is assumed default.
		secret, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID)
		if err == nil && secret != "" {
			return s.keyring.Set(KeyringService, KeyringAccount, secret)
		}
		return nil
	}

	// Preserve current as default if default profile doesn't exist yet
	if _, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID); err != nil {
		if cur, err := s.keyring.Get(KeyringService, KeyringAccount); err == nil && cur != "" {
			_ = s.keyring.Set(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID, cur)
		}
	}

	secret, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+accountID)
	if err != nil || secret == "" {
		return fmt.Errorf("%w: %q", ErrProfileNotFound, accountID)
	}

	return s.keyring.Set(KeyringService, KeyringAccount, secret)
}

// DeleteProfile deletes gemini:antigravity-profile-<accountID>.
func (s *KeyringStore) DeleteProfile(accountID string) error {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return nil
	}
	return s.keyring.Delete(KeyringService, ProfileAccountPrefix+accountID)
}

// decodeGoKeyringSecret decodes go-keyring's base64 or hex encoding prefixes.
func decodeGoKeyringSecret(raw string) string {
	const base64Prefix = "go-keyring-base64:"
	const encodedPrefix = "go-keyring-encoded:"
	if strings.HasPrefix(raw, base64Prefix) {
		payload := strings.TrimPrefix(raw, base64Prefix)
		if decoded, err := base64.StdEncoding.DecodeString(payload); err == nil {
			return string(decoded)
		}
		if decoded, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
			return string(decoded)
		}
		return raw
	}
	if strings.HasPrefix(raw, encodedPrefix) {
		payload := strings.TrimPrefix(raw, encodedPrefix)
		if decoded, err := hex.DecodeString(payload); err == nil {
			return string(decoded)
		}
		return raw
	}
	return raw
}

func extractJWTEmail(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(decoded, &claims) == nil && claims.Email != "" {
		return claims.Email
	}
	return ""
}

// ExtractEmail parses the OAuth credential JSON blob and returns the associated email if available.
func ExtractEmail(secret string) string {
	secret = decodeGoKeyringSecret(secret)
	var tokenData struct {
		IDTokenEmail string          `json:"id_token_email"`
		Email        string          `json:"email"`
		User         string          `json:"user"`
		IDToken      string          `json:"id_token"`
		Token        json.RawMessage `json:"token"`
	}
	if err := json.Unmarshal([]byte(secret), &tokenData); err != nil {
		return ""
	}
	if tokenData.IDTokenEmail != "" {
		return tokenData.IDTokenEmail
	}
	if tokenData.Email != "" {
		return tokenData.Email
	}
	if tokenData.User != "" {
		return tokenData.User
	}
	if len(tokenData.Token) > 0 && tokenData.Token[0] == '{' {
		var nested struct {
			IDTokenEmail string `json:"id_token_email"`
			Email        string `json:"email"`
			User         string `json:"user"`
			IDToken      string `json:"id_token"`
		}
		if json.Unmarshal(tokenData.Token, &nested) == nil {
			if nested.IDTokenEmail != "" {
				return nested.IDTokenEmail
			}
			if nested.Email != "" {
				return nested.Email
			}
			if nested.User != "" {
				return nested.User
			}
			if email := extractJWTEmail(nested.IDToken); email != "" {
				return email
			}
		}
	}
	if email := extractJWTEmail(tokenData.IDToken); email != "" {
		return email
	}
	return ""
}

// GetProfileSecret reads the stored secret for accountID without mutating the active keyring slot.
func (s *KeyringStore) GetProfileSecret(accountID string) (string, error) {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return "", errors.New("keyring not available")
	}

	var secret string
	var err error
	if accountID == domain.DefaultAccountID {
		secret, err = s.keyring.Get(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID)
		if err != nil || secret == "" {
			secret, err = s.keyring.Get(KeyringService, KeyringAccount)
		}
	} else {
		secret, err = s.keyring.Get(KeyringService, ProfileAccountPrefix+accountID)
	}
	if err != nil || secret == "" {
		return "", ErrProfileNotFound
	}
	return decodeGoKeyringSecret(secret), nil
}

// GetProfileEmail returns the email associated with the stored profile for accountID.
func (s *KeyringStore) GetProfileEmail(accountID string) (string, error) {
	secret, err := s.GetProfileSecret(accountID)
	if err != nil {
		return "", err
	}
	return ExtractEmail(secret), nil
}

// PrepareSignIn ensures existing credentials in the active slot are safely backed up to default,
// and clears the active slot (and any previous profile for accountID) so that agy prompts for fresh Google login.
func (s *KeyringStore) PrepareSignIn(accountID string) error {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return errors.New("keyring not available")
	}

	// Always ensure default has a snapshot before we clear the active slot
	if cur, err := s.keyring.Get(KeyringService, KeyringAccount); err == nil && cur != "" {
		if _, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID); err != nil {
			_ = s.keyring.Set(KeyringService, ProfileAccountPrefix+domain.DefaultAccountID, cur)
		}
	}

	// Delete active slot so agy starts unauthenticated
	_ = s.keyring.Delete(KeyringService, KeyringAccount)

	// Also delete previous profile for this account so stale credentials aren't kept
	if accountID != domain.DefaultAccountID {
		_ = s.keyring.Delete(KeyringService, ProfileAccountPrefix+accountID)
	}
	s.pendingSignIn = accountID

	return nil
}

// CheckActiveSecret checks if the active gemini:antigravity slot has been populated with credentials
// for the pending sign-in account. If populated, it snapshots the new secret into
// gemini:antigravity-profile-<accountID>, clears pendingSignIn, and returns the email.
func (s *KeyringStore) CheckActiveSecret(accountID string) (bool, string, error) {
	accountID = domain.NormalizeAccount(accountID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return false, "", errors.New("keyring not available")
	}
	if s.pendingSignIn != accountID {
		return false, "", nil
	}

	secret, err := s.keyring.Get(KeyringService, KeyringAccount)
	if err != nil || secret == "" {
		return false, "", nil
	}

	s.pendingSignIn = ""
	email := ExtractEmail(secret)
	targetProfile := ProfileAccountPrefix + accountID
	_ = s.keyring.Set(KeyringService, targetProfile, secret)
	return true, email, nil
}

// RestoreDefault restores the default profile into the active gemini:antigravity slot if available.
func (s *KeyringStore) RestoreDefault() error {
	accountID := domain.DefaultAccountID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyring == nil {
		return errors.New("keyring not available")
	}
	secret, err := s.keyring.Get(KeyringService, ProfileAccountPrefix+accountID)
	if err == nil && secret != "" {
		return s.keyring.Set(KeyringService, KeyringAccount, secret)
	}
	return nil
}
