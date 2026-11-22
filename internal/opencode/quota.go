package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"composer/internal/domain"
)

var goUsageEndpoint = "https://opencode.ai/zen/go/v1/usage"

var ErrNoGoCredentials = errors.New("not signed in to OpenCode Go")

type goAuthEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type goUsageWindow struct {
	Status   string `json:"status"`
	Percent  *int   `json:"percent"`
	ResetsAt string `json:"resetsAt"`
}

type goUsagePayload struct {
	Usage map[string]goUsageWindow `json:"usage"`
}

var goWindowNames = map[string]string{
	"rolling": "five_hour",
	"weekly":  "seven_day",
	"monthly": "monthly",
}

func goAuthPath() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			candidate := filepath.Join(local, "opencode", "auth.json")
			if _, statErr := os.Stat(candidate); statErr == nil || err != nil {
				return candidate, nil
			}
			if err == nil {
				fallback := filepath.Join(home, ".local", "share", "opencode", "auth.json")
				if _, statErr := os.Stat(fallback); statErr == nil {
					return fallback, nil
				}
			}
			return candidate, nil
		}
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json"), nil
}

func goKeyFromContent(raw string) string {
	var entries map[string]goAuthEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return ""
	}
	if entry, ok := entries["opencode-go"]; ok && entry.Type == "api" && entry.Key != "" {
		return entry.Key
	}
	return ""
}

func goAPIKey() (string, error) {
	if key := strings.TrimSpace(os.Getenv("OPENCODE_GO_API_KEY")); key != "" {
		return key, nil
	}
	if content := strings.TrimSpace(os.Getenv("OPENCODE_AUTH_CONTENT")); content != "" {
		if key := goKeyFromContent(content); key != "" {
			return key, nil
		}
	}
	path, err := goAuthPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ErrNoGoCredentials
	}
	if key := goKeyFromContent(string(raw)); key != "" {
		return key, nil
	}
	return "", ErrNoGoCredentials
}

func goLimitsFrom(payload goUsagePayload) []domain.RateLimit {
	limits := make([]domain.RateLimit, 0, len(payload.Usage))
	for name, window := range payload.Usage {
		if window.Percent == nil {
			continue
		}
		mapped, ok := goWindowNames[name]
		if !ok {
			mapped = name
		}
		limit := domain.RateLimit{
			Window:      mapped,
			Status:      window.Status,
			UsedPercent: window.Percent,
		}
		if window.ResetsAt != "" {
			if parsed, err := time.Parse(time.RFC3339, window.ResetsAt); err == nil {
				limit.ResetsAt = parsed.Unix()
			}
		}
		limits = append(limits, limit)
	}
	return limits
}

func FetchGoQuota(ctx context.Context, client *http.Client) ([]domain.RateLimit, int64, error) {
	key, err := goAPIKey()
	if err != nil {
		return nil, 0, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, goUsageEndpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")

	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, 0, errors.New("OpenCode Go key rejected, sign in again with /connect")
	case http.StatusForbidden:
		return nil, 0, errors.New("no active OpenCode Go subscription on this key")
	case http.StatusOK:
	default:
		return nil, 0, fmt.Errorf("go usage request failed: %s", strings.TrimSpace(response.Status))
	}

	var payload goUsagePayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, 0, err
	}

	limits := goLimitsFrom(payload)
	if len(limits) == 0 {
		return nil, 0, errors.New("go usage response reported no windows")
	}
	return limits, time.Now().UnixMilli(), nil
}
