package antigravity

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"composer/internal/domain"
)

func withTestQuotaEnv(t *testing.T, home string, store *KeyringStore, dailyURL, prodURL, oauthURL string, now time.Time) {
	t.Helper()
	clearTokenCache()
	origDaily, origProd, origOAuth := dailyQuotaEndpoint, prodQuotaEndpoint, oauthTokenEndpoint
	origHome, origStore, origNow := userHomeDirFunc, quotaKeyringStore, nowFunc

	dailyQuotaEndpoint = dailyURL
	prodQuotaEndpoint = prodURL
	oauthTokenEndpoint = oauthURL
	userHomeDirFunc = func() (string, error) { return home, nil }
	quotaKeyringStore = store
	nowFunc = func() time.Time { return now }

	t.Cleanup(func() {
		clearTokenCache()
		dailyQuotaEndpoint = origDaily
		prodQuotaEndpoint = origProd
		oauthTokenEndpoint = origOAuth
		userHomeDirFunc = origHome
		quotaKeyringStore = origStore
		nowFunc = origNow
	})
}

func TestFetchQuotaSummaryMergeAndUntouchedResetSuppression(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	mock := newMockKeyring()
	rawTok := `{"token":{"access_token":"tok-kr-1","expiry":"2026-11-21T13:00:00Z"}}`
	encoded := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(rawTok))
	_ = mock.Set(KeyringService, ProfileAccountPrefix+"work", encoded)
	_ = mock.Set(KeyringService, KeyringAccount, "untouched-active-slot")
	store := NewKeyringStore(mock)

	expectedUA := "antigravity/hub/2.24.0 (aidev_client; os_type=" + runtime.GOOS + "; arch=" + runtime.GOARCH + "; cl=963137146)"

	dailySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:retrieveUserQuotaSummary" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok-kr-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("User-Agent") != expectedUA {
			t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), expectedUA)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"groups": []any{
				map[string]any{
					"displayName": "Gemini",
					"buckets": []any{
						// Untouched 5h bucket: remainingFraction == 1, no description, resetTime == now + 5h + 15s -> ResetsAt suppressed
						map[string]any{
							"bucketId":          "gemini-5h",
							"window":            "5h",
							"remainingFraction": 1.0,
							"resetTime":         now.Add(5*time.Hour + 15*time.Second).Format(time.RFC3339),
						},
						// Used weekly bucket -> keep ResetsAt
						map[string]any{
							"bucketId":          "gemini-weekly",
							"window":            "weekly",
							"remainingFraction": 0.25,
							"resetTime":         now.Add(48 * time.Hour).Format(time.RFC3339),
						},
						// Disabled bucket -> skipped
						map[string]any{
							"bucketId":          "gemini-disabled",
							"window":            "5h",
							"disabled":          true,
							"remainingFraction": 0.5,
						},
					},
				},
			},
		})
	}))
	defer dailySrv.Close()

	prodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:retrieveUserQuotaSummary" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"buckets": []any{
				// Duplicate gemini-weekly with different value -> daily (primary) must win
				map[string]any{
					"bucketId":          "gemini-weekly",
					"window":            "weekly",
					"remainingFraction": 0.99,
				},
				// Prod-only 3p bucket -> merged in
				map[string]any{
					"bucketId":          "3p-5h",
					"window":            "5h",
					"remainingFraction": 0.6,
					"resetTime":         now.Add(3 * time.Hour).Format(time.RFC3339),
				},
			},
		})
	}))
	defer prodSrv.Close()

	withTestQuotaEnv(t, t.TempDir(), store, dailySrv.URL, prodSrv.URL, "", now)

	limits, _, err := FetchQuota(context.Background(), "work", nil)
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}
	if active, _ := mock.Get(KeyringService, KeyringAccount); active != "untouched-active-slot" {
		t.Fatalf("FetchQuota mutated active keyring slot to %q", active)
	}
	if len(limits) != 3 {
		t.Fatalf("got %d limits, want 3: %+v", len(limits), limits)
	}

	byWin := make(map[string]domain.RateLimit)
	for _, l := range limits {
		byWin[l.Window] = l
	}
	g5 := byWin["Gemini · 5h"]
	if g5.UsedPercent == nil || *g5.UsedPercent != 0 || g5.ResetsAt != 0 {
		t.Fatalf("Gemini · 5h = %+v, want 0%% with suppressed ResetsAt=0", g5)
	}
	gw := byWin["Gemini · Weekly"]
	if gw.UsedPercent == nil || *gw.UsedPercent != 75 || gw.ResetsAt != now.Add(48*time.Hour).Unix() {
		t.Fatalf("Gemini · Weekly = %+v, want 75%% (from daily host) and ResetsAt", gw)
	}
	tp := byWin["Claude & GPT · 5h"]
	if tp.UsedPercent == nil || *tp.UsedPercent != 40 || tp.ResetsAt != now.Add(3*time.Hour).Unix() {
		t.Fatalf("Claude & GPT · 5h = %+v, want 40%%", tp)
	}
}

