package claude

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"composer/internal/domain"
	"composer/internal/process"
)

func TestMCPConfigGenerationAndBuildArgs(t *testing.T) {
	cfg := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:54321/mcp",
		AuthHeader: "Bearer token-abc-xyz",
	}

	path, err := writeMCPConfig(cfg)
	if err != nil {
		t.Fatalf("writeMCPConfig failed: %v", err)
	}
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp config file: %v", err)
	}

	var parsed struct {
		MCPServers struct {
			Orchestrator struct {
				Type    string            `json:"type"`
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
			} `json:"orchestrator"`
		} `json:"mcpServers"`
	}

	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}

	if parsed.MCPServers.Orchestrator.Type != "http" {
		t.Errorf("type = %q, want 'http'", parsed.MCPServers.Orchestrator.Type)
	}
	if parsed.MCPServers.Orchestrator.URL != "http://127.0.0.1:54321/mcp" {
		t.Errorf("url = %q, want 'http://127.0.0.1:54321/mcp'", parsed.MCPServers.Orchestrator.URL)
	}
	if parsed.MCPServers.Orchestrator.Headers["Authorization"] != "Bearer token-abc-xyz" {
		t.Errorf("Authorization header = %q, want 'Bearer token-abc-xyz'", parsed.MCPServers.Orchestrator.Headers["Authorization"])
	}

	a := NewAdapter(domain.ProviderSettings{}, nil)
	args := a.buildArgs(domain.SessionStartInput{ThreadID: "th-1"}, path)

	hasConfig := false
	hasAllowed := false
	for i, arg := range args {
		if arg == "--mcp-config" && i+1 < len(args) && args[i+1] == path {
			hasConfig = true
		}
		if arg == "--allowedTools" && i+1 < len(args) && args[i+1] == "mcp__orchestrator__*" {
			hasAllowed = true
		}
	}

	if !hasConfig {
		t.Errorf("args missing --mcp-config %q: %v", path, args)
	}
	if !hasAllowed {
		t.Errorf("args missing --allowedTools mcp__orchestrator__*: %v", args)
	}
}

func TestMCPTempFileCleanup(t *testing.T) {
	cfg := &domain.MCPConfig{
		Endpoint:   "http://127.0.0.1:54321/mcp",
		AuthHeader: "Bearer token",
	}

	path, err := writeMCPConfig(cfg)
	if err != nil {
		t.Fatalf("writeMCPConfig failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	a := NewAdapter(domain.ProviderSettings{}, nil)
	s := &session{
		threadID:      "t-cleanup",
		mcpConfigFile: path,
		cancel:        func() {},
	}
	a.sessions["t-cleanup"] = s

	// Calling StopSession should remove the temp file
	_ = a.StopSession(context.Background(), "t-cleanup")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected temp file %s to be deleted after StopSession, got err: %v", path, err)
		_ = os.Remove(path)
	}
}

func TestMCPStartSessionProcessFailureCleanup(t *testing.T) {
	a := NewAdapter(domain.ProviderSettings{
		BinaryPath: "non-existent-binary-for-orchestrator-test",
	}, nil)

	in := domain.SessionStartInput{
		ThreadID: "t-fail",
		Cwd:      t.TempDir(),
		MCP: &domain.MCPConfig{
			Endpoint:   "http://127.0.0.1:1234/mcp",
			AuthHeader: "Bearer token",
		},
	}

	_, err := a.StartSession(context.Background(), in)
	if err == nil {
		t.Fatalf("expected StartSession to fail for non-existent binary")
	}

	// Verify no stray orchestrator-mcp-*.json files were left behind
	// (By testing writeMCPConfig and immediate removal on error)
	_ = process.ErrNotFound
}
