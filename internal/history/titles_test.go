package history

import (
	"testing"

	"composer/internal/domain"
)

func TestTitles(t *testing.T) {
	store := New(t.TempDir())
	recorder := NewRecorder(store)
	recorder.OpenWorkspace(domain.Workspace{ID: "ws", Title: "I need you to do a few thing…", Prompt: "I need you to do a few things"}, "")
	recorder.TrackTask(domain.Task{WorkspaceID: "ws", ThreadID: "t1", Title: "I need you to do a few thing…", Prompt: "I need you to do a few things"})
	recorder.TrackTask(domain.Task{WorkspaceID: "ws", ThreadID: "t1-cont-2", Title: "I need you to do a few thing…"})

	state, ok := recorder.NoteUserTurn("t1-cont-2")
	if !ok || state.ThreadID != "t1" || state.UserTurns != 1 || state.Prompt != "I need you to do a few things" {
		t.Fatalf("unexpected state %+v", state)
	}

	if _, err := recorder.SetTitle("t1-cont-2", "Implement furniture", "ai", 1); err != nil {
		t.Fatal(err)
	}
	meta, err := store.LoadMeta("ws")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range meta.Tasks {
		if task.Title != "Implement furniture" || task.TitleSource != "ai" {
			t.Fatalf("task %s not retitled: %+v", task.ThreadID, task)
		}
	}
	if meta.Workspace.Title != "Implement furniture" {
		t.Fatalf("single-chat workspace title not updated: %q", meta.Workspace.Title)
	}

	recorder.TrackTask(domain.Task{WorkspaceID: "ws", ThreadID: "t1", Title: "stale frontend title"})
	if state, _ := recorder.NoteUserTurn("t1"); state.Title != "Implement furniture" || state.UserTurns != 2 {
		t.Fatalf("re-tracking must keep the generated title and turn count: %+v", state)
	}

	if _, err := recorder.SetTitle("t1", "Sofa configurator", "user", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.SetTitle("t1", "AI rewrite", "ai", 5); err == nil {
		t.Fatal("AI must not overwrite a user rename")
	}
	if state, _ := recorder.NoteUserTurn("t1"); state.Title != "Sofa configurator" || state.Source != "user" {
		t.Fatalf("rename lost: %+v", state)
	}
}
