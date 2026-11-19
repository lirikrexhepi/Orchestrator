package session

import (
	"strings"
	"testing"

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
