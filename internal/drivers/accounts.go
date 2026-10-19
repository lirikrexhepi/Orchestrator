package drivers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"composer/internal/domain"
	"composer/internal/opencode"
	"composer/internal/provider"
)

var (
	_ provider.AccountDriver = (*claudeDriver)(nil)
	_ provider.AccountDriver = (*openCodeDriver)(nil)
)

func (d *claudeDriver) Binary(settings domain.ProviderSettings) string {
	return binaryFor(settings, "claude")
}

func (d *openCodeDriver) Binary(settings domain.ProviderSettings) string {
	return binaryFor(settings, "opencode")
}

func (d *claudeDriver) AccountEnv(dir string) map[string]string {
	return map[string]string{"CLAUDE_CONFIG_DIR": dir}
}

func (d *claudeDriver) AccountUnset() []string {
	return []string{
		"CLAUDE_CONFIG_DIR",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_REFRESH_TOKEN",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
	}
}

func (d *claudeDriver) DefaultConfigDir(env map[string]string) string {
	if dir := strings.TrimSpace(lookupEnv(env, "CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func (d *claudeDriver) SharedSettings() []string {
	return []string{"CLAUDE.md", "settings.json", "skills", "agents", "commands", "output-styles"}
}

func (d *claudeDriver) SignInArgs() []string {
	return []string{"auth", "login"}
}

func (d *claudeDriver) StatusArgs() []string {
	return []string{"auth", "status", "--json"}
}

func (d *claudeDriver) ParseStatus(result provider.CommandResult) provider.AccountStatus {
	var status struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		Email            string `json:"email"`
		OrgName          string `json:"orgName"`
		SubscriptionType string `json:"subscriptionType"`
	}
	raw := strings.TrimSpace(result.Stdout)
	if start := strings.Index(raw, "{"); start >= 0 {
		raw = raw[start:]
	}
	if json.Unmarshal([]byte(raw), &status) != nil {
		detail := strings.TrimSpace(firstLine(result.Stderr + "\n" + result.Stdout))
		return provider.AccountStatus{Detail: detail}
	}
	if !status.LoggedIn {
		return provider.AccountStatus{Known: true, Detail: "Not signed in"}
	}
	parts := make([]string, 0, 3)
	for _, part := range []string{status.Email, status.OrgName, status.SubscriptionType} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	if len(parts) == 0 && status.AuthMethod != "" {
		parts = append(parts, status.AuthMethod)
	}
	return provider.AccountStatus{SignedIn: true, Known: true, Detail: strings.Join(parts, " · ")}
}

func (d *openCodeDriver) AccountEnv(dir string) map[string]string {
	return map[string]string{"XDG_DATA_HOME": dir}
}

func (d *openCodeDriver) AccountUnset() []string {
	return []string{
		"XDG_DATA_HOME",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"OPENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_GENERATIVE_AI_API_KEY",
	}
}

func (d *openCodeDriver) DefaultConfigDir(env map[string]string) string {
	if dir := strings.TrimSpace(lookupEnv(env, "XDG_DATA_HOME")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

func (d *openCodeDriver) SharedSettings() []string {
	return nil
}

func (d *openCodeDriver) SignInArgs() []string {
	return []string{"auth", "login"}
}

func (d *openCodeDriver) StatusArgs() []string {
	if opencode.IsV2(context.Background(), domain.ProviderSettings{}) {
		return []string{"auth", "list", "--standalone"}
	}
	return []string{"auth", "list"}
}

var openCodeCredentials = regexp.MustCompile(`(\d+)\s+credentials?`)

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (d *openCodeDriver) ParseStatus(result provider.CommandResult) provider.AccountStatus {
	text := ansiSequence.ReplaceAllString(result.Stdout+"\n"+result.Stderr, "")
	if strings.Contains(text, "No authenticated integrations") {
		return provider.AccountStatus{Known: true, Detail: "Not signed in"}
	}
	match := openCodeCredentials.FindStringSubmatch(text)
	if match == nil && result.Err == nil && opencode.IsV2(context.Background(), domain.ProviderSettings{}) {
		if line := strings.TrimSpace(firstLine(text)); line != "" {
			return provider.AccountStatus{SignedIn: true, Known: true, Detail: line}
		}
	}
	if match == nil {
		return provider.AccountStatus{Detail: strings.TrimSpace(firstLine(text))}
	}
	count, _ := strconv.Atoi(match[1])
	if count == 0 {
		return provider.AccountStatus{Known: true, Detail: "Not signed in"}
	}
	noun := "providers"
	if count == 1 {
		noun = "provider"
	}
	return provider.AccountStatus{SignedIn: true, Known: true, Detail: strconv.Itoa(count) + " " + noun}
}

func lookupEnv(env map[string]string, key string) string {
	for name, value := range env {
		if strings.EqualFold(name, key) {
			return value
		}
	}
	return ""
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
