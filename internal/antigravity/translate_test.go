package antigravity

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/jsonrpc"
)

type eventRecorder struct {
	mu     sync.Mutex
	events []domain.RuntimeEvent
}

func (r *eventRecorder) Emit(e domain.RuntimeEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *eventRecorder) all() []domain.RuntimeEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.RuntimeEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *eventRecorder) wait(t *testing.T, predicate func(domain.RuntimeEvent) bool) domain.RuntimeEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.events {
			if predicate(e) {
				r.mu.Unlock()
				return e
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for matching event")
	return domain.RuntimeEvent{}
}

func newTestRig(t *testing.T, perm domain.PermissionMode) (*Adapter, *session, *jsonrpc.Conn, *eventRecorder) {
	t.Helper()
	rec := &eventRecorder{}
	a := NewAdapter(domain.ProviderSettings{}, rec)

	s := &session{
		threadID:   "t1",
		cwd:        `C:\repo`,
		permission: perm,
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
		sessionID:  "sess-test",
	}

	rClient, wServer := io.Pipe()
	rServer, wClient := io.Pipe()

	serverConn := jsonrpc.NewConn(rServer, wServer, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return nil, nil
	})
	clientConn := jsonrpc.NewConn(rClient, wClient, a.makeHandler(s))
	s.conn = clientConn

	a.mu.Lock()
	a.sessions["t1"] = s
	a.mu.Unlock()

	t.Cleanup(func() {
		clientConn.Close(nil)
		serverConn.Close(nil)
		_ = wClient.Close()
		_ = wServer.Close()
		_ = rClient.Close()
		_ = rServer.Close()
	})

	return a, s, serverConn, rec
}

func TestAgentThoughtAndMessageItemSegmentation(t *testing.T) {
	_, s, serverConn, rec := newTestRig(t, domain.PermissionBypass)
	s.turnID = "turn-1"

	// 1. Thought chunk 1
	thought1, _ := json.Marshal(acpTextContent{Type: "text", Text: "thinking 1..."})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "agent_thought_chunk",
			Content:       thought1,
		},
	})
	ev1 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventAgentThought && e.Text == "thinking 1..."
	})
	if ev1.ItemID != "turn-1:thought:1" {
		t.Fatalf("thought 1 ItemID = %q, want turn-1:thought:1", ev1.ItemID)
	}

	// 2. Thought chunk 2 (same segment)
	thought2, _ := json.Marshal(acpTextContent{Type: "text", Text: "thinking 2..."})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "agent_thought_chunk",
			Content:       thought2,
		},
	})
	ev2 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventAgentThought && e.Text == "thinking 2..."
	})
	if ev2.ItemID != "turn-1:thought:1" {
		t.Fatalf("thought 2 ItemID = %q, want turn-1:thought:1", ev2.ItemID)
	}

	// 3. Tool call (resets activeKind)
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "call-1",
			Name:          "run_command",
			Title:         "Running test",
			Status:        "in_progress",
			Args:          map[string]any{"command": "go test"},
		},
	})
	toolEv1 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolCall && e.Tool != nil && e.Tool.ID == "call-1"
	})
	if toolEv1.Tool.Summary != "Running test" {
		t.Errorf("tool summary = %q, want 'Running test'", toolEv1.Tool.Summary)
	}

	// 4. Tool call update
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    "call-1",
			Name:          "run_command",
			Status:        "completed",
			RawOutput:     json.RawMessage(`"ok"`),
		},
	})
	toolEv2 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolResult && e.Tool != nil && e.Tool.ID == "call-1"
	})
	if toolEv2.Tool.Status != domain.ToolCompleted {
		t.Errorf("tool status = %q, want completed", toolEv2.Tool.Status)
	}

	// 5. Message chunk 1 after tool call -> must get segmentSeq = 2 (turn-1:msg:2)
	msg1, _ := json.Marshal(acpTextContent{Type: "text", Text: "Hello user!"})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       msg1,
		},
	})
	msgEv1 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventAgentMessage && e.Text == "Hello user!"
	})
	if msgEv1.ItemID != "turn-1:msg:2" {
		t.Fatalf("msg 1 ItemID = %q, want turn-1:msg:2", msgEv1.ItemID)
	}

	// 6. Message chunk 2 (same segment)
	msg2, _ := json.Marshal(acpTextContent{Type: "text", Text: " More text."})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       msg2,
		},
	})
	msgEv2 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventAgentMessage && e.Text == " More text."
	})
	if msgEv2.ItemID != "turn-1:msg:2" {
		t.Fatalf("msg 2 ItemID = %q, want turn-1:msg:2", msgEv2.ItemID)
	}

	// 7. Another tool call -> resets activeKind
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "call-2",
			Name:          "run_command",
			Status:        "completed",
		},
	})
	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolCall && e.Tool != nil && e.Tool.ID == "call-2"
	})

	// 8. Message chunk after second tool call -> segmentSeq = 3 (turn-1:msg:3)
	msg3, _ := json.Marshal(acpTextContent{Type: "text", Text: "Done now."})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       msg3,
		},
	})
	msgEv3 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventAgentMessage && e.Text == "Done now."
	})
	if msgEv3.ItemID != "turn-1:msg:3" {
		t.Fatalf("msg 3 ItemID = %q, want turn-1:msg:3", msgEv3.ItemID)
	}
}

