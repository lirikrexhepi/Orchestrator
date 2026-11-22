package main

import (
	"context"
	"strings"
	"testing"

	"composer/internal/domain"
	"composer/internal/provider"
	"composer/internal/session"
)

type fakeChatDriver struct {
	emitter provider.Emitter
}

func (d *fakeChatDriver) Kind() domain.DriverKind { return domain.DriverClaude }
func (d *fakeChatDriver) DisplayName() string     { return "Fake" }
func (d *fakeChatDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{Enabled: true}
}
func (d *fakeChatDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	return domain.ProviderSnapshot{Availability: domain.AvailabilityReady}
}
func (d *fakeChatDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	d.emitter = emit
	return &fakeChatAdapter{}, nil
}

type fakeChatAdapter struct{}

func (a *fakeChatAdapter) Driver() domain.DriverKind           { return domain.DriverClaude }
func (a *fakeChatAdapter) Capabilities() provider.Capabilities { return provider.Capabilities{} }
func (a *fakeChatAdapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	return domain.Session{ThreadID: in.ThreadID}, nil
}
func (a *fakeChatAdapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error { return nil }
func (a *fakeChatAdapter) InterruptTurn(ctx context.Context, threadID string) error   { return nil }
func (a *fakeChatAdapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	return nil
}
func (a *fakeChatAdapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	return nil
}
func (a *fakeChatAdapter) StopSession(ctx context.Context, threadID string) error { return nil }
func (a *fakeChatAdapter) StopAll(ctx context.Context) error                      { return nil }
func (a *fakeChatAdapter) HasSession(threadID string) bool                        { return true }

func TestChatRefLiveActiveThread(t *testing.T) {
	driver := &fakeChatDriver{}
	registry := provider.NewRegistry(driver)
	app := &App{
		manager:    session.NewManager(registry),
		workspaces: session.NewWorkspaces(),
	}

	ws := app.workspaces.Create("Workspace Alpha", "Test prompt", "/path/to/dragged/project")
	app.workspaces.AddTask(ws.ID, domain.Task{
		ThreadID:    "thread-live-drag-1",
		WorkspaceID: ws.ID,
		Title:       "Live Drag Chat",
		Cwd:         "/path/to/dragged/project",
	})

	_, err := app.manager.Start(context.Background(), domain.DriverClaude, domain.SessionStartInput{
		ThreadID: "thread-live-drag-1",
		Cwd:      "/path/to/dragged/project",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Publish live in-memory messages before any disk flush has occurred
	app.manager.RecordUserMessage("thread-live-drag-1", "turn-1", "Please check server status")

	driver.emitter.Emit(domain.RuntimeEvent{
		ThreadID: "thread-live-drag-1",
		TurnID:   "turn-1",
		Kind:     domain.EventAgentMessage,
		Text:     "Checking services now.",
		Seq:      2,
		At:       1005,
	})
	// Massive tool stdout that should be omitted
	driver.emitter.Emit(domain.RuntimeEvent{
		ThreadID: "thread-live-drag-1",
		TurnID:   "turn-1",
		Kind:     domain.EventToolResult,
		Tool: &domain.ToolCall{
			Name:   "bash",
			Output: strings.Repeat("bloated bash stdout line\n", 300),
		},
		Seq: 3,
		At:  1010,
	})
	// File edit that should appear compactly
	driver.emitter.Emit(domain.RuntimeEvent{
		ThreadID: "thread-live-drag-1",
		TurnID:   "turn-1",
		Kind:     domain.EventToolCall,
		Tool: &domain.ToolCall{
			Name:     "edit_file",
			Category: domain.ToolCategoryEdit,
			Target:   "main.go",
		},
		Seq: 4,
		At:  1015,
	})
	driver.emitter.Emit(domain.RuntimeEvent{
		ThreadID: "thread-live-drag-1",
		TurnID:   "turn-1",
		Kind:     domain.EventTurnCompleted,
		Seq:      5,
		At:       1020,
	})

	ref := ws.ID + "/thread-live-drag-1"
	ctx := app.chatRefContext(ref)

	// Verify threadId, title, and cwd attributes
	if !strings.Contains(ctx, `threadId="thread-live-drag-1"`) {
		t.Fatalf("expected threadId attribute in context, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, `title="Live Drag Chat"`) {
		t.Fatalf("expected title attribute in context, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, `cwd="/path/to/dragged/project"`) {
		t.Fatalf("expected cwd attribute in context, got:\n%s", ctx)
	}

	// Verify conversation messages present
	if !strings.Contains(ctx, "Please check server status") {
		t.Fatalf("expected user message in context, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "Checking services now.") {
		t.Fatalf("expected agent message in context, got:\n%s", ctx)
	}

	// Verify compact file modification present
	if !strings.Contains(ctx, "[Modified files: main.go]") {
		t.Fatalf("expected compact modified files line, got:\n%s", ctx)
	}

	// Verify raw tool stdout bloat is omitted
	if strings.Contains(ctx, "bloated bash stdout") {
		t.Fatalf("raw tool stdout was not omitted from chat context:\n%s", ctx)
	}
}

func TestExpandChatRefs(t *testing.T) {
	driver := &fakeChatDriver{}
	registry := provider.NewRegistry(driver)
	app := &App{
		manager:    session.NewManager(registry),
		workspaces: session.NewWorkspaces(),
	}

	ws := app.workspaces.Create("Workspace Beta", "Prompt", "/path/beta")
	app.workspaces.AddTask(ws.ID, domain.Task{
		ThreadID:    "thread-beta-1",
		WorkspaceID: ws.ID,
		Title:       "Beta Chat",
		Cwd:         "/path/beta",
	})
	app.manager.RecordUserMessage("thread-beta-1", "turn-1", "Hello from beta")

	files := []domain.FileRef{
		{Path: "chat://" + ws.ID + "/thread-beta-1"},
		{Path: "/local/file.txt"},
	}

	block, rest := app.expandChatRefs(files)
	if !strings.Contains(block, `threadId="thread-beta-1"`) {
		t.Fatalf("expected expanded chat context, got:\n%s", block)
	}
	if len(rest) != 1 || rest[0].Path != "/local/file.txt" {
		t.Fatalf("expected remaining files to have only non-chat file, got: %+v", rest)
	}
}
