package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"composer/internal/domain"
	"composer/internal/mcp"
	"composer/internal/provider"
	"composer/internal/session"
)

type testFakeDriver struct {
	kind    domain.DriverKind
	adapter *testFakeAdapter
}

func (d *testFakeDriver) Kind() domain.DriverKind { return d.kind }
func (d *testFakeDriver) DisplayName() string     { return string(d.kind) }
func (d *testFakeDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{}
}
func (d *testFakeDriver) Command() string { return "fake" }
func (d *testFakeDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	return domain.ProviderSnapshot{
		Driver:       d.kind,
		Availability: domain.AvailabilityReady,
		Models:       []domain.Model{{ID: "fake-model-1", Default: true}},
	}
}
func (d *testFakeDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	return d.adapter, nil
}
func (d *testFakeDriver) Binary(settings domain.ProviderSettings) string { return "fake" }
func (d *testFakeDriver) AccountEnv(dir string) map[string]string        { return nil }
func (d *testFakeDriver) AccountUnset() []string                         { return nil }
func (d *testFakeDriver) DefaultConfigDir(env map[string]string) string  { return "" }
func (d *testFakeDriver) SharedSettings() []string                       { return nil }
func (d *testFakeDriver) SignInArgs() []string                           { return nil }
func (d *testFakeDriver) StatusArgs() []string                           { return nil }
func (d *testFakeDriver) ParseStatus(result provider.CommandResult) provider.AccountStatus {
	return provider.AccountStatus{}
}

type testFakeAdapter struct {
	mu       sync.Mutex
	started  []domain.SessionStartInput
	sessions map[string]domain.SessionStartInput
}

