package session

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"composer/internal/domain"
)

func TestExtractMergesDeltaReplies(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventUserMessage, TurnID: "t1", Text: "remember ESPERANSA"},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "Understood! I'll remember ", Delta: true},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "the word.", Delta: true},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	got := ExtractConversationText(events, 0)
	if !strings.Contains(got, "ESPERANSA") {
		t.Errorf("user text missing: %q", got)
	}
	if !strings.Contains(got, "Understood! I'll remember the word.") {
		t.Errorf("merged deltas missing: %q", got)
	}
}

func TestExtractSkipsEchoedFinalFrame(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "HELLO", Delta: true},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "HELLO"},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	got := ExtractConversationText(events, 0)
	if count := strings.Count(got, "HELLO"); count != 1 {
		t.Errorf("echo duplicated %d times: %q", count, got)
	}
}

func TestExtractKeepsDistinctFinalResponse(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "Working on it...", Delta: true},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "Done: fixed the bug."},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	got := ExtractConversationText(events, 0)
	if !strings.Contains(got, "Working on it...") || !strings.Contains(got, "Done: fixed the bug.") {
		t.Errorf("distinct texts lost: %q", got)
	}
}

func TestExtractDoesNotDuplicateEmptyTurnDeltasAcrossFlushes(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventAgentMessage, Text: "Initial notice", Delta: true},
		{Kind: domain.EventTurnCompleted},
		{Kind: domain.EventToolResult, Tool: &domain.ToolCall{Output: "first result"}},
		{Kind: domain.EventAgentMessage, Text: "Second update", Delta: true},
		{Kind: domain.EventTurnCompleted},
		{Kind: domain.EventToolResult, Tool: &domain.ToolCall{Output: "second result"}},
	}
	got := ExtractConversationText(events, 0)
	if count := strings.Count(got, "Initial notice"); count != 1 {
		t.Errorf("initial notice duplicated %d times, want 1: %q", count, got)
	}
	if count := strings.Count(got, "Second update"); count != 1 {
		t.Errorf("second update duplicated %d times, want 1: %q", count, got)
	}
}

func TestExtractDeduplicatesConsecutiveIdenticalMessages(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventAgentMessage, Text: "You've hit your session limit"},
		{Kind: domain.EventTurnCompleted},
		{Kind: domain.EventAgentMessage, Text: "You've hit your session limit"},
		{Kind: domain.EventTurnCompleted},
		{Kind: domain.EventAgentMessage, Text: "You've hit your session limit"},
		{Kind: domain.EventTurnCompleted},
	}
	got := ExtractConversationText(events, 0)
	if count := strings.Count(got, "You've hit your session limit"); count != 1 {
		t.Errorf("session limit message duplicated %d times, want 1: %q", count, got)
	}
}

func TestExtractOmitsRawToolOutputs(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventUserMessage, TurnID: "t1", Text: "run ls"},
		{Kind: domain.EventToolResult, TurnID: "t1", Tool: &domain.ToolCall{
			Name:   "bash",
			Output: strings.Repeat("huge directory listing output\n", 200),
		}},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "Done."},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	got := ExtractConversationText(events, 0)
	if strings.Contains(got, "huge directory listing") {
		t.Fatalf("raw tool output was dumped into transcript: %q", got)
	}
	if !strings.Contains(got, "Done.") {
		t.Fatalf("agent message missing: %q", got)
	}
}

func TestExtractCompactModifiedFiles(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventUserMessage, TurnID: "t1", Text: "fix the bug"},
		{Kind: domain.EventToolCall, TurnID: "t1", Tool: &domain.ToolCall{
			Name:     "edit_file",
			Category: domain.ToolCategoryEdit,
			Target:   "internal/session/handoff.go",
		}},
		{Kind: domain.EventToolResult, TurnID: "t1", Tool: &domain.ToolCall{
			Name:     "edit_file",
			Category: domain.ToolCategoryEdit,
			Target:   "internal/session/handoff.go",
		}},
		{Kind: domain.EventToolCall, TurnID: "t1", Tool: &domain.ToolCall{
			Name:     "write_file",
			Category: domain.ToolCategoryWrite,
			Target:   "desktop/app_session.go",
		}},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "Files updated."},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	got := ExtractConversationText(events, 0)
	if !strings.Contains(got, "[Modified files: internal/session/handoff.go, desktop/app_session.go]") {
		t.Fatalf("expected compact modified files line, got: %q", got)
	}
}

func TestTruncateRunesSafeUTF8(t *testing.T) {
	multibyte := "こんにちは世界🌍🚀Antigravity"
	truncated := truncateRunes(multibyte, 6)
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated string is not valid utf-8: %q", truncated)
	}
	if utf8.RuneCountInString(truncated) != 6 {
		t.Fatalf("expected 6 runes, got %d: %q", utf8.RuneCountInString(truncated), truncated)
	}
	if truncated != "こんにちは世" {
		t.Fatalf("unexpected content: %q", truncated)
	}

	// Also verify ExtractConversationText truncates long multi-byte message safely
	longMultibyte := strings.Repeat("世界🌟", 2000)
	events := []domain.RuntimeEvent{
		{Kind: domain.EventUserMessage, TurnID: "t1", Text: longMultibyte},
		{Kind: domain.EventTurnCompleted, TurnID: "t1"},
	}
	extracted := ExtractConversationText(events, 0)
	if !utf8.ValidString(extracted) {
		t.Fatalf("extracted conversation text has invalid utf-8 bytes: %q", extracted)
	}
}

func TestBuildHandoffPromptForThreadIncludesReadThreadPointer(t *testing.T) {
	// Generate events exceeding maxHandoffChars (24000)
	var events []domain.RuntimeEvent
	for i := 0; i < 30; i++ {
		events = append(events,
			domain.RuntimeEvent{
				Kind:   domain.EventUserMessage,
				TurnID: fmt.Sprintf("t-%d", i),
				Text:   fmt.Sprintf("User turn %d with lots of details: %s", i, strings.Repeat("abcde ", 200)),
			},
			domain.RuntimeEvent{
				Kind:   domain.EventAgentMessage,
				TurnID: fmt.Sprintf("t-%d", i),
				Text:   fmt.Sprintf("Assistant reply %d with lots of details: %s", i, strings.Repeat("12345 ", 200)),
			},
			domain.RuntimeEvent{
				Kind:   domain.EventTurnCompleted,
				TurnID: fmt.Sprintf("t-%d", i),
			},
		)
	}

	prompt := BuildHandoffPromptForThread("claude", "antigravity", "thread-abc-123", events, "/workspace")
	if !strings.Contains(prompt, `[Earlier turns omitted. Call read_thread(threadId: "thread-abc-123") via the orchestrator MCP server if you need earlier details.]`) {
		t.Fatalf("expected read_thread pointer in handoff prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "This is a direct continuation of the same thread.") {
		t.Fatalf("expected direct continuation framing, got:\n%s", prompt)
	}
}
