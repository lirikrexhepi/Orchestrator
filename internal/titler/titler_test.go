package titler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"Update CLIs and OpenCode v2":           "Update CLIs and OpenCode v2",
		"\"Furniture configurator for brands\"": "Furniture configurator for brands",
		"Title: Sofa builder.":                  "Sofa builder",
		"**Fix login redirect**\n\nextra":       "Fix login redirect",
		"  \n Update CLIs & OpenCode v2 Support": "Update CLIs & OpenCode v2 Support",
	}
	for raw, want := range cases {
		if got, ok := Clean(raw); !ok || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, bad := range []string{"", "   ", strings.Repeat("word ", 12), strings.Repeat("x", 90)} {
		if _, ok := Clean(bad); ok {
			t.Errorf("Clean(%q) should be rejected", bad)
		}
	}
}

func TestLooksAutomatic(t *testing.T) {
	prompt := "I need you to do a few things, first off update my CLIs"
	if !LooksAutomatic("I need you to do a few thing…", prompt) {
		t.Error("truncated prompt title must count as automatic")
	}
	if !LooksAutomatic("New chat", "") {
		t.Error("placeholder must count as automatic")
	}
	if LooksAutomatic("Add login", "Implement the login form") {
		t.Error("orchestrator task names are not automatic")
	}
}

func TestDue(t *testing.T) {
	if _, due := Due(State{Source: SourceUser, UserTurns: 50}); due {
		t.Error("a user rename must never be replaced")
	}
	if refine, due := Due(State{Title: "Fix bug…", Prompt: "Fix bug in login", UserTurns: 1}); !due || refine {
		t.Error("first message should get an initial title")
	}
	if _, due := Due(State{Source: SourceAI, TitleTurn: 1, UserTurns: 2}); due {
		t.Error("one message after titling is too soon")
	}
	if refine, due := Due(State{Source: SourceAI, TitleTurn: 1, UserTurns: 3}); !due || !refine {
		t.Error("early chats refine after two messages")
	}
	if _, due := Due(State{Source: SourceAI, TitleTurn: 40, UserTurns: 50}); due {
		t.Error("long chats refine rarely")
	}
	if _, due := Due(State{Source: SourceAI, TitleTurn: 40, UserTurns: 52}); !due {
		t.Error("long chats still refine eventually")
	}
}

func TestPromptStaysSmall(t *testing.T) {
	huge := strings.Repeat("furniture configurator sofa ", 20000)
	tracker := NewTracker()
	var recent []string
	for i := 0; i < 40; i++ {
		recent = tracker.Note("t", huge)
	}
	prompt := Prompt(Request{Current: "Implement furniture", First: huge, Recent: recent, Refine: true})
	if len(recent) != keepRecent {
		t.Fatalf("tracker kept %d messages", len(recent))
	}
	if len(prompt) > 6000 {
		t.Fatalf("prompt is %d bytes; it must stay bounded no matter how big the chat is", len(prompt))
	}
	if !strings.Contains(prompt, "Current title: Implement furniture") {
		t.Fatal("refine prompt must include the current title")
	}
}

func TestGenerateFallsBack(t *testing.T) {
	calls := []string{}
	backends := []Backend{
		{Name: "opencode", Run: func(ctx context.Context, prompt string) (string, error) {
			calls = append(calls, "opencode")
			return "", errors.New("down")
		}},
		{Name: "antigravity", Run: func(ctx context.Context, prompt string) (string, error) {
			calls = append(calls, "antigravity")
			return strings.Repeat("too long ", 20), nil
		}},
		{Name: "claude", Timeout: time.Second, Run: func(ctx context.Context, prompt string) (string, error) {
			calls = append(calls, "claude")
			return "Sofa configurator for brands", nil
		}},
	}
	title, from, err := Generate(context.Background(), backends, "p")
	if err != nil || title != "Sofa configurator for brands" || from != "claude" {
		t.Fatalf("got %q from %q: %v", title, from, err)
	}
	if strings.Join(calls, ",") != "opencode,antigravity,claude" {
		t.Fatalf("wrong order: %v", calls)
	}
	if _, _, err := Generate(context.Background(), backends[:1], "p"); err == nil {
		t.Fatal("all backends failing must be an error so the old title stays")
	}
}

func TestTrackerSingleFlight(t *testing.T) {
	tracker := NewTracker()
	if !tracker.Begin("a") || tracker.Begin("a") {
		t.Fatal("a thread must only title once at a time")
	}
	tracker.End("a")
	if !tracker.Begin("a") {
		t.Fatal("thread must be free again after End")
	}
}