func (a *testFakeAdapter) Driver() domain.DriverKind           { return domain.DriverClaude }
func (a *testFakeAdapter) Capabilities() provider.Capabilities { return provider.Capabilities{} }
func (a *testFakeAdapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	a.mu.Lock()
	a.started = append(a.started, in)
	if a.sessions == nil {
		a.sessions = make(map[string]domain.SessionStartInput)
	}
	a.sessions[in.ThreadID] = in
	a.mu.Unlock()
	return domain.Session{ThreadID: in.ThreadID, Driver: domain.DriverClaude, Cwd: in.Cwd, Model: in.Model}, nil
}
func (a *testFakeAdapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error { return nil }
func (a *testFakeAdapter) InterruptTurn(ctx context.Context, threadID string) error    { return nil }
func (a *testFakeAdapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	return nil
}
func (a *testFakeAdapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	return nil
}
func (a *testFakeAdapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	delete(a.sessions, threadID)
	a.mu.Unlock()
	return nil
}
func (a *testFakeAdapter) StopAll(ctx context.Context) error { return nil }
func (a *testFakeAdapter) HasSession(threadID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.sessions[threadID]
	return ok
}

func TestReadThreadCollapsingAndViews(t *testing.T) {
	app := &App{
		workspaces: session.NewWorkspaces(),
	}
	backend := &appMCPBackend{app: app}

	ws := app.workspaces.Create("Workspace 1", "prompt", "/tmp/repo")
	task, ok := app.workspaces.AddTask(ws.ID, domain.Task{
		ThreadID:   "thread-read-test",
		Title:      "Test Task",
		Driver:     domain.DriverClaude,
		Permission: domain.PermissionDefault,
	})
	if !ok {
		t.Fatalf("failed to add task")
	}

	events := []domain.RuntimeEvent{
		{
			Kind:     domain.EventUserMessage,
			ThreadID: task.ThreadID,
			Text:     "Initial prompt",
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "I will ",
			Delta:    true,
			ItemID:   "msg-1",
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "check the repo.",
			Delta:    true,
			ItemID:   "msg-1",
		},
		{
			Kind:     domain.EventToolCall,
			ThreadID: task.ThreadID,
			Tool: &domain.ToolCall{
				ID:     "tool-1",
				Name:   "bash",
				Target: "ls",
				Status: domain.ToolPending,
				Input:  map[string]any{"cmd": "ls -la"},
			},
		},
		{
			Kind:     domain.EventToolResult,
			ThreadID: task.ThreadID,
			Tool: &domain.ToolCall{
				ID:     "tool-1",
				Name:   "bash",
				Target: "ls",
				Status: domain.ToolCompleted,
				Output: "file1.go\nfile2.go",
			},
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "Found files.",
		},
		{
			Kind:     domain.EventTurnFailed,
			ThreadID: task.ThreadID,
			Error:    "failed to compile",
		},
	}

	itemsMessages := collapseAndFilterEvents(events, "messages")
	// Expected:
	// 0: user "Initial prompt"
	// 1: assistant "I will check the repo." (collapsed from deltas)
	// 2: assistant "Found files." (or merged assistant)
	// 3: error "failed to compile"
	for _, item := range itemsMessages {
		if item.Kind == "tool" {
			t.Fatalf("expected no tool items in 'messages' view, found: %+v", item)
		}
	}

	// Verify streaming delta collapse
	if !strings.Contains(itemsMessages[1].Text, "I will check the repo.") {
		t.Errorf("expected collapsed assistant message, got %q", itemsMessages[1].Text)
	}

	itemsActivity := collapseAndFilterEvents(events, "activity")
	foundTool := false
	for _, item := range itemsActivity {
		if item.Kind == "tool" {
			foundTool = true
			if item.Tool != "bash" {
				t.Errorf("tool = %q, want 'bash'", item.Tool)
			}
			if item.Status != "completed" {
				t.Errorf("status = %q, want 'completed'", item.Status)
			}
			if item.Text != "file1.go\nfile2.go" {
				t.Errorf("text = %q, want 'file1.go\\nfile2.go'", item.Text)
			}
		}
	}
	if !foundTool {
		t.Fatalf("expected tool item in 'activity' view")
	}

	// Test ReadThread method with limit
	res, err := backend.ReadThread(context.Background(), task.ThreadID, "messages", 2, 2000)
	if err != nil {
		t.Fatalf("ReadThread failed: %v", err)
	}
	_ = res
}

func TestManagerIssueAndRevokeMCP(t *testing.T) {
	adapter := &testFakeAdapter{}
	driver := &testFakeDriver{kind: domain.DriverClaude, adapter: adapter}
	reg := provider.NewRegistry(driver)
	mgr := session.NewManager(reg)

	srv := mcp.NewServer(&appMCPBackend{})
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start MCP server: %v", err)
	}
	defer srv.Close(context.Background())

	mgr.SetMCPProvider(&appMCPProvider{server: srv})

	in := domain.SessionStartInput{
		ThreadID:   "thread-mcp-mgr-1",
		Cwd:        "/test/cwd",
		Permission: domain.PermissionDefault,
	}

	sess, err := mgr.Start(context.Background(), domain.DriverClaude, in)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if sess.ThreadID != "thread-mcp-mgr-1" {
		t.Errorf("sess.ThreadID = %q, want 'thread-mcp-mgr-1'", sess.ThreadID)
	}

	adapter.mu.Lock()
	if len(adapter.started) != 1 {
		t.Fatalf("expected 1 started session, got %d", len(adapter.started))
	}
	startedIn := adapter.started[0]
	adapter.mu.Unlock()

	if startedIn.MCP == nil {
		t.Fatalf("expected started session to have MCP populated")
	}
	if startedIn.MCP.ServerName != "orchestrator" {
		t.Errorf("ServerName = %q, want 'orchestrator'", startedIn.MCP.ServerName)
	}
	if startedIn.MCP.Endpoint != endpoint {
		t.Errorf("Endpoint = %q, want %q", startedIn.MCP.Endpoint, endpoint)
	}
	if !strings.HasPrefix(startedIn.MCP.AuthHeader, "Bearer ") {
		t.Errorf("AuthHeader = %q, want prefix 'Bearer '", startedIn.MCP.AuthHeader)
	}

	rawToken := strings.TrimPrefix(startedIn.MCP.AuthHeader, "Bearer ")
	scope, _, _, found := srv.Registry().Resolve(rawToken)
	if !found {
		t.Fatalf("expected token %q to be resolved in registry", rawToken)
	}
	if scope.ThreadID != "thread-mcp-mgr-1" {
		t.Errorf("scope.ThreadID = %q, want 'thread-mcp-mgr-1'", scope.ThreadID)
	}

	// Stop session -> should revoke credential
	if err := mgr.Stop(context.Background(), "thread-mcp-mgr-1"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	_, _, _, foundAfterStop := srv.Registry().Resolve(rawToken)
	if foundAfterStop {
		t.Fatalf("expected credential to be revoked after Stop")
	}
}