func TestToolPathNormalizationAndLocations(t *testing.T) {
	_, s, serverConn, rec := newTestRig(t, domain.PermissionBypass)
	s.turnID = "turn-1"

	// 1. Path in Locations, input empty
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "call-loc",
			Name:          "read_file",
			Locations:     []acpLocation{{Path: "/C:/Users/PC/file.go", Line: 42}},
		},
	})
	ev1 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolCall && e.Tool != nil && e.Tool.ID == "call-loc"
	})
	if ev1.Tool.Input["path"] != "C:/Users/PC/file.go" {
		t.Errorf("tool.Input[path] = %v, want C:/Users/PC/file.go", ev1.Tool.Input["path"])
	}

	// 2. Path in Args with /D:/...
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "call-args",
			Name:          "edit_file",
			Args:          map[string]any{"target_file": "/D:/data/main.rs"},
		},
	})
	ev2 := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolCall && e.Tool != nil && e.Tool.ID == "call-args"
	})
	if ev2.Tool.Input["target_file"] != "D:/data/main.rs" {
		t.Errorf("tool.Input[target_file] = %v, want D:/data/main.rs", ev2.Tool.Input["target_file"])
	}
}

func TestNativeDiffExtraction(t *testing.T) {
	_, s, serverConn, rec := newTestRig(t, domain.PermissionBypass)
	s.turnID = "turn-1"

	content, _ := json.Marshal([]acpToolContentItem{
		{
			Type:    "diff",
			Path:    "/C:/repo/pkg/code.go",
			OldText: "func Old() {}",
			NewText: "func New() {}",
		},
	})

	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    "call-diff",
			Name:          "edit_file",
			Status:        "completed",
			Content:       content,
		},
	})

	ev := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Tool != nil && e.Tool.ID == "call-diff" && len(e.Tool.Diffs) > 0
	})
	diff := ev.Tool.Diffs[0]
	if diff.Path != "C:/repo/pkg/code.go" {
		t.Errorf("diff.Path = %q, want C:/repo/pkg/code.go", diff.Path)
	}
	if diff.OldText != "func Old() {}" || diff.NewText != "func New() {}" {
		t.Errorf("diff text mismatch: %+v", diff)
	}
}

func TestHiddenToolsSkipped(t *testing.T) {
	_, s, serverConn, rec := newTestRig(t, domain.PermissionBypass)
	s.turnID = "turn-1"

	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "task-1",
			Name:          "manage_task",
			Status:        "completed",
		},
	})
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "sched-1",
			Name:          "schedule",
			Status:        "completed",
		},
	})

	time.Sleep(100 * time.Millisecond)
	for _, e := range rec.all() {
		if e.Tool != nil && (e.Tool.Name == "manage_task" || e.Tool.Name == "schedule") {
			t.Fatalf("hidden tool was emitted: %+v", e.Tool)
		}
	}
}

func TestUsageAndPlanUpdates(t *testing.T) {
	_, s, serverConn, rec := newTestRig(t, domain.PermissionBypass)
	s.turnID = "turn-1"

	// Usage update
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "usage_update",
			Used:          4500,
			Size:          200000,
		},
	})
	usageEv := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventUsage && e.Usage != nil && e.Usage.ContextTokens == 4500
	})
	if usageEv.Usage.ContextWindow != 200000 {
		t.Errorf("ContextWindow = %d, want 200000", usageEv.Usage.ContextWindow)
	}
	s.mu.Lock()
	ctxWin := s.contextWindow
	s.mu.Unlock()
	if ctxWin != 200000 {
		t.Errorf("session.contextWindow = %d, want 200000", ctxWin)
	}

	// Plan update
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-test",
		Update: acpSessionUpdate{
			SessionUpdate: "plan",
			Entries: []domain.PlanEntry{
				{Content: "Design database", Status: "completed"},
				{Content: "Write tests", Status: "in_progress"},
			},
		},
	})
	planEv := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventPlan && len(e.Plan) == 2
	})
	if planEv.Plan[0].Content != "Design database" || planEv.Plan[1].Status != "in_progress" {
		t.Errorf("unexpected plan: %+v", planEv.Plan)
	}
}
