package antigravity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/provider"
)

var (
	ErrNoCredentials = errors.New("not signed in to Antigravity")
	errTokenExpired  = errors.New("Antigravity's saved sign-in has expired; sign in again to refresh limits")

	dailyQuotaEndpoint = "https://daily-cloudcode-pa.googleapis.com"
	prodQuotaEndpoint  = "https://cloudcode-pa.googleapis.com"
	oauthTokenEndpoint = "https://oauth2.googleapis.com/token"

	userHomeDirFunc        = os.UserHomeDir
	quotaKeyringStore      = DefaultKeyringStore
	nowFunc                = time.Now
	refreshKeyringViaCLI   = defaultRefreshKeyringViaCLI
	cliRefreshCooldown     = 2 * time.Minute
	cliRefreshMu           sync.Mutex
	cliRefreshLastAttempt  = make(map[string]time.Time)
)

func defaultRefreshKeyringViaCLI(ctx context.Context, accountID string) error {
	if quotaKeyringStore == nil {
		return ErrNoCredentials
	}
	normID := domain.NormalizeAccount(accountID)
	now := nowFunc()

	cliRefreshMu.Lock()
	if last, ok := cliRefreshLastAttempt[normID]; ok && now.Sub(last) < cliRefreshCooldown {
		cliRefreshMu.Unlock()
		return errTokenExpired
	}
	cliRefreshLastAttempt[normID] = now
	cliRefreshMu.Unlock()

	SpawnMutex.Lock()
	defer SpawnMutex.Unlock()

	if err := quotaKeyringStore.ActivateProfile(normID); err != nil && normID != domain.DefaultAccountID {
		return err
	}
	if normID != domain.DefaultAccountID {
		defer func() { _ = quotaKeyringStore.RestoreDefault() }()
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res := provider.RunCommand(runCtx, "agy", []string{"models"}, nil, "")
	if res.Err != nil {
		return res.Err
	}
	return quotaKeyringStore.CaptureProfile(normID)
}

const quotaRequestTimeout = 10 * time.Second

func antigravityUserAgent() string {
	return fmt.Sprintf("antigravity/hub/2.24.0 (aidev_client; os_type=%s; arch=%s; cl=963137146)", runtime.GOOS, runtime.GOARCH)
}

type cachedAccessToken struct {
	accessToken string
	expiresAt   time.Time
}

var (
	tokenCacheMu sync.Mutex
	tokenCache   = make(map[string]cachedAccessToken)
)

func clearTokenCache() {
	tokenCacheMu.Lock()
	tokenCache = make(map[string]cachedAccessToken)
	tokenCacheMu.Unlock()
}

func newQuotaClient(base *http.Client) *http.Client {
	c := &http.Client{
		Timeout: quotaRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if base != nil {
		if base.Transport != nil {
			c.Transport = base.Transport
		}
		if base.Timeout > 0 {
			c.Timeout = base.Timeout
		}
	}
	return c
}

type rawTokenPayload struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
	ProjectID    string `json:"project_id"`
	Expiry       any    `json:"expiry"`
	Token        *struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Expiry       any    `json:"expiry"`
	} `json:"token"`
}

func parseExpiry(raw any) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t, true
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, true
		}
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return epochToTime(n), true
		}
	case float64:
		return epochToTime(v), true
	case int64:
		return epochToTime(float64(v)), true
	}
	return time.Time{}, false
}

func epochToTime(v float64) time.Time {
	if v > 1e12 {
		sec := int64(v / 1000)
		nsec := int64(math.Round((v - float64(sec)*1000) * 1e6))
		return time.Unix(sec, nsec)
	}
	return time.Unix(int64(v), 0)
}

type resolvedCreds struct {
	accessToken string
	projectID   string
}