func TestFetchQuotaACPTokenRefreshAndProjectAndNoRedirect(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	acpDir := filepath.Join(home, ".gemini", "antigravity-acp")
	if err := os.MkdirAll(acpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	acpToken := `{"client_id":"cid-1","client_secret":"csec-1","refresh_token":"rtok-1","project_id":"proj-99"}`
	if err := os.WriteFile(filepath.Join(acpDir, "acp_token.json"), []byte(acpToken), 0o600); err != nil {
		t.Fatal(err)
	}

	var refreshCalls atomic.Int32
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") != "cid-1" || r.Form.Get("client_secret") != "csec-1" || r.Form.Get("refresh_token") != "rtok-1" || r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("unexpected oauth form: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-tok-1",
			"expires_in":   3600,
		})
	}))
	defer oauthSrv.Close()

	redirectTargetHit := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	dailySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1internal:retrieveUserQuotaSummary" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["project"] != "proj-99" {
				t.Errorf("expected project=proj-99 in request body, got %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"buckets": []any{
					map[string]any{"bucketId": "gemini-argon-5h", "window": "5h", "remainingFraction": 0.9},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer dailySrv.Close()

	// Prod server returns 302 redirect to verify Bearer token is never forwarded across redirects.
	prodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer prodSrv.Close()

	store := NewKeyringStore(newMockKeyring())
	withTestQuotaEnv(t, home, store, dailySrv.URL, prodSrv.URL, oauthSrv.URL, now)

	limits, _, err := FetchQuota(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}
	if redirectTargetHit {
		t.Fatal("HTTP client followed redirect and could leak Bearer token")
	}
	if len(limits) != 1 || limits[0].Window != "Gemini Argon · 5h" || *limits[0].UsedPercent != 10 {
		t.Fatalf("limits = %+v", limits)
	}
	// Second call within token expiry uses cached refreshed token.
	if _, _, err := FetchQuota(context.Background(), "default", nil); err != nil {
		t.Fatalf("second FetchQuota: %v", err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected cached token reuse (1 refresh), got %d", refreshCalls.Load())
	}
}

func TestFetchQuotaAvailableModelsFallbackAndExpiredAndNoCreds(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	acpDir := filepath.Join(home, ".gemini", "antigravity-acp")
	_ = os.MkdirAll(acpDir, 0o755)
	store := NewKeyringStore(newMockKeyring())

	// 1. No credentials at all -> ErrNoCredentials
	withTestQuotaEnv(t, home, store, "http://127.0.0.1:0", "http://127.0.0.1:0", "", now)
	if _, _, err := FetchQuota(context.Background(), "default", nil); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}

	// 2. Expired antigravity-oauth-token -> clear error
	expired := "go-keyring-encoded:" + hex.EncodeToString([]byte(`{"token":{"access_token":"old","expiry":"2026-11-21T12:00:30Z"}}`))
	_ = os.WriteFile(filepath.Join(acpDir, "antigravity-oauth-token"), []byte(expired), 0o600)
	if _, _, err := FetchQuota(context.Background(), "default", nil); err == nil || !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expected expired error, got %v", err)
	}

	// 3. Valid antigravity-oauth-token with empty summary -> fetchAvailableModels fallback
	valid := `{"access_token":"cli-tok-1","expiry":"2026-11-21T14:00:00Z"}`
	_ = os.WriteFile(filepath.Join(acpDir, "antigravity-oauth-token"), []byte(valid), 0o600)

	dailySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:retrieveUserQuotaSummary":
			_ = json.NewEncoder(w).Encode(map[string]any{"buckets": []any{}})
		case "/v1internal:fetchAvailableModels":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": map[string]any{
					"gemini-2.5-pro": map[string]any{
						"modelProvider": "MODEL_PROVIDER_GOOGLE",
						"quotaInfo":     map[string]any{"remainingFraction": 0.8},
					},
					"gemini-2.5-flash": map[string]any{
						"modelProvider":   "MODEL_PROVIDER_GOOGLE",
						"quotaInfo":       map[string]any{"remainingFraction": 0.4},
						"weeklyQuotaInfo": map[string]any{"remainingFraction": 0.5, "resetTime": now.Add(24 * time.Hour).Format(time.RFC3339)},
					},
					"claude-sonnet": map[string]any{
						"modelProvider": "MODEL_PROVIDER_ANTHROPIC",
						"quotaInfo":     map[string]any{"remainingFraction": 0.7},
					},
				},
			})
		}
	}))
	defer dailySrv.Close()

	prodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer prodSrv.Close()

	withTestQuotaEnv(t, home, store, dailySrv.URL, prodSrv.URL, "", now)

	limits, _, err := FetchQuota(context.Background(), "default", nil)
	if err != nil {
		t.Fatalf("FetchQuota fallback: %v", err)
	}
	byWin := make(map[string]domain.RateLimit)
	for _, l := range limits {
		byWin[l.Window] = l
	}
	if l := byWin["Gemini · Quota"]; l.UsedPercent == nil || *l.UsedPercent != 60 {
		t.Fatalf("Gemini · Quota = %+v, want max usedPercent=60", l)
	}
	if l := byWin["Gemini · Weekly"]; l.UsedPercent == nil || *l.UsedPercent != 50 {
		t.Fatalf("Gemini · Weekly = %+v, want 50", l)
	}
	if l := byWin["Claude · Quota"]; l.UsedPercent == nil || *l.UsedPercent != 30 {
		t.Fatalf("Claude · Quota = %+v, want 30", l)
	}

	// Named account without keyring secret must return ErrNoCredentials, not read default files.
	if _, _, err := FetchQuota(context.Background(), "other", nil); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("named account without keyring should return ErrNoCredentials, got %v", err)
	}
}

