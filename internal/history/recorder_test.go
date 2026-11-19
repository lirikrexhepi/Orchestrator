package history

import (
	"testing"

	"composer/internal/domain"
)

// fakeCoordinator stands in for the live orchestrator conversation.
type fakeCoordinator struct {
	events   []domain.RuntimeEvent
	recorder *Recorder
	bound    []string
}

func (f *fakeCoordinator) BindWorkspace(workspaceID string) string {
	threadID := "coordinator-" + workspaceID
	f.bound = append(f.bound, workspaceID)

	stamped := make([]domain.RuntimeEvent, 0, len(f.events))
	for _, event := range f.events {
		event.ThreadID = threadID
		stamped = append(stamped, event)
	}
	f.recorder.RecordCoordinator(workspaceID, threadID, stamped)
	f.events = nil
	return threadID
}

func TestSessionStoresOrchestratorAndEveryAgent(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)
	defer recorder.Close()

	// The user talks to the orchestrator, which produces a plan.
	coordinator := &fakeCoordinator{
		recorder: recorder,
		events: []domain.RuntimeEvent{
			{Kind: domain.EventAgentMessage, Text: "fix both parcel bugs", Seq: 1},
			{Kind: domain.EventAgentMessage, Text: "here is the plan", Seq: 2},
		},
	}
	tracker := NewTracker(recorder, coordinator)

	workspace := domain.Workspace{ID: "ws-1", Title: "Parcel bugs", Cwd: "C:/proj", UpdatedAt: 5}
	coordinatorThread := tracker.BindCoordinator(workspace.ID)
	tracker.OpenWorkspace(workspace, coordinatorThread)

	// Two agents are spawned from that one plan.
	for _, task := range []domain.Task{
		{ID: "t-1", WorkspaceID: "ws-1", ThreadID: "thread-a", Title: "PHA-1015", Driver: domain.DriverClaude},
		{ID: "t-2", WorkspaceID: "ws-1", ThreadID: "thread-b", Title: "PHA-1016", Driver: domain.DriverClaude},
	} {
		tracker.TrackTask(task)
	}

	recorder.Record("thread-a", domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: "thread-a", Text: "fixed grass", Seq: 3})
	recorder.Record("thread-b", domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: "thread-b", Text: "fixed walls", Seq: 4})
	recorder.NoteProviderSession("thread-a", "claude-abc")
	recorder.NoteProviderSession("thread-b", "claude-def")
	recorder.Record("thread-a", domain.RuntimeEvent{Kind: domain.EventTurnCompleted, ThreadID: "thread-a", Seq: 5})

	loaded, err := store.Load("ws-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The requirement: one session carries the orchestrator plus every agent.
	if len(loaded.Transcripts) != 3 {
		t.Fatalf("expected orchestrator + 2 agents = 3 transcripts, got %d: %v",
			len(loaded.Transcripts), keys(loaded.Transcripts))
	}
	orchestrator := loaded.Transcripts[coordinatorThread]
	if len(orchestrator) != 2 {
		t.Errorf("orchestrator transcript = %d events, want 2", len(orchestrator))
	}
	if len(loaded.Meta.Tasks) != 2 {
		t.Errorf("tasks = %d, want 2", len(loaded.Meta.Tasks))
	}
	if loaded.Meta.Resume["thread-a"] != "claude-abc" {
		t.Errorf("resume id for thread-a = %q", loaded.Meta.Resume["thread-a"])
	}
}

func TestSecondSpawnGetsItsOwnSession(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)
	defer recorder.Close()

	coordinator := &fakeCoordinator{
		recorder: recorder,
		events:   []domain.RuntimeEvent{{Kind: domain.EventAgentMessage, Text: "first request", Seq: 1}},
	}
	tracker := NewTracker(recorder, coordinator)

	first := domain.Workspace{ID: "ws-1", Title: "First", UpdatedAt: 1}
	tracker.OpenWorkspace(first, tracker.BindCoordinator(first.ID))
	tracker.TrackTask(domain.Task{WorkspaceID: "ws-1", ThreadID: "thread-a"})

	// A later, unrelated request must not inherit the first conversation.
	coordinator.events = []domain.RuntimeEvent{{Kind: domain.EventAgentMessage, Text: "second request", Seq: 2}}
	second := domain.Workspace{ID: "ws-2", Title: "Second", UpdatedAt: 2}
	tracker.OpenWorkspace(second, tracker.BindCoordinator(second.ID))
	tracker.TrackTask(domain.Task{WorkspaceID: "ws-2", ThreadID: "thread-b"})

	one, err := store.Load("ws-1")
	if err != nil {
		t.Fatalf("load ws-1: %v", err)
	}
	two, err := store.Load("ws-2")
	if err != nil {
		t.Fatalf("load ws-2: %v", err)
	}

	firstText := one.Transcripts["coordinator-ws-1"]
	secondText := two.Transcripts["coordinator-ws-2"]
	if len(firstText) != 1 || firstText[0].Text != "first request" {
		t.Errorf("ws-1 orchestrator = %+v", firstText)
	}
	if len(secondText) != 1 || secondText[0].Text != "second request" {
		t.Errorf("ws-2 orchestrator should not repeat the first conversation, got %+v", secondText)
	}
}

