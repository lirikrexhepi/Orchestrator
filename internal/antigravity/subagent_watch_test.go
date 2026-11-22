package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"composer/internal/domain"
)

func TestParseInvokeSubagentOutput(t *testing.T) {
	raw := "Launched subagent.\n```json\n{\"conversationId\":\"child-1234-abcd\",\"logAbsoluteUri\":\"file:///C:/tmp/brain/child-1234-abcd/.system_generated/logs/transcript.jsonl\"}\n```"
	ref, ok := parseInvokeSubagentOutput(raw)
	if !ok {
		t.Fatal("expected parseInvokeSubagentOutput to succeed")
	}
	if ref.conversationID != "child-1234-abcd" {
		t.Fatalf("conversationID = %q, want child-1234-abcd", ref.conversationID)
	}
	if ref.logAbsoluteURI == "" {
		t.Fatal("expected logAbsoluteURI to be parsed")
	}
}

func TestWatchNativeSubagentStreamsStepsAndCompletesOnInboxMessage(t *testing.T) {
	brainRoot := t.TempDir()
	parentID := "parent-sess-1"
	childID := "child-conv-9999"

	childLogs := filepath.Join(brainRoot, childID, ".system_generated", "logs")
	parentMsgs := filepath.Join(brainRoot, parentID, ".system_generated", "messages")
	if err := os.MkdirAll(childLogs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(parentMsgs, 0o755); err != nil {
		t.Fatal(err)
	}

	transcriptPath := filepath.Join(childLogs, "transcript.jsonl")
	stepLine, _ := json.Marshal(map[string]any{
		"step_index": 1,
		"type":       "PLANNER_RESPONSE",
		"content":    "Reading adapter code",
		"tool_calls": []map[string]any{
			{"name": "read_file", "args": map[string]any{"Path": "adapter.go"}},
		},
	})
	if err := os.WriteFile(transcriptPath, append(stepLine, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &eventRecorder{}
	a := NewAdapter(domain.ProviderSettings{}, rec)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &session{
		threadID:        "thread-1",
		sessionID:       parentID,
		account:         domain.DefaultAccountID,
		procCtx:         ctx,
		nativeSubagents: make(map[string]bool),
	}

	logURI := "file:///" + filepath.ToSlash(transcriptPath)
	go a.watchNativeSubagent(ctx, s, "turn-1", "tool-invoke-1", childID, logURI, time.Now().Add(-time.Second))

	stepEv := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventSubagentToolCall && e.ParentToolID == "tool-invoke-1" && e.Tool != nil && e.Tool.Name == "read_file"
	})
	if stepEv.ParentToolID != "tool-invoke-1" {
		t.Fatalf("unexpected step parent tool id: %q", stepEv.ParentToolID)
	}

	// Write inbox message simulating background subagent completion.
	msgBytes, _ := json.Marshal(nativeInboxMessage{
		ID:        "msg-1",
		Sender:    childID,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Content:   "Audit finished with 3 fixes.",
	})
	if err := os.WriteFile(filepath.Join(parentMsgs, "msg-1.json"), msgBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	doneEv := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolResult && e.Tool != nil && e.Tool.ID == "tool-invoke-1" && !e.Tool.Async
	})
	if doneEv.Tool.Status != domain.ToolCompleted || doneEv.Tool.Output != "Audit finished with 3 fixes." {
		t.Fatalf("unexpected completion tool result: %+v", doneEv.Tool)
	}
}
