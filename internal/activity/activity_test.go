package activity

import (
	"context"
	"testing"
	"time"

	"composer/internal/domain"
)

type memorySink struct {
	rows []Row
}

func (m *memorySink) AddActivity(_ context.Context, rows []Row) error {
	m.rows = append(m.rows, rows...)
	return nil
}

func total(rows []Row) Counts {
	var c Counts
	for _, row := range rows {
		c.add(row.Counts)
	}
	return c
}

func TestTrackerCountsDeltasAndDedupesTools(t *testing.T) {
	sink := &memorySink{}
	tracker := NewTracker(sink, func(string) Thread {
		return Thread{Project: "/work/app", Driver: "claude", Model: "opus"}
	})
	at := time.Now().UnixMilli() + 1000
	events := []domain.RuntimeEvent{
		{Kind: domain.EventSessionStarted, ThreadID: "t1", At: at},
		{Kind: domain.EventUserMessage, ThreadID: "t1", At: at},
		{Kind: domain.EventToolCall, ThreadID: "t1", TurnID: "u1", At: at, Tool: &domain.ToolCall{ID: "x", Status: domain.ToolPending}},
		{Kind: domain.EventToolCall, ThreadID: "t1", TurnID: "u1", At: at, Tool: &domain.ToolCall{ID: "x"}},
		{Kind: domain.EventToolCall, ThreadID: "t1", TurnID: "u1", At: at, Tool: &domain.ToolCall{ID: "y"}},
		{Kind: domain.EventUsage, ThreadID: "t1", TurnID: "u1", At: at, Usage: &domain.Usage{InputTokens: 10, OutputTokens: 5}},
		{Kind: domain.EventUsage, ThreadID: "t1", TurnID: "u1", At: at, Usage: &domain.Usage{InputTokens: 30, OutputTokens: 15}},
		{Kind: domain.EventTurnCompleted, ThreadID: "t1", TurnID: "u1", At: at},
		{Kind: domain.EventUsage, ThreadID: "t1", TurnID: "u2", At: at, Usage: &domain.Usage{InputTokens: 4}},
		{Kind: domain.EventUserMessage, ThreadID: "t1", At: at - 60_000},
	}
	for _, event := range events {
		tracker.Observe(event)
	}
	if err := tracker.Close(); err != nil {
		t.Fatal(err)
	}

	got := total(sink.rows)
	want := Counts{Messages: 1, Sessions: 1, ToolCalls: 2, InputTokens: 34, OutputTokens: 15}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, row := range sink.rows {
		if row.Project != "/work/app" || row.Driver != "claude" || row.Model != "opus" {
			t.Fatalf("unexpected attribution %+v", row.Key)
		}
	}
}

func TestBuildWindowsStreaksAndShares(t *testing.T) {
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, time.Local)
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format(dayLayout) }
	rows := []Row{
		{Key: Key{Day: day(0), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 3, InputTokens: 100}},
		{Key: Key{Day: day(-1), Project: "/a", Driver: "codex", Model: "gpt"}, Counts: Counts{Messages: 2, OutputTokens: 300}},
		{Key: Key{Day: day(-2), Project: "/b", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 1, Sessions: 1}},
		{Key: Key{Day: day(-7), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 4}},
		{Key: Key{Day: day(-9), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 9}},
		{Key: Key{Day: day(-10), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 1}},
		{Key: Key{Day: day(-11), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 1}},
		{Key: Key{Day: day(-12), Project: "/a", Driver: "claude", Model: "opus"}, Counts: Counts{Messages: 1}},
	}

	o := Build(rows, now, "week", "")
	if o.Current.Messages != 6 || o.Current.Sessions != 1 || o.Current.Tokens != 400 {
		t.Fatalf("current %+v", o.Current)
	}
	if o.Previous.Messages != 13 {
		t.Fatalf("previous %+v", o.Previous)
	}
	if o.CurrentStreak != 3 || o.LongestStreak != 4 {
		t.Fatalf("streaks %d %d", o.CurrentStreak, o.LongestStreak)
	}
	if len(o.Providers) != 2 || o.Providers[0].Driver != "codex" {
		t.Fatalf("providers %+v", o.Providers)
	}
	if o.Days[len(o.Days)-1].Date != day(0) || o.Days[0].Date[:4] != "2025" || len(o.Days)%7 != 3 {
		t.Fatalf("grid from %s to %s (%d)", o.Days[0].Date, o.Days[len(o.Days)-1].Date, len(o.Days))
	}
	if o.YearMessages != 22 || o.BusiestMonth != "2026-10" {
		t.Fatalf("year %d month %s", o.YearMessages, o.BusiestMonth)
	}
	if len(o.Projects) != 2 || o.Projects[0].Path != "/a" {
		t.Fatalf("projects %+v", o.Projects)
	}

	scoped := Build(rows, now, "week", "/b")
	if scoped.Current.Messages != 1 || len(scoped.Projects) != 2 {
		t.Fatalf("scoped %+v", scoped.Current)
	}
}

func TestMonthWindowClampsToShorterPreviousMonth(t *testing.T) {
	today := time.Date(2026, 3, 31, 0, 0, 0, 0, time.Local)
	start, prevStart, prevEnd := windows(today, "month")
	if start != "2026-03-01" || prevStart != "2026-02-01" || prevEnd != "2026-02-28" {
		t.Fatalf("got %s %s %s", start, prevStart, prevEnd)
	}
}
