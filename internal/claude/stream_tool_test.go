package claude

import (
	"testing"

	"composer/internal/domain"
)

func TestStreamToolUseStartAndAssistantMerge(t *testing.T) {
	emit := &captureEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := newContextSession()

	// 1. stream_event (content_block_start for Write) immediately emits an in-progress EventToolCall with ID and Name.
	feed(t, a, s,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_write_1","name":"Write"}}}`,
	)

	if len(emit.events) != 1 {
		t.Fatalf("expected 1 event after content_block_start for Write, got %d", len(emit.events))
	}
	ev1 := emit.events[0]
	if ev1.Kind != domain.EventToolCall {
		t.Fatalf("expected EventToolCall, got %s", ev1.Kind)
	}
	if ev1.ItemID != "toolu_write_1" {
		t.Errorf("ItemID = %q, want 'toolu_write_1'", ev1.ItemID)
	}
	if ev1.Tool == nil {
		t.Fatalf("Tool is nil")
	}
	if ev1.Tool.ID != "toolu_write_1" {
		t.Errorf("Tool.ID = %q, want 'toolu_write_1'", ev1.Tool.ID)
	}
	if ev1.Tool.Name != "Write" {
		t.Errorf("Tool.Name = %q, want 'Write'", ev1.Tool.Name)
	}
	if ev1.Tool.Status != domain.ToolInProgress {
		t.Errorf("Tool.Status = %v, want ToolInProgress", ev1.Tool.Status)
	}

	// 2. stream_event (content_block_start for AskUserQuestion) is suppressed and marked hidden.
	feed(t, a, s,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_ask_1","name":"AskUserQuestion"}}}`,
	)

	if len(emit.events) != 1 {
		t.Fatalf("expected AskUserQuestion to be suppressed, but events grew to %d", len(emit.events))
	}
	s.mu.Lock()
	hidden := s.hiddenTools["toolu_ask_1"]
	s.mu.Unlock()
	if !hidden {
		t.Errorf("expected toolu_ask_1 to be marked hidden")
	}

	// 3. The subsequent assistant frame emits the populated EventToolCall with the same ID and full Input.
	feed(t, a, s,
		`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"tool_use","id":"toolu_write_1","name":"Write","input":{"path":"main.go","content":"package main"}},{"type":"tool_use","id":"toolu_ask_1","name":"AskUserQuestion","input":{"questions":["proceed?"]}}]}}`,
	)

	if len(emit.events) != 2 {
		t.Fatalf("expected 2 events after assistant frame, got %d", len(emit.events))
	}
	ev2 := emit.events[1]
	if ev2.Kind != domain.EventToolCall {
		t.Fatalf("expected EventToolCall for Write in assistant frame, got %s", ev2.Kind)
	}
	if ev2.ItemID != "toolu_write_1" {
		t.Errorf("ItemID = %q, want 'toolu_write_1'", ev2.ItemID)
	}
	if ev2.Tool == nil {
		t.Fatalf("Tool is nil")
	}
	if ev2.Tool.ID != "toolu_write_1" {
		t.Errorf("Tool.ID = %q, want 'toolu_write_1'", ev2.Tool.ID)
	}
	if ev2.Tool.Name != "Write" {
		t.Errorf("Tool.Name = %q, want 'Write'", ev2.Tool.Name)
	}
	if ev2.Tool.Status != domain.ToolInProgress {
		t.Errorf("Tool.Status = %v, want ToolInProgress", ev2.Tool.Status)
	}
	if ev2.Tool.Input == nil {
		t.Fatalf("Tool.Input is nil, expected populated input")
	}
	if path, ok := ev2.Tool.Input["path"].(string); !ok || path != "main.go" {
		t.Errorf("Tool.Input[path] = %v, want 'main.go'", ev2.Tool.Input["path"])
	}
}
