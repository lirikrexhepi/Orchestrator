package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/provider"
)

type stubAdapter struct {
	emit          provider.Emitter
	omitCwd       bool
	startedInputs []domain.SessionStartInput
}

func (a *stubAdapter) Driver() domain.DriverKind           { return domain.DriverClaude }
func (a *stubAdapter) Capabilities() provider.Capabilities { return provider.Capabilities{} }
func (a *stubAdapter) StartSession(_ context.Context, in domain.SessionStartInput) (domain.Session, error) {
	a.startedInputs = append(a.startedInputs, in)
	cwd := in.Cwd
	if a.omitCwd {
		cwd = ""
	}
	return domain.Session{ThreadID: in.ThreadID, Driver: domain.DriverClaude, Cwd: cwd}, nil
}
func (a *stubAdapter) SendTurn(context.Context, domain.SendTurnInput) error        { return nil }
func (a *stubAdapter) InterruptTurn(context.Context, string) error                { return nil }
func (a *stubAdapter) RespondToApproval(context.Context, string, string, domain.ApprovalDecision) error {
	return nil
}
func (a *stubAdapter) RespondToQuestion(context.Context, string, string, []string) error {
	return nil
}
func (a *stubAdapter) HasSession(string) bool                  { return true }
func (a *stubAdapter) StopSession(context.Context, string) error { return nil }
func (a *stubAdapter) StopAll(context.Context) error             { return nil }

func TestBusSlowSubscriberDoesNotBlockSubscribeOrUnsubscribe(t *testing.T) {
	bus := NewBus()
	slowCh, slowUnsub := bus.Subscribe()
	defer slowUnsub()

	// Fill slow subscriber buffer completely so next control event hits controlSendTimeout.
	for i := 0; i < subscriberBuffer; i++ {
		bus.Publish(domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: "t1", Delta: true, Text: "x"})
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.Publish(domain.RuntimeEvent{Kind: domain.EventTurnCompleted, ThreadID: "t1"})
	}()

	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	fastCh, fastUnsub := bus.Subscribe()
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Subscribe blocked by slow subscriber for %v", elapsed)
	}
	fastUnsub()

	// Unsubscribing the blocked slow subscriber must wake Publish immediately.
	start = time.Now()
	slowUnsub()
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Unsubscribe blocked for %v", elapsed)
	}
	wg.Wait()
	_ = slowCh
	_ = fastCh
}

func TestManagerPreservesInputCwdAndSnapshotOrdering(t *testing.T) {
	reg := provider.NewRegistry()
	m := NewManager(reg)
	stub := &stubAdapter{omitCwd: true}
	m.adapters[adapterKey{driver: domain.DriverClaude, account: domain.DefaultAccountID}] = stub

	dir := t.TempDir()
	sess, err := m.Start(context.Background(), domain.DriverClaude, domain.SessionStartInput{
		ThreadID: "th-1",
		Cwd:      dir,
		Model:    "sonnet",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sess.Cwd != dir {
		t.Fatalf("expected session.Cwd=%q, got %q", dir, sess.Cwd)
	}

	m.record(domain.RuntimeEvent{Kind: domain.EventUserMessage, ThreadID: "th-1", Text: "hi"})
	m.record(domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: "th-1", TurnID: "1", ItemID: "i1", Delta: true, Text: "a"})
	m.record(domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: "th-1", TurnID: "1", ItemID: "i1", Delta: true, Text: "b"})

	events, lastSeq := m.HistorySnapshot("th-1")
	if len(events) != 2 {
		t.Fatalf("expected 2 merged events, got %d", len(events))
	}
	if lastSeq != events[1].Seq || lastSeq < 3 {
		t.Fatalf("unexpected lastSeq=%d for events=%+v", lastSeq, events)
	}
}

func TestSpawnerAndCoordinatorRequireExplicitCwd(t *testing.T) {
	m := NewManager(provider.NewRegistry())
	w := NewWorkspaces()
	sp := NewSpawner(m, w)
	if _, err := sp.Spawn(context.Background(), []SpawnRequest{{Title: "x", Prompt: "go"}}, SpawnOptions{Driver: domain.DriverClaude}); err == nil {
		t.Fatal("expected Spawner.Spawn with empty Cwd to return an error")
	}

	coord := NewCoordinator(m)
	if _, err := coord.Send(context.Background(), Config{Driver: string(domain.DriverClaude)}, "hello"); err == nil {
		t.Fatal("expected Coordinator.Send with empty Cwd to return an error")
	}
}

func TestWorkspacesListSortsByUpdatedAtDesc(t *testing.T) {
	w := NewWorkspaces()
	w1 := w.Create("old", "p1", "/tmp/a")
	w1.CreatedAt = 100
	w1.UpdatedAt = 500
	w2 := w.Create("new-created", "p2", "/tmp/b")
	w2.CreatedAt = 300
	w2.UpdatedAt = 300

	list := w.List()
	if len(list) != 2 || list[0].ID != w1.ID {
		t.Fatalf("expected w1 (UpdatedAt=500) first, got %+v", list)
	}
}
