package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"composer/internal/domain"
)

func event(threadID, text string, seq uint64) domain.RuntimeEvent {
	return domain.RuntimeEvent{
		Kind:     domain.EventAgentMessage,
		ThreadID: threadID,
		Text:     text,
		Seq:      seq,
		At:       1700000000000,
	}
}

func TestRoundTripKeepsOrchestratorAndAgentsTogether(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()

	meta := Meta{
		Workspace:           domain.Workspace{ID: "ws-1", Title: "Fix parcels", UpdatedAt: 20},
		CoordinatorThreadID: "coordinator-ws-1",
		Tasks: []domain.Task{
			{ID: "t-1", WorkspaceID: "ws-1", ThreadID: "thread-a", Title: "PHA-1015"},
			{ID: "t-2", WorkspaceID: "ws-1", ThreadID: "thread-b", Title: "PHA-1016"},
		},
		Resume: map[string]string{"thread-a": "claude-sess-1"},
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	// The orchestrator and both agents record onto the same workspace.
	if err := store.Append("ws-1", "coordinator-ws-1", event("coordinator-ws-1", "plan", 1)); err != nil {
		t.Fatalf("append coordinator: %v", err)
	}
	for i, thread := range []string{"thread-a", "thread-b"} {
		if err := store.Append("ws-1", thread, event(thread, "work", uint64(i+2))); err != nil {
			t.Fatalf("append %s: %v", thread, err)
		}
	}

	loaded, err := store.Load("ws-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The whole point of the feature: one load yields the orchestrator plus
	// every agent it spawned, not a single thread.
	if got := len(loaded.Transcripts); got != 3 {
		t.Fatalf("expected 3 transcripts (orchestrator + 2 agents), got %d", got)
	}
	if loaded.Meta.CoordinatorThreadID != "coordinator-ws-1" {
		t.Errorf("coordinator thread id lost: %q", loaded.Meta.CoordinatorThreadID)
	}
	if got := loaded.Transcripts["coordinator-ws-1"]; len(got) != 1 || got[0].Text != "plan" {
		t.Errorf("orchestrator transcript not restored: %+v", got)
	}
	if loaded.Meta.Resume["thread-a"] != "claude-sess-1" {
		t.Errorf("resume id lost: %+v", loaded.Meta.Resume)
	}
	if len(loaded.Meta.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(loaded.Meta.Tasks))
	}
}

func TestLoadSeesEventsBufferedInSameRun(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()

	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: "ws-1"}}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	// Deliberately no Flush: reopening a session in the run that created it must
	// not depend on the buffer having filled.
	if err := store.Append("ws-1", "thread-a", event("thread-a", "hello", 1)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	loaded, err := store.Load("ws-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.Transcripts["thread-a"]; len(got) != 1 {
		t.Fatalf("buffered event not visible to Load, got %d events", len(got))
	}
}