func resolveCredentials(ctx context.Context, accountID string, client *http.Client) (resolvedCreds, error) {
	normID := domain.NormalizeAccount(accountID)
	now := nowFunc()

	// 1. Try keyring profile secret first.
	if quotaKeyringStore != nil {
		secret, err := quotaKeyringStore.GetProfileSecret(normID)
		if err == nil && secret != "" {
			creds, parseErr := resolveSecretPayload(ctx, secret, now, client)
			if errors.Is(parseErr, errTokenExpired) && refreshKeyringViaCLI != nil {
				if refErr := refreshKeyringViaCLI(ctx, normID); refErr == nil {
					if freshSecret, freshErr := quotaKeyringStore.GetProfileSecret(normID); freshErr == nil && freshSecret != "" {
						creds, parseErr = resolveSecretPayload(ctx, freshSecret, nowFunc(), client)
					}
				}
			}
			if parseErr != nil {
				return resolvedCreds{}, parseErr
			}
			if creds.accessToken != "" {
				return creds, nil
			}
		}
	}

	// Only default profile falls back to ~/.gemini/antigravity-acp files.
	if normID != domain.DefaultAccountID {
		return resolvedCreds{}, ErrNoCredentials
	}

	home, err := userHomeDirFunc()
	if err != nil || home == "" {
		return resolvedCreds{}, ErrNoCredentials
	}
	acpDir := filepath.Join(home, ".gemini", "antigravity-acp")

	// 2a. Fallback to acp_token.json (supports OAuth refresh).
	acpTokenPath := filepath.Join(acpDir, "acp_token.json")
	if raw, err := os.ReadFile(acpTokenPath); err == nil {
		creds, parseErr := resolveSecretPayload(ctx, string(raw), now, client)
		if parseErr != nil {
			return resolvedCreds{}, parseErr
		}
		if creds.accessToken != "" {
			return creds, nil
		}
	}

	// 2b. Fallback to antigravity-oauth-token.
	cliTokenPath := filepath.Join(acpDir, "antigravity-oauth-token")
	if raw, err := os.ReadFile(cliTokenPath); err == nil {
		creds, parseErr := resolveSecretPayload(ctx, string(raw), now, client)
		if parseErr != nil {
			return resolvedCreds{}, parseErr
		}
		if creds.accessToken != "" {
			return creds, nil
		}
	}

	return resolvedCreds{}, ErrNoCredentials
}

func resolveSecretPayload(ctx context.Context, raw string, now time.Time, client *http.Client) (resolvedCreds, error) {
	decoded := strings.TrimSpace(decodeGoKeyringSecret(raw))
	if decoded == "" {
		return resolvedCreds{}, ErrNoCredentials
	}

	var payload rawTokenPayload
	if err := json.Unmarshal([]byte(decoded), &payload); err != nil {
		return resolvedCreds{}, fmt.Errorf("invalid Antigravity token payload: %w", err)
	}

	accessToken := payload.AccessToken
	refreshToken := payload.RefreshToken
	rawExpiry := payload.Expiry
	if payload.Token != nil {
		if payload.Token.AccessToken != "" {
			accessToken = payload.Token.AccessToken
		}
		if payload.Token.RefreshToken != "" {
			refreshToken = payload.Token.RefreshToken
		}
		if payload.Token.Expiry != nil {
			rawExpiry = payload.Token.Expiry
		}
	}

	expiry, hasExpiry := parseExpiry(rawExpiry)
	canRefresh := payload.ClientID != "" && payload.ClientSecret != "" && refreshToken != ""

	if accessToken != "" && (!hasExpiry || expiry.After(now.Add(60*time.Second))) {
		return resolvedCreds{accessToken: accessToken, projectID: payload.ProjectID}, nil
	}

	if canRefresh {
		fp := fingerprintCreds(payload.ClientID, refreshToken, payload.ProjectID)
		tokenCacheMu.Lock()
		cached, ok := tokenCache[fp]
		tokenCacheMu.Unlock()
		if ok && cached.accessToken != "" && cached.expiresAt.After(now.Add(60*time.Second)) {
			return resolvedCreds{accessToken: cached.accessToken, projectID: payload.ProjectID}, nil
		}

		refreshed, expiresIn, err := refreshOAuthToken(ctx, client, payload.ClientID, payload.ClientSecret, refreshToken)
		if err != nil {
			return resolvedCreds{}, err
		}
		tokenCacheMu.Lock()
		tokenCache[fp] = cachedAccessToken{
			accessToken: refreshed,
			expiresAt:   now.Add(time.Duration(expiresIn) * time.Second),
		}
		tokenCacheMu.Unlock()
		return resolvedCreds{accessToken: refreshed, projectID: payload.ProjectID}, nil
	}

	if hasExpiry && !expiry.After(now.Add(60*time.Second)) {
		return resolvedCreds{}, errTokenExpired
	}
	return resolvedCreds{}, ErrNoCredentials
}

