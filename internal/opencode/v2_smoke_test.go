package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/provider"
)

type recorder struct {
	mu     sync.Mutex
	events []domain.RuntimeEvent
	signal chan struct{}
}

func (r *recorder) Emit(event domain.RuntimeEvent) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
	select {
	case r.signal <- struct{}{}:
	default:
	}
}

func (r *recorder) waitFor(t *testing.T, timeout time.Duration, match func(domain.RuntimeEvent) bool) domain.RuntimeEvent {
	t.Helper()
	deadline := time.After(timeout)
	seen := 0
	for {
		r.mu.Lock()
		for ; seen < len(r.events); seen++ {
			if match(r.events[seen]) {
				event := r.events[seen]
				r.mu.Unlock()
				return event
			}
		}
		r.mu.Unlock()
		select {
		case <-r.signal:
		case <-time.After(200 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for event")
		}
	}
}

func (r *recorder) text(kind domain.EventKind, turnID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out strings.Builder
	for _, event := range r.events {
		if event.Kind == kind && event.TurnID == turnID {
			out.WriteString(event.Text)
		}
	}
	return out.String()
}

func TestV2LiveRoundTrip(t *testing.T) {
	bin := os.Getenv("OPENCODE_V2_BIN")
	if bin == "" || testing.Short() {
		t.Skip("set OPENCODE_V2_BIN to run the live OpenCode 2 smoke test")
	}
	model := os.Getenv("OPENCODE_V2_MODEL")
	if model == "" {
		model = "opencode/big-pickle"
	}
	settings := domain.ProviderSettings{BinaryPath: bin}
	if MajorVersion(context.Background(), settings) < 2 {
		t.Fatalf("%s is not OpenCode 2", bin)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("the secret word is tangerine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recorder{signal: make(chan struct{}, 1)}
	router := NewRouter(settings, rec)
	defer router.StopAll(context.Background())
	var _ provider.Adapter = router

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	session, err := router.StartSession(ctx, domain.SessionStartInput{
		ThreadID: "v2-smoke", Cwd: dir, Model: model, Permission: domain.PermissionDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.ProviderSessionID, "ses") {
		t.Fatalf("unexpected session id %q", session.ProviderSessionID)
	}

	if err := router.SendTurn(ctx, domain.SendTurnInput{ThreadID: "v2-smoke", TurnID: "t1", Text: "Use your read tool to read note.txt, then tell me the secret word in one short sentence."}); err != nil {
		t.Fatal(err)
	}
	done := rec.waitFor(t, 3*time.Minute, func(e domain.RuntimeEvent) bool {
		return e.TurnID == "t1" && (e.Kind == domain.EventTurnCompleted || e.Kind == domain.EventTurnFailed)
	})
	if done.Kind != domain.EventTurnCompleted {
		t.Fatalf("turn failed: %s", done.Error)
	}
	rec.waitFor(t, time.Second, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolResult && e.Tool != nil && e.Tool.Name == "read" && strings.Contains(e.Tool.Output, "tangerine")
	})
	rec.waitFor(t, time.Second, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventUsage && e.Usage != nil && e.Usage.ContextTokens > 0
	})
	if reply := rec.text(domain.EventAgentMessage, "t1"); !strings.Contains(strings.ToLower(reply), "tangerine") {
		t.Fatalf("reply missing secret word: %q", reply)
	}

	if err := router.SendTurn(ctx, domain.SendTurnInput{ThreadID: "v2-smoke", TurnID: "t2", Text: "Ask me with your question tool whether I prefer tea or coffee. Do not assume an answer."}); err != nil {
		t.Fatal(err)
	}
	asked := rec.waitFor(t, 3*time.Minute, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventQuestionAsked || (e.TurnID == "t2" && e.Kind == domain.EventTurnCompleted)
	})
	if asked.Kind != domain.EventQuestionAsked {
		t.Fatal("model finished without asking")
	}
	answer := asked.Question.Questions[0].Options[0]
	if err := router.RespondToQuestion(ctx, "v2-smoke", asked.Question.RequestID, []string{answer}); err != nil {
		t.Fatal(err)
	}
	finished := rec.waitFor(t, 3*time.Minute, func(e domain.RuntimeEvent) bool {
		return e.TurnID == "t2" && (e.Kind == domain.EventTurnCompleted || e.Kind == domain.EventTurnFailed)
	})
	if finished.Kind != domain.EventTurnCompleted {
		t.Fatalf("question turn failed: %s", finished.Error)
	}

	if err := router.SendTurn(ctx, domain.SendTurnInput{ThreadID: "v2-smoke", TurnID: "t3", Text: "Count from 1 to 400, one number per line."}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 2*time.Minute, func(e domain.RuntimeEvent) bool {
		return e.TurnID == "t3" && (e.Kind == domain.EventAgentMessage || e.Kind == domain.EventAgentThought)
	})
	if err := router.InterruptTurn(ctx, "v2-smoke"); err != nil {
		t.Fatal(err)
	}
	stopped := rec.waitFor(t, time.Minute, func(e domain.RuntimeEvent) bool {
		return e.TurnID == "t3" && (e.Kind == domain.EventTurnCompleted || e.Kind == domain.EventTurnFailed)
	})
	if stopped.StopReason != domain.StopCancelled {
		t.Fatalf("interrupt ended with %q", stopped.StopReason)
	}

	if err := router.StopSession(ctx, "v2-smoke"); err != nil {
		t.Fatal(err)
	}

	resumed, err := router.StartSession(ctx, domain.SessionStartInput{
		ThreadID: "v2-resume", Cwd: dir, Model: model, Resume: session.ProviderSessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ProviderSessionID != session.ProviderSessionID {
		t.Fatalf("resume opened %s, want %s", resumed.ProviderSessionID, session.ProviderSessionID)
	}

	models, err := ListModels(ctx, settings)
	if err != nil || len(models) == 0 {
		t.Fatalf("model catalog: %v (%d models)", err, len(models))
	}
}
