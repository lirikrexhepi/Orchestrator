package main

import (
	"testing"

	"composer/internal/domain"
)

func TestTurnToRemoveCountsLaterRepeats(t *testing.T) {
	events := []domain.RuntimeEvent{
		{Kind: domain.EventUserMessage, TurnID: "t1", Text: "continue"},
		{Kind: domain.EventAgentMessage, TurnID: "t1", Text: "continue working"},
		{Kind: domain.EventUserMessage, TurnID: "t2", Text: "something else"},
		{Kind: domain.EventUserMessage, TurnID: "t3", Text: "continue"},
		{Kind: domain.EventUserMessage, TurnID: "t4", Text: "continue"},
	}
	text, occurrence, ok := turnToRemove(events, "t1")
	if !ok || text != "continue" || occurrence != 2 {
		t.Fatalf("got %q %d %v", text, occurrence, ok)
	}
	if _, occurrence, _ := turnToRemove(events, "t4"); occurrence != 0 {
		t.Fatalf("latest repeat = %d", occurrence)
	}
	if _, _, ok := turnToRemove(events, "missing"); ok {
		t.Fatal("expected missing turn to be reported")
	}
}