func fingerprintCreds(clientID, refreshToken, projectID string) string {
	sum := sha256.Sum256([]byte(clientID + "\x00" + refreshToken + "\x00" + projectID))
	return hex.EncodeToString(sum[:])
}

func refreshOAuthToken(ctx context.Context, client *http.Client, clientID, clientSecret, refreshToken string) (string, int64, error) {
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	reqCtx, cancel := context.WithTimeout(ctx, quotaRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, oauthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("refresh Antigravity token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
			return "", 0, errors.New("Antigravity sign-in refresh was rejected; sign in again")
		}
		return "", 0, fmt.Errorf("refresh Antigravity token: %s (%s)", resp.Status, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("decode refreshed Antigravity token: %w", err)
	}
	if tokenResp.AccessToken == "" || tokenResp.ExpiresIn <= 0 {
		return "", 0, errors.New("refreshed Antigravity token response was empty")
	}
	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

type quotaAmount struct {
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
}

type quotaBucket struct {
	BucketID          string   `json:"bucketId"`
	Window            string   `json:"window"`
	DisplayName       string   `json:"displayName"`
	Description       string   `json:"description"`
	Disabled          bool     `json:"disabled"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
}

type quotaSummaryResponse struct {
	Groups []struct {
		DisplayName string        `json:"displayName"`
		Buckets     []quotaBucket `json:"buckets"`
	} `json:"groups"`
	Buckets []quotaBucket `json:"buckets"`
}

type availableModelsResponse struct {
	Models map[string]struct {
		ModelProvider   string       `json:"modelProvider"`
		QuotaInfo       *quotaAmount `json:"quotaInfo"`
		WeeklyQuotaInfo *quotaAmount `json:"weeklyQuotaInfo"`
	} `json:"models"`
}

type parsedBucket struct {
	id    string
	label string
	limit domain.RateLimit
}

func clampFraction(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func summaryWindowDuration(window string) (time.Duration, bool) {
	switch window {
	case "5h":
		return 5 * time.Hour, true
	case "weekly":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func parseSummaryResetUnix(bucket quotaBucket, requestedAt time.Time) int64 {
	if bucket.ResetTime == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, bucket.ResetTime)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, bucket.ResetTime)
		if err != nil {
			return 0
		}
	}
	if bucket.RemainingFraction != nil && *bucket.RemainingFraction == 1 && bucket.Description == "" {
		if dur, ok := summaryWindowDuration(bucket.Window); ok {
			expected := requestedAt.Add(dur)
			if math.Abs(parsed.Sub(expected).Seconds()) <= 60 {
				return 0
			}
		}
	}
	return parsed.Unix()
}

func parseSummaryResponse(raw []byte, requestedAt time.Time) ([]parsedBucket, error) {
	var summary quotaSummaryResponse
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil, err
	}

	type bucketGroup struct {
		displayName string
		buckets     []quotaBucket
	}
	var groups []bucketGroup
	for _, g := range summary.Groups {
		if len(g.Buckets) > 0 {
			groups = append(groups, bucketGroup{displayName: g.DisplayName, buckets: g.Buckets})
		}
	}
	if len(groups) == 0 && len(summary.Buckets) > 0 {
		groups = []bucketGroup{{buckets: summary.Buckets}}
	}

	var out []parsedBucket
	for _, g := range groups {
		for _, b := range g.buckets {
			if b.Disabled || b.BucketID == "" || b.RemainingFraction == nil {
				continue
			}
			frac := *b.RemainingFraction
			if math.IsNaN(frac) || math.IsInf(frac, 0) {
				continue
			}

			family := bucketFamily(b.BucketID, g.displayName)
			winSuffix := bucketWindowLabel(b.Window, b.DisplayName)
			label := family
			if winSuffix != "" && winSuffix != family {
				label = family + " · " + winSuffix
			}

			used := int(math.Round((1 - clampFraction(frac)) * 100))
			resetsAt := parseSummaryResetUnix(b, requestedAt)
			out = append(out, parsedBucket{
				id:    b.BucketID,
				label: label,
				limit: domain.RateLimit{
					Window:      label,
					Status:      label,
					UsedPercent: &used,
					ResetsAt:    resetsAt,
				},
			})
		}
	}
	// Put 5h windows before weekly windows within each group so short-term
	// session limits appear first (matching Claude's Session 5h -> Weekly 7d).
	sort.SliceStable(out, func(i, j int) bool {
		fi, _, _ := strings.Cut(out[i].label, " · ")
		fj, _, _ := strings.Cut(out[j].label, " · ")
		if fi != fj {
			return false
		}
		i5h := strings.HasSuffix(out[i].label, "· 5h")
		j5h := strings.HasSuffix(out[j].label, "· 5h")
		return i5h && !j5h
	})
	return out, nil
}

func bucketFamily(bucketID, groupDisplayName string) string {
	trimmed := strings.TrimSpace(groupDisplayName)
	switch {
	case strings.HasPrefix(bucketID, "gemini-argon-"):
		return firstNonEmpty(trimmed, "Gemini Argon")
	case strings.HasPrefix(bucketID, "gemini-carbon-"):
		return firstNonEmpty(trimmed, "Gemini Carbon")
	case strings.HasPrefix(bucketID, "gemini-"):
		return "Gemini"
	case strings.HasPrefix(bucketID, "3p-"):
		return "Claude & GPT"
	default:
		return firstNonEmpty(trimmed, bucketID)
	}
}

func bucketWindowLabel(window, displayName string) string {
	switch window {
	case "weekly":
		return "Weekly"
	case "5h":
		return "5h"
	default:
		return firstNonEmpty(strings.TrimSpace(displayName), strings.TrimSpace(window), "Quota")
	}
}

func parseModelsResponse(raw []byte) ([]parsedBucket, error) {
	var resp availableModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	best := make(map[string]parsedBucket)
	for modelID, model := range resp.Models {
		var family string
		switch {
		case model.ModelProvider == "MODEL_PROVIDER_GOOGLE" || strings.HasPrefix(modelID, "gemini-"):
			family = "Gemini"
		case model.ModelProvider == "MODEL_PROVIDER_ANTHROPIC" || strings.HasPrefix(modelID, "claude-"):
			family = "Claude"
		case model.ModelProvider == "MODEL_PROVIDER_OPENAI" || strings.HasPrefix(modelID, "gpt-"):
			family = "GPT"
		default:
			continue
		}

		entries := []struct {
			suffix string
			label  string
			quota  *quotaAmount
		}{
			{"quota", "Quota", model.QuotaInfo},
			{"weekly", "Weekly", model.WeeklyQuotaInfo},
		}

		for _, entry := range entries {
			if entry.quota == nil || entry.quota.RemainingFraction == nil {
				continue
			}
			frac := *entry.quota.RemainingFraction
			if math.IsNaN(frac) || math.IsInf(frac, 0) {
				continue
			}
			used := int(math.Round((1 - clampFraction(frac)) * 100))
			var resetsAt int64
			if entry.quota.ResetTime != "" {
				if t, err := time.Parse(time.RFC3339Nano, entry.quota.ResetTime); err == nil {
					resetsAt = t.Unix()
				} else if t, err := time.Parse(time.RFC3339, entry.quota.ResetTime); err == nil {
					resetsAt = t.Unix()
				}
			}
			id := strings.ToLower(family) + "-" + entry.suffix
			label := family + " · " + entry.label
			prev, exists := best[id]
			if !exists || (prev.limit.UsedPercent != nil && used > *prev.limit.UsedPercent) {
				best[id] = parsedBucket{
					id:    id,
					label: label,
					limit: domain.RateLimit{
						Window:      label,
						Status:      label,
						UsedPercent: &used,
						ResetsAt:    resetsAt,
					},
				}
			}
		}
	}

	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]parsedBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, best[k])
	}
	// Put 5h windows before weekly windows within each group so short-term
	// session limits appear first (matching Claude's Session 5h -> Weekly 7d).
	sort.SliceStable(out, func(i, j int) bool {
		fi, _, _ := strings.Cut(out[i].label, " · ")
		fj, _, _ := strings.Cut(out[j].label, " · ")
		if fi != fj {
			return false
		}
		i5h := strings.HasSuffix(out[i].label, "· 5h")
		j5h := strings.HasSuffix(out[j].label, "· 5h")
		return i5h && !j5h
	})
	return out, nil
}

type hostResult struct {
	buckets    []parsedBucket
	statusCode int
	err        error
}

func queryEndpoints(
	ctx context.Context,
	client *http.Client,
	creds resolvedCreds,
	operation string,
	keepMax bool,
	parse func(raw []byte, requestedAt time.Time) ([]parsedBucket, error),
) ([]parsedBucket, int, error) {
	endpoints := []string{dailyQuotaEndpoint, prodQuotaEndpoint}
	results := make([]hostResult, len(endpoints))

	bodyMap := map[string]any{}
	if creds.projectID != "" {
		bodyMap["project"] = creds.projectID
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, 0, err
	}

	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		wg.Add(1)
		go func(idx int, baseURL string) {
			defer wg.Done()
			reqCtx, cancel := context.WithTimeout(ctx, quotaRequestTimeout)
			defer cancel()

			requestedAt := nowFunc()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/v1internal:"+operation, bytes.NewReader(bodyBytes))
			if err != nil {
				results[idx] = hostResult{err: err}
				return
			}
			req.Header.Set("Authorization", "Bearer "+creds.accessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", antigravityUserAgent())

			resp, err := client.Do(req)
			if err != nil {
				results[idx] = hostResult{err: err}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				results[idx] = hostResult{
					statusCode: resp.StatusCode,
					err:        fmt.Errorf("quota request failed (%s): %s", resp.Status, strings.TrimSpace(string(detail))),
				}
				return
			}

			raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
			if err != nil {
				results[idx] = hostResult{statusCode: resp.StatusCode, err: err}
				return
			}
			buckets, err := parse(raw, requestedAt)
			results[idx] = hostResult{buckets: buckets, statusCode: resp.StatusCode, err: err}
		}(i, endpoint)
	}
	wg.Wait()

	var merged []parsedBucket
	indexByID := make(map[string]int)
	var firstErr error
	var authStatus int
	for _, res := range results {
		if res.statusCode == http.StatusUnauthorized || res.statusCode == http.StatusForbidden {
			if authStatus == 0 {
				authStatus = res.statusCode
			}
		}
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		for _, b := range res.buckets {
			if idx, exists := indexByID[b.id]; exists {
				if keepMax && b.limit.UsedPercent != nil && merged[idx].limit.UsedPercent != nil && *b.limit.UsedPercent > *merged[idx].limit.UsedPercent {
					merged[idx] = b
				}
				continue
			}
			indexByID[b.id] = len(merged)
			merged = append(merged, b)
		}
	}
	return merged, authStatus, firstErr
}

func finalizeLimits(buckets []parsedBucket) []domain.RateLimit {
	// Ensure Window is distinct across all buckets even if two bucket IDs share a display label.
	labelCount := make(map[string]int, len(buckets))
	for _, b := range buckets {
		labelCount[b.limit.Window]++
	}
	limits := make([]domain.RateLimit, 0, len(buckets))
	for _, b := range buckets {
		limit := b.limit
		if labelCount[limit.Window] > 1 {
			limit.Window = fmt.Sprintf("%s (%s)", limit.Window, b.id)
		}
		limits = append(limits, limit)
	}
	return limits
}

// FetchQuota retrieves Antigravity usage quota windows for the given account.
func FetchQuota(ctx context.Context, accountID string, client *http.Client) ([]domain.RateLimit, int64, error) {
	httpClient := newQuotaClient(client)
	creds, err := resolveCredentials(ctx, accountID, httpClient)
	if err != nil {
		return nil, 0, err
	}

	summaryBuckets, summaryAuth, summaryErr := queryEndpoints(ctx, httpClient, creds, "retrieveUserQuotaSummary", false, parseSummaryResponse)
	if len(summaryBuckets) > 0 {
		return finalizeLimits(summaryBuckets), nowFunc().UnixMilli(), nil
	}

	modelBuckets, modelAuth, modelErr := queryEndpoints(ctx, httpClient, creds, "fetchAvailableModels", true, func(raw []byte, _ time.Time) ([]parsedBucket, error) {
		return parseModelsResponse(raw)
	})
	if len(modelBuckets) > 0 {
		return finalizeLimits(modelBuckets), nowFunc().UnixMilli(), nil
	}

	if summaryAuth == http.StatusUnauthorized || summaryAuth == http.StatusForbidden || modelAuth == http.StatusUnauthorized || modelAuth == http.StatusForbidden {
		return nil, 0, errors.New("Antigravity credentials were rejected; sign in again")
	}
	if summaryErr != nil {
		return nil, 0, summaryErr
	}
	if modelErr != nil {
		return nil, 0, modelErr
	}
	return nil, 0, errors.New("Antigravity did not return usable quota information")
}