func TestTruncatedFinalLineIsRecovered(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: "ws-1"}}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if err := store.Append("ws-1", "thread-a", event("thread-a", "first", 1)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a crash mid-write: a valid line followed by a partial one.
	path := filepath.Join(root, "ws-1", "thread-a.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := file.WriteString(`{"kind":"agent.message","tex`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	file.Close()

	reopened := New(root)
	defer reopened.Close()
	loaded, err := reopened.Load("ws-1")
	if err != nil {
		t.Fatalf("Load after crash: %v", err)
	}
	if got := loaded.Transcripts["thread-a"]; len(got) != 1 || got[0].Text != "first" {
		t.Fatalf("complete events should survive a truncated tail, got %+v", got)
	}
}

func TestListIsNewestFirstAndSkipsBrokenWorkspaces(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	defer store.Close()

	for _, ws := range []domain.Workspace{
		{ID: "ws-old", Title: "older", UpdatedAt: 10},
		{ID: "ws-new", Title: "newer", UpdatedAt: 99},
	} {
		if err := store.SaveMeta(Meta{Workspace: ws}); err != nil {
			t.Fatalf("SaveMeta: %v", err)
		}
	}
	// A directory from a crashed write, with no readable metadata.
	if err := os.MkdirAll(filepath.Join(root, "ws-broken"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected the broken workspace to be skipped, got %d entries", len(list))
	}
	if list[0].Workspace.ID != "ws-new" {
		t.Errorf("expected newest first, got %q", list[0].Workspace.ID)
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	defer store.Close()

	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: "ws-1"}}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	if err := store.Append("ws-1", "thread-a", event("thread-a", "x", 1)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Delete("ws-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "ws-1")); !os.IsNotExist(err) {
		t.Fatalf("workspace directory should be gone, stat err = %v", err)
	}
	// Appending after a delete must not resurrect a half-formed workspace that
	// List would then surface without metadata.
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("expected empty history, got %d", len(list))
	}
}

func TestSafeNameRejectsPathEscape(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "______etc_passwd",
		"ws-1":             "ws-1",
		"thread/a":         "thread_a",
		"":                 "_",
	}
	for input, want := range cases {
		if got := safeName(input); got != want {
			t.Errorf("safeName(%q) = %q, want %q", input, got, want)
		}
	}

	// The property that actually matters: no result can escape its directory.
	for _, input := range []string{"../../etc/passwd", "a/b/c", "..", ".", "x\\y"} {
		got := safeName(input)
		if strings.ContainsAny(got, `/\`) || got == ".." || got == "." {
			t.Errorf("safeName(%q) = %q still traverses", input, got)
		}
	}
}

func TestDisabledStoreIsInert(t *testing.T) {
	// A store whose root cannot be created must degrade quietly: losing history
	// is acceptable, refusing to run agents is not. A regular file where the
	// root should go is what makes MkdirAll fail.
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	store := New(filepath.Join(blocker, "history"))
	defer store.Close()
	if !store.disabled {
		t.Fatal("store should have disabled itself when its root could not be created")
	}
	if err := store.Append("ws-1", "thread-a", event("thread-a", "x", 1)); err != nil {
		t.Errorf("Append on disabled store should be a no-op, got %v", err)
	}
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: "ws-1"}}); err != nil {
		t.Errorf("SaveMeta on disabled store should be a no-op, got %v", err)
	}
	if list, err := store.List(); err != nil || len(list) != 0 {
		t.Errorf("List on disabled store = %v, %v", list, err)
	}
}

func TestMergeContinuationsOnLoadAndList(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	defer store.Close()

	wsID := "ws-switch-test"
	baseID := "task-base-1"
	contID := "task-base-1-cont-9999"

	meta := Meta{
		Workspace: domain.Workspace{ID: wsID, Title: "Cross-provider session", UpdatedAt: 100},
		Tasks: []domain.Task{
			{
				ThreadID:  baseID,
				Title:     "Switch prompt",
				Driver:    "antigravity",
				Model:     "gemini-3.8-flash",
				State:     domain.TaskClosed,
				CreatedAt: 100,
				UpdatedAt: 110,
			},
			{
				ThreadID:  contID,
				Title:     "Switch prompt",
				Driver:    "opencode",
				Model:     "opencode/muse-spark-1.3",
				State:     domain.TaskRunning,
				CreatedAt: 120,
				UpdatedAt: 130,
			},
		},
		Resume: map[string]string{
			baseID: "resume-antigravity",
			contID: "resume-opencode",
		},
	}

	if err := store.SaveMeta(meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	// Write base transcript
	_ = store.Append(wsID, baseID, domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: baseID,
		Text:     "initial prompt",
		At:       105,
	})
	_ = store.Append(wsID, baseID, domain.RuntimeEvent{
		Kind:     domain.EventAgentMessage,
		ThreadID: baseID,
		Text:     "gemini reply",
		At:       110,
	})

	// Write cont transcript
	_ = store.Append(wsID, contID, domain.RuntimeEvent{
		Kind:     domain.EventNotice,
		ThreadID: contID,
		Text:     "Switched from antigravity to opencode",
		At:       122,
	})
	_ = store.Append(wsID, contID, domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: contID,
		Text:     "continuation prompt",
		At:       125,
	})
	_ = store.Append(wsID, contID, domain.RuntimeEvent{
		Kind:     domain.EventAgentMessage,
		ThreadID: contID,
		Text:     "opencode reply",
		At:       130,
	})
	_ = store.Flush(wsID)

	// List should report 1 merged task
	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(list))
	}
	if len(list[0].Tasks) != 1 {
		t.Fatalf("expected 1 merged task in list, got %d", len(list[0].Tasks))
	}
	mergedTask := list[0].Tasks[0]
	if mergedTask.ThreadID != baseID {
		t.Errorf("expected root thread ID %s, got %s", baseID, mergedTask.ThreadID)
	}
	if mergedTask.Driver != "opencode" || mergedTask.Model != "opencode/muse-spark-1.3" {
		t.Errorf("expected latest provider opencode/muse-spark-1.3, got %s / %s", mergedTask.Driver, mergedTask.Model)
	}

	// Load should merge transcripts and clean up continuation file
	loaded, err := store.Load(wsID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Meta.Tasks) != 1 {
		t.Fatalf("expected 1 task in loaded meta, got %d", len(loaded.Meta.Tasks))
	}
	if _, hasContInMap := loaded.Transcripts[contID]; hasContInMap {
		t.Errorf("continuation thread ID should have been removed from Transcripts map")
	}
	events := loaded.Transcripts[baseID]
	if len(events) != 5 {
		t.Fatalf("expected 5 merged events, got %d", len(events))
	}
	if events[0].Text != "initial prompt" || events[2].Kind != domain.EventNotice || events[4].Text != "opencode reply" {
		t.Errorf("events not merged in correct sequence: %v", events)
	}
	if loaded.Meta.Resume[baseID] != "resume-opencode" {
		t.Errorf("expected resume id resume-opencode, got %s", loaded.Meta.Resume[baseID])
	}
}

func TestUserHistoryHealing(t *testing.T) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Skip("no APPDATA")
	}
	historyDir := filepath.Join(appData, "composer", "history")
	if _, err := os.Stat(historyDir); err != nil {
		t.Skip("no composer history dir")
	}

	store := New(historyDir)
	defer store.Close()

	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Verify no continuation tasks exist in listed items
	for _, meta := range list {
		for _, task := range meta.Tasks {
			if strings.Contains(task.ThreadID, "-cont-") {
				t.Errorf("found unmerged continuation task in workspace %s: %s", meta.Workspace.ID, task.ThreadID)
			}
		}
	}

	// Load ws-2-mu8vfw30 if it exists and verify it loads as 1 task
	testWs := "ws-2-mu8vfw30"
	if _, err := os.Stat(filepath.Join(historyDir, testWs)); err == nil {
		loaded, err := store.Load(testWs)
		if err != nil {
			t.Fatalf("Load %s failed: %v", testWs, err)
		}
		if len(loaded.Meta.Tasks) != 1 {
			t.Errorf("expected 1 task in %s, got %d", testWs, len(loaded.Meta.Tasks))
		}
		for threadID := range loaded.Transcripts {
			if strings.Contains(threadID, "-cont-") {
				t.Errorf("found unmerged continuation transcript in %s: %s", testWs, threadID)
			}
		}
	}
}

func TestAutoHealCorruptMeta(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	defer store.Close()

	wsDir := filepath.Join(root, "ws-heal")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Corrupt meta.json with zero bytes
	if err := os.WriteFile(filepath.Join(wsDir, "meta.json"), make([]byte, 500), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create valid transcript with a user prompt
	tPath := filepath.Join(wsDir, "task-agent-1.jsonl")
	ev := domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: "task-agent-1",
		Text:     "Build the new dashboard",
		Driver:   domain.DriverAntigravity,
		At:       1700000000123,
	}
	payload, _ := json.Marshal(ev)
	if err := os.WriteFile(tPath, append(payload, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// LoadMeta should auto-heal from the transcript
	meta, err := store.LoadMeta("ws-heal")
	if err != nil {
		t.Fatalf("LoadMeta failed to auto-heal: %v", err)
	}
	if meta.Workspace.ID != "ws-heal" {
		t.Errorf("expected ws-heal, got %s", meta.Workspace.ID)
	}
	if meta.Workspace.Prompt != "Build the new dashboard" {
		t.Errorf("expected prompt 'Build the new dashboard', got %q", meta.Workspace.Prompt)
	}
	if len(meta.Tasks) != 1 || meta.Tasks[0].ThreadID != "task-agent-1" {
		t.Errorf("expected task-agent-1, got %+v", meta.Tasks)
	}

	// Verify it also shows up in List
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 || list[0].Workspace.ID != "ws-heal" {
		t.Fatalf("expected 1 item in list, got %d", len(list))
	}
}

func TestBOMInMeta(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	defer store.Close()

	wsDir := filepath.Join(root, "ws-bom")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	rawJSON := `{"workspace":{"id":"ws-bom","title":"Test BOM","createdAt":1700000000000,"updatedAt":1700000000000},"tasks":[]}`
	bomPayload := append([]byte("\xef\xbb\xbf"), []byte(rawJSON)...)
	if err := os.WriteFile(filepath.Join(wsDir, "meta.json"), bomPayload, 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := store.LoadMeta("ws-bom")
	if err != nil {
		t.Fatalf("LoadMeta failed on BOM: %v", err)
	}
	if meta.Workspace.Title != "Test BOM" {
		t.Errorf("expected 'Test BOM', got %q", meta.Workspace.Title)
	}
}

func TestRemoveTurnDropsOnlyThatTurnAndKeepsAppending(t *testing.T) {
	store := New(t.TempDir())
	defer store.Close()
	if err := store.SaveMeta(Meta{Workspace: domain.Workspace{ID: "ws-1"}, Tasks: []domain.Task{{ThreadID: "thread-a", WorkspaceID: "ws-1"}}}); err != nil {
		t.Fatal(err)
	}
	add := func(kind domain.EventKind, turn, text string) {
		if err := store.Append("ws-1", "thread-a", domain.RuntimeEvent{Kind: kind, ThreadID: "thread-a", TurnID: turn, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	add(domain.EventUserMessage, "t1", "keep")
	add(domain.EventAgentMessage, "t1", "kept reply")
	add(domain.EventNotice, "", "switched model")
	add(domain.EventUserMessage, "t2", "wrong chat")
	add(domain.EventAgentMessage, "t2", "reply to wrong chat")
	add(domain.EventTurnCompleted, "t2", "")
	add(domain.EventUserMessage, "t3", "keep too")

	if err := store.RemoveTurn("ws-1", "thread-a", "t2"); err != nil {
		t.Fatal(err)
	}
	add(domain.EventAgentMessage, "t3", "after removal")

	loaded, err := store.Load("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range loaded.Transcripts["thread-a"] {
		if e.TurnID == "t2" {
			t.Fatalf("turn t2 still present: %+v", e)
		}
		texts = append(texts, e.Text)
	}
	want := []string{"keep", "kept reply", "switched model", "keep too", "after removal"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("transcript = %v, want %v", texts, want)
	}
}
