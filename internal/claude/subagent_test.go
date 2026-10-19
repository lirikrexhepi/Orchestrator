package claude

import (
	"testing"

	"composer/internal/domain"
)

func TestSubagentFramesCarryTheirParentToolID(t *testing.T) {
	emit := &captureEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := newContextSession()

	feed(t, a, s,
		`{"type":"assistant","parent_tool_use_id":"toolu_task","uuid":"u1","message":{"id":"m1","content":[{"type":"text","text":"looking"},{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"a.go"}}]}}`,
		`{"type":"user","parent_tool_use_id":"toolu_task","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"package a"}]}}`,
	)

	want := []domain.EventKind{domain.EventSubagentMessage, domain.EventSubagentToolCall, domain.EventSubagentToolDone}
	if len(emit.events) != len(want) {
		t.Fatalf("got %d events, want %d", len(emit.events), len(want))
	}
	for i, kind := range want {
		if emit.events[i].Kind != kind || emit.events[i].ParentToolID != "toolu_task" {
			t.Fatalf("event %d = %s parent=%q, want %s parent=toolu_task", i, emit.events[i].Kind, emit.events[i].ParentToolID, kind)
		}
	}
}

func TestAsyncAgentLaunchDoesNotEndTheSubagentAndNotificationReportsTotals(t *testing.T) {
	emit := &captureEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := newContextSession()

	feed(t, a, s,
		`{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"toolu_task","content":"Async agent launched"}]},"tool_use_result":{"isAsync":true,"status":"async_launched","agentId":"a1"}}`,
		`{"type":"system","subtype":"task_notification","task_id":"a1","tool_use_id":"toolu_task","status":"completed","summary":"all done","usage":{"total_tokens":10,"tool_uses":7,"duration_ms":125000}}`,
	)

	if len(emit.events) != 2 {
		t.Fatalf("got %d events, want 2", len(emit.events))
	}
	launch := emit.events[0].Tool
	if launch == nil || !launch.Async || launch.Output != "" {
		t.Fatalf("launch result = %+v, want async with no output", launch)
	}
	done := emit.events[1]
	if done.Kind != domain.EventToolResult || done.Tool == nil || done.Tool.ID != "toolu_task" {
		t.Fatalf("notification event = %+v", done)
	}
	if done.Tool.Async || done.Tool.ToolCount != 7 || done.Tool.DurationMs != 125000 || done.Tool.Status != domain.ToolCompleted {
		t.Fatalf("notification tool = %+v", done.Tool)
	}
}

func TestTaskProgressReportsLiveSubagentTokens(t *testing.T) {
	emit := &captureEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := newContextSession()

	feed(t, a, s,
		`{"type":"system","subtype":"task_progress","task_id":"a1","tool_use_id":"toolu_task","usage":{"total_tokens":20748,"tool_uses":1,"duration_ms":1811},"last_tool_name":"Bash"}`,
		`{"type":"system","subtype":"task_notification","task_id":"a1","tool_use_id":"toolu_task","status":"completed","summary":"ok","usage":{"total_tokens":22425,"tool_uses":2,"duration_ms":7217}}`,
	)

	if len(emit.events) != 2 {
		t.Fatalf("got %d events, want 2", len(emit.events))
	}
	progress := emit.events[0]
	if progress.Kind != domain.EventSubagentProgress || progress.ParentToolID != "toolu_task" || progress.Tool == nil {
		t.Fatalf("progress event = %+v", progress)
	}
	if progress.Tool.Tokens != 20748 || progress.Tool.ToolCount != 1 || progress.Tool.Status != domain.ToolInProgress {
		t.Fatalf("progress tool = %+v", progress.Tool)
	}
	if done := emit.events[1].Tool; done == nil || done.Tokens != 22425 {
		t.Fatalf("notification tool = %+v", done)
	}
}

func TestForegroundAgentResultCarriesReportedTotals(t *testing.T) {
	emit := &captureEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := newContextSession()

	feed(t, a, s,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_task","content":"report"}]},"tool_use_result":{"status":"completed","totalDurationMs":48851,"totalToolUseCount":11,"totalTokens":30500}}`,
	)

	tool := emit.events[0].Tool
	if tool.Async || tool.DurationMs != 48851 || tool.ToolCount != 11 || tool.Tokens != 30500 {
		t.Fatalf("tool = %+v", tool)
	}
}
