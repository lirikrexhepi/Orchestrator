package titler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/provider"
	"composer/internal/shell"
)

const (
	claudeTitleModel = "haiku"
	claudeSystem     = "You write short titles for coding chats. Reply with the title only."
)

var preferredAntigravityModels = []string{"gemini-3.8-flash-low", "gemini-3.8-flash-medium"}

func Installed(binary string, settings domain.ProviderSettings) bool {
	_, found := shell.LookPath(binary, provider.LaunchEnv(settings))
	return found
}

func binaryOr(settings domain.ProviderSettings, fallback string) string {
	if strings.TrimSpace(settings.BinaryPath) != "" {
		return settings.BinaryPath
	}
	return fallback
}

func Claude(settings domain.ProviderSettings, workDir string) func(ctx context.Context, prompt string) (string, error) {
	return func(ctx context.Context, prompt string) (string, error) {
		args := []string{
			"-p",
			"--model", claudeTitleModel,
			"--no-session-persistence",
			"--tools", "",
			"--strict-mcp-config",
			"--disable-slash-commands",
			"--output-format", "json",
			"--system-prompt", claudeSystem,
		}
		deadline, ok := ctx.Deadline()
		timeout := 45 * time.Second
		if ok {
			timeout = time.Until(deadline)
		}
		result := provider.RunCommandWith(ctx, binaryOr(settings, "claude"), args, provider.LaunchEnv(settings), workDir, prompt, timeout)
		var reply struct {
			Result         string `json:"result"`
			IsError        bool   `json:"is_error"`
			TerminalReason string `json:"terminal_reason"`
		}
		if err := json.Unmarshal([]byte(lastJSON(result.Stdout)), &reply); err != nil {
			if result.Err != nil {
				return "", errors.New(strings.TrimSpace(firstLine(result.Stderr + " " + result.Err.Error())))
			}
			return "", errors.New("claude returned no JSON")
		}
		if reply.IsError || strings.TrimSpace(reply.Result) == "" {
			return "", errors.New("claude failed: " + reply.TerminalReason)
		}
		return reply.Result, nil
	}
}

type antigravityModels struct {
	mu     sync.Mutex
	chosen string
	at     time.Time
}

var agyModels antigravityModels

func (m *antigravityModels) pick(ctx context.Context, binary string, env map[string]string, workDir string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chosen != "" && time.Since(m.at) < time.Hour {
		return m.chosen
	}
	result := provider.RunCommandWith(ctx, binary, []string{"models"}, env, workDir, "", 20*time.Second)
	var ids []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && !strings.HasSuffix(fields[0], "...") && strings.Contains(fields[0], "-") {
			ids = append(ids, fields[0])
		}
	}
	chosen := ""
	for _, preferred := range preferredAntigravityModels {
		for _, id := range ids {
			if id == preferred {
				chosen = id
				break
			}
		}
		if chosen != "" {
			break
		}
	}
	if chosen == "" {
		for _, id := range ids {
			if strings.Contains(id, "flash") && strings.HasSuffix(id, "-low") {
				chosen = id
				break
			}
		}
	}
	if chosen == "" {
		for _, id := range ids {
			if strings.Contains(id, "flash") {
				chosen = id
				break
			}
		}
	}
	if chosen == "" {
		chosen = preferredAntigravityModels[0]
	}
	m.chosen, m.at = chosen, time.Now()
	return chosen
}

func Antigravity(settings domain.ProviderSettings, workDir string, onConversation func(id string)) func(ctx context.Context, prompt string) (string, error) {
	return func(ctx context.Context, prompt string) (string, error) {
		binary := binaryOr(settings, "agy")
		env := provider.LaunchEnv(settings)
		model := agyModels.pick(ctx, binary, env, workDir)
		args := []string{"-p", prompt, "--model", model, "--output-format", "json", "--disable-slash-commands"}
		deadline, ok := ctx.Deadline()
		timeout := 45 * time.Second
		if ok {
			timeout = time.Until(deadline)
		}
		result := provider.RunCommandWith(ctx, binary, args, env, workDir, "", timeout)
		var reply struct {
			ConversationID string `json:"conversation_id"`
			Status         string `json:"status"`
			Response       string `json:"response"`
		}
		if err := json.Unmarshal([]byte(lastJSON(result.Stdout)), &reply); err != nil {
			if result.Err != nil {
				return "", errors.New(strings.TrimSpace(firstLine(result.Stderr + " " + result.Err.Error())))
			}
			return "", errors.New("antigravity returned no JSON")
		}
		if reply.ConversationID != "" && onConversation != nil {
			onConversation(reply.ConversationID)
		}
		if reply.Status != "" && reply.Status != "SUCCESS" {
			return "", errors.New("antigravity status " + reply.Status)
		}
		return reply.Response, nil
	}
}

func lastJSON(output string) string {
	output = strings.TrimSpace(output)
	if start := strings.LastIndex(output, "\n{"); start >= 0 {
		return output[start+1:]
	}
	if start := strings.Index(output, "{"); start >= 0 {
		return output[start:]
	}
	return output
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