func TestQuotaSourceSnapshotOnUpdateAndInvalidate(t *testing.T) {
	var calls atomic.Int32
	src := NewQuotaSource("")
	src.fetch = func(ctx context.Context, client *http.Client) ([]domain.RateLimit, int64, error) {
		n := int(calls.Add(1))
		pct := n * 10
		return []domain.RateLimit{{Window: "Gemini · 5h", UsedPercent: &pct}}, int64(n * 1000), nil
	}

	var mu sync.Mutex
	updates := 0
	done := make(chan struct{}, 4)
	src.OnUpdate(func() {
		mu.Lock()
		updates++
		mu.Unlock()
		done <- struct{}{}
	})

	limits, _, err := src.Snapshot()
	if err != nil || len(limits) != 0 {
		t.Fatalf("initial non-blocking snapshot = %+v, err=%v", limits, err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first OnUpdate")
	}

	limits, fetchedAt, err := src.Snapshot()
	if err != nil || len(limits) != 1 || *limits[0].UsedPercent != 10 || fetchedAt != 1000 {
		t.Fatalf("cached snapshot = %+v, fetchedAt=%d, err=%v", limits, fetchedAt, err)
	}

	src.Invalidate()
	_, _, _ = src.Snapshot()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for invalidated OnUpdate")
	}

	limits, fetchedAt, _ = src.Snapshot()
	if len(limits) != 1 || *limits[0].UsedPercent != 20 || fetchedAt != 2000 {
		t.Fatalf("refreshed snapshot = %+v, fetchedAt=%d", limits, fetchedAt)
	}
}
