package main

import (
	"os"
	"path/filepath"
	"testing"

	"composer/internal/domain"
)

func TestResolveResumeCwdPrefersLiveTaskDir(t *testing.T) {
	taskDir := t.TempDir()
	wsDir := t.TempDir()
	task := domain.Task{ThreadID: "task-1", Cwd: taskDir}
	if got := resolveResumeCwd(task, wsDir); got != taskDir {
		t.Fatalf("cwd = %q, want task dir %q", got, taskDir)
	}
}

func TestResolveResumeCwdFallsBackToLiveWorkspaceDir(t *testing.T) {
	wsDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	task := domain.Task{ThreadID: "task-1", Cwd: missing}
	if got := resolveResumeCwd(task, wsDir); got != wsDir {
		t.Fatalf("cwd = %q, want workspace dir %q", got, wsDir)
	}
}

func TestResolveResumeCwdUsesEmptyTaskCwdFromWorkspace(t *testing.T) {
	wsDir := t.TempDir()
	task := domain.Task{ThreadID: "task-1"}
	if got := resolveResumeCwd(task, wsDir); got != wsDir {
		t.Fatalf("cwd = %q, want workspace dir %q", got, wsDir)
	}
}

func TestResolveResumeCwdPrefersLiveWorktree(t *testing.T) {
	worktree := t.TempDir()
	wsDir := t.TempDir()
	task := domain.Task{ThreadID: "task-1", Cwd: wsDir, Worktree: &domain.Worktree{Path: worktree}}
	if got := resolveResumeCwd(task, wsDir); got != worktree {
		t.Fatalf("cwd = %q, want worktree %q", got, worktree)
	}
}

func TestResolveResumeCwdKeepsCandidateWhenNothingIsLive(t *testing.T) {
	missingTask := filepath.Join(os.TempDir(), "orchestrator-missing-task")
	missingWs := filepath.Join(os.TempDir(), "orchestrator-missing-ws")
	_ = os.RemoveAll(missingTask)
	_ = os.RemoveAll(missingWs)
	task := domain.Task{ThreadID: "task-1", Cwd: missingTask}
	if got := resolveResumeCwd(task, missingWs); got != missingTask {
		t.Fatalf("cwd = %q, want original candidate %q", got, missingTask)
	}
}
