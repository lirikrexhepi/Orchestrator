package domain

import (
	"strings"
	"testing"
)

func kept(events []RuntimeEvent, turnID string) string {
	drop := ExchangeMask(events, turnID)
	var out []string
	for i, event := range events {
		if !drop[i] {
			out = append(out, event.Text)
		}
	}
	return strings.Join(out, ",")
}

func TestExchangeMaskRemovesUntaggedReplies(t *testing.T) {
	events := []RuntimeEvent{
		{Kind: EventUserMessage, TurnID: "t1", Text: "first"},
		{Kind: EventAgentMessage, TurnID: "t1", Text: "reply1"},
		{Kind: EventToolCall, TurnID: "t1", Text: "bg", Tool: &ToolCall{ID: "agent-old"}},
		{Kind: EventUserMessage, TurnID: "t2", Text: "wrong chat"},
		{Kind: EventTurnStarted, TurnID: "t2", Text: "started"},
		{Kind: EventSessionStarted, Text: "session"},
		{Kind: EventAgentThought, Text: "untagged thought"},
		{Kind: EventAgentMessage, Text: "untagged reply"},
		{Kind: EventToolCall, Text: "spawn", Tool: &ToolCall{ID: "agent-new"}},
		{Kind: EventSubagentMessage, ParentToolID: "agent-new", Text: "new sub"},
		{Kind: EventSubagentMessage, ParentToolID: "agent-old", Text: "old sub"},
		{Kind: EventNotice, Text: "notice"},
		{Kind: EventTurnCompleted, Text: "done"},
		{Kind: EventUserMessage, TurnID: "t3", Text: "next"},
		{Kind: EventAgentMessage, Text: "untagged reply to next"},
		{Kind: EventSubagentMessage, ParentToolID: "agent-new", Text: "late new sub"},
	}
	got := kept(events, "t2")
	want := "first,reply1,bg,session,old sub,notice,next,untagged reply to next"
	if got != want {
		t.Fatalf("kept = %s\nwant  %s", got, want)
	}
}

func TestExchangeMaskLeavesOtherTurnsAlone(t *testing.T) {
	events := []RuntimeEvent{
		{Kind: EventUserMessage, TurnID: "t1", Text: "a"},
		{Kind: EventAgentMessage, TurnID: "t1", Text: "b"},
		{Kind: EventUserMessage, TurnID: "t2", Text: "c"},
		{Kind: EventAgentMessage, TurnID: "t1", Text: "late tagged t1"},
	}
	if got := kept(events, "t2"); got != "a,b,late tagged t1" {
		t.Fatalf("kept = %s", got)
	}
	if got := kept(events, "missing"); got != "a,b,c,late tagged t1" {
		t.Fatalf("unknown turn removed something: %s", got)
	}
}
