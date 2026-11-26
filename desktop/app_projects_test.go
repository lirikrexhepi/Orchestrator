package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"composer/internal/domain"
	"composer/internal/history"
	"composer/internal/memory"
	"composer/internal/projects"
	"composer/internal/session"
)

func TestRelocateProjectMigratesHistoryAndMemory(t *testing.T) {
	dataDir := t.TempDir()
	oldDir := filepath.Join(t.TempDir(), "falacy")
	newDir := filepath.Join(t.TempDir(), "projects", "falacy")
	otherDir := filepath.Join(t.TempDir(), "other")
	for _, p := range []string{oldDir, newDir, otherDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	projStore := projects.New(dataDir)
	proj, err := projStore.Add(oldDir, false)
	if err != nil {
		t.Fatalf("Add project: %v", err)
	}
	if _, err := projStore.Add(otherDir, false); err != nil {
		t.Fatalf("Add other project: %v", err)
	}

	histStore := history.New(filepath.Join(dataDir, "history"))
	recorder := history.NewRecorder(histStore)
	workspaces := session.NewWorkspaces()
	memStore := memory.New(filepath.Join(dataDir, "memory"))
	if err := memStore.SaveProjectMemory(oldDir, "remember falacy architecture"); err != nil {
		t.Fatalf("SaveProjectMemory: %v", err)
	}

	// Seed two workspaces: one in falacy (with a normal task + a worktree task),
	// and one in another project that must remain untouched.
	falacyMeta := history.Meta{
		Workspace: domain.Workspace{ID: "ws-falacy", Cwd: oldDir, Title: "Fix bug"},
		Tasks: []domain.Task{
			{ID: "t1", WorkspaceID: "ws-falacy", ThreadID: "th-1", Title: "Main", Cwd: oldDir},
			{ID: "t2", WorkspaceID: "ws-falacy", ThreadID: "th-2", Title: "WT", Cwd: filepath.Join(oldDir, "wt"), Worktree: &domain.Worktree{Path: filepath.Join(oldDir, "wt")}},
		},
	}
	otherMeta := history.Meta{
		Workspace: domain.Workspace{ID: "ws-other", Cwd: otherDir, Title: "Other"},
		Tasks:     []domain.Task{{ID: "t3", WorkspaceID: "ws-other", ThreadID: "th-3", Title: "Other", Cwd: otherDir}},
	}
	for _, meta := range []history.Meta{falacyMeta, otherMeta} {
		if err := histStore.SaveMeta(meta); err != nil {
			t.Fatalf("SaveMeta %s: %v", meta.Workspace.ID, err)
		}
		workspaces.Restore(meta.Workspace)
		for _, task := range meta.Tasks {
			_, _ = workspaces.RestoreTask(task)
		}
		recorder.Restore(meta)
	}

	// Remove the old folder to mimic moving it off Desktop.
	if err := os.RemoveAll(oldDir); err != nil {
		t.Fatalf("remove oldDir: %v", err)
	}

	app := &App{
		ctx:          context.Background(),
		headless:     true,
		projects:     projStore,
		historyStore: histStore,
		recorder:     recorder,
		workspaces:   workspaces,
		memory:       memStore,
	}

	updated, err := app.RelocateProject(proj.ID, newDir)
	if err != nil {
		t.Fatalf("RelocateProject: %v", err)
	}
	if updated.Path != newDir || updated.Missing {
		t.Fatalf("unexpected updated project: %+v", updated)
	}

	migrated, err := histStore.LoadMeta("ws-falacy")
	if err != nil {
		t.Fatalf("LoadMeta ws-falacy: %v", err)
	}
	if !isSamePath(migrated.Workspace.Cwd, newDir) {
		t.Fatalf("workspace cwd not migrated: got %q want %q", migrated.Workspace.Cwd, newDir)
	}
	if !isSamePath(migrated.Tasks[0].Cwd, newDir) {
		t.Fatalf("task 0 cwd not migrated: got %q want %q", migrated.Tasks[0].Cwd, newDir)
	}
	if !isSamePath(migrated.Tasks[1].Cwd, filepath.Join(oldDir, "wt")) {
		t.Fatalf("worktree task cwd should stay untouched, got %q", migrated.Tasks[1].Cwd)
	}

	untouched, err := histStore.LoadMeta("ws-other")
	if err != nil {
		t.Fatalf("LoadMeta ws-other: %v", err)
	}
	if !isSamePath(untouched.Workspace.Cwd, otherDir) {
		t.Fatalf("other workspace should stay untouched, got %q", untouched.Workspace.Cwd)
	}

	if got := memStore.LoadProjectMemory(newDir); got != "remember falacy architecture" {
		t.Fatalf("project memory not migrated: got %q", got)
	}
}