func TestEventsOnUnknownThreadsAreDropped(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)
	defer recorder.Close()

	// The live coordinator thread belongs to no workspace until a spawn claims
	// it; recording it loose would create a directory List cannot describe.
	recorder.Record("coordinator", domain.RuntimeEvent{Kind: domain.EventAgentMessage, Text: "chatter"})
	recorder.Record("stray-thread", domain.RuntimeEvent{Kind: domain.EventAgentMessage, Text: "orphan"})

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no workspaces, got %d", len(list))
	}
}

func TestTaskStateUpdatesSurviveReload(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)

	recorder.OpenWorkspace(domain.Workspace{ID: "ws-1"}, "coordinator-ws-1")
	recorder.TrackTask(domain.Task{WorkspaceID: "ws-1", ThreadID: "thread-a", State: domain.TaskRunning})
	recorder.UpdateTaskState("thread-a", domain.TaskComplete, "did the thing")
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened := New(store.Root())
	defer reopened.Close()
	meta, err := reopened.LoadMeta("ws-1")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if len(meta.Tasks) != 1 {
		t.Fatalf("tasks = %d", len(meta.Tasks))
	}
	if meta.Tasks[0].State != domain.TaskComplete {
		t.Errorf("state = %q, want complete", meta.Tasks[0].State)
	}
	if meta.Tasks[0].Summary != "did the thing" {
		t.Errorf("summary = %q", meta.Tasks[0].Summary)
	}
}

func TestAppendingMessageToOlderChatMovesItToIndexZero(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)
	defer recorder.Close()

	// ws-1 was created earlier (UpdatedAt: 100)
	recorder.OpenWorkspace(domain.Workspace{ID: "ws-1", Title: "Old Chat", UpdatedAt: 100}, "coord-1")
	recorder.TrackTask(domain.Task{WorkspaceID: "ws-1", ThreadID: "thread-1", State: domain.TaskRunning})

	// ws-2 was created later (UpdatedAt: 200)
	recorder.OpenWorkspace(domain.Workspace{ID: "ws-2", Title: "New Chat", UpdatedAt: 200}, "coord-2")
	recorder.TrackTask(domain.Task{WorkspaceID: "ws-2", ThreadID: "thread-2", State: domain.TaskRunning})

	// ws-sub is a subagent (Hidden/Subagent: true)
	recorder.OpenWorkspace(domain.Workspace{ID: "ws-sub", Title: "Subagent Task", Subagent: true, Hidden: true, UpdatedAt: 300}, "coord-sub")
	recorder.TrackTask(domain.Task{WorkspaceID: "ws-sub", ThreadID: "thread-sub", Subagent: true, Hidden: true, State: domain.TaskRunning})

	// Initial list: ws-2 should be first, ws-sub should be excluded entirely
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 visible chats (subagent excluded), got %d", len(list))
	}
	if list[0].Workspace.ID != "ws-2" {
		t.Fatalf("expected ws-2 first, got %s", list[0].Workspace.ID)
	}

	// Now append a new user message to older chat ws-1 at timestamp 500
	recorder.Record("thread-1", domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: "thread-1",
		Text:     "Resuming older chat",
		At:       500,
	})

	// After recording, ws-1 should jump to index 0 with UpdatedAt 500
	list2, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list2) != 2 {
		t.Fatalf("expected 2 visible chats, got %d", len(list2))
	}
	if list2[0].Workspace.ID != "ws-1" {
		t.Fatalf("expected ws-1 at index 0 after new message, got %s", list2[0].Workspace.ID)
	}
	if list2[0].Workspace.UpdatedAt != 500 {
		t.Fatalf("expected ws-1 UpdatedAt 500, got %d", list2[0].Workspace.UpdatedAt)
	}
}

func keys(m map[string][]domain.RuntimeEvent) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
