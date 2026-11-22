package main

import (
	"os"
	"path/filepath"
	"testing"

	"composer/internal/history"
	"composer/internal/session"
)

func TestImportClaudeCodeSessionStoresReplayableWorkspace(t *testing.T) {
	config := t.TempDir()
	projects := filepath.Join(config, "projects", "proj")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"parentUuid":"x","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"text","text":"Fix the login bug"}]},"uuid":"u1","timestamp":"2026-09-20T10:00:01.000Z","cwd":"C:\\work\\app","sessionId":"sess-9","version":"2.1.0"}` + "\n" +
		`{"parentUuid":"u1","isSidechain":false,"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"a.ts"}}]},"uuid":"a1","timestamp":"2026-09-20T10:00:05.000Z","sessionId":"sess-9"}` + "\n"
	sample := filepath.Join(projects, "sess-9.jsonl")
	if err := os.WriteFile(sample, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", config)

	store := history.New(t.TempDir())
	recorder := history.NewRecorder(store)
	t.Cleanup(func() { _ = recorder.Close() })
	app := &App{
		workspaces:   session.NewWorkspaces(),
		historyStore: store,
		recorder:     recorder,
	}

	sessions, err := app.ListClaudeCodeSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Title != "Fix the login bug" {
		t.Fatalf("sessions = %+v", sessions)
	}

	workspaceID, err := app.ImportClaudeCodeSession(sample)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	loaded, err := store.Load(workspaceID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Meta.Tasks) != 1 {
		t.Fatalf("tasks = %+v", loaded.Meta.Tasks)
	}
	task := loaded.Meta.Tasks[0]
	if task.Driver != "claude" || task.State != "complete" {
		t.Fatalf("task = %+v", task)
	}
	if resume, ok := loaded.Meta.Resume[task.ThreadID]; !ok || resume != "sess-9" {
		t.Fatalf("import must store a resume id: %+v", loaded.Meta.Resume)
	}
	events := loaded.Transcripts[task.ThreadID]
	if len(events) == 0 {
		t.Fatalf("no transcript stored")
	}
	if events[0].Kind != "notice" {
		t.Fatalf("first event should mark the import, got %q", events[0].Kind)
	}

	if _, err := app.ImportClaudeCodeSession(sample + ".missing"); err == nil {
		t.Fatalf("missing file must fail")
	}
	outside := filepath.Join(t.TempDir(), "evil.jsonl")
	if err := os.WriteFile(outside, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ImportClaudeCodeSession(outside); err == nil {
		t.Fatalf("path outside the projects tree must be rejected")
	}
}
