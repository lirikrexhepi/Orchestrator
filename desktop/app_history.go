package main

import (
	"os"

	"composer/internal/domain"
	"composer/internal/history"
	"composer/internal/logger"
	"composer/internal/session"
)

// rehydrateFromHistory rebuilds in-memory routing from disk so a restart keeps
// recording to the same workspaces instead of orphaning them.
//
// Roles stay fixed like the competitors (Zeron's journal + snapshots, Emdash's
// single SQLite): JSONL under history/ is the canonical transcript, SQLite is
// only the metadata index, and CLI-native session ids are best-effort cache.
// Without this, Workspaces starts empty and every worktree looks orphaned.
func (a *App) rehydrateFromHistory() {
	if a.historyStore == nil {
		return
	}
	metas, err := a.historyStore.List()
	if err != nil || len(metas) == 0 {
		return
	}
	for _, meta := range metas {
		if meta.Workspace.ID == "" {
			continue
		}
		if a.workspaces != nil {
			a.workspaces.Restore(meta.Workspace)
			for _, task := range meta.Tasks {
				_, _ = a.workspaces.RestoreTask(task)
			}
		}
		if a.recorder != nil {
			a.recorder.Restore(meta)
		}
	}
}

// ListHistory reports every stored session, newest first.
//
// A Composer session is a workspace: the orchestrator conversation plus every
// agent it spawned. Listing reads only metadata, never transcripts.
func (a *App) ListHistory() []history.Meta {
	metas, err := a.historyStore.List()
	if err != nil {
		return nil
	}
	return metas
}

// LoadHistory reopens one session in full: metadata, the orchestrator
// transcript, and every agent's transcript.
func (a *App) LoadHistory(workspaceID string) (history.Session, error) {
	return a.historyStore.Load(workspaceID)
}

// DeleteHistory removes a stored session permanently.
func (a *App) DeleteHistory(workspaceID string) error {
	return a.recorder.Forget(workspaceID)
}

// DeleteTaskHistory removes an individual chat task from history.
// If threadID is empty or it was the only task, the entire workspace is removed.
func (a *App) DeleteTaskHistory(workspaceID, threadID string) error {
	if threadID == "" {
		return a.recorder.Forget(workspaceID)
	}
	return a.recorder.ForgetCliTask(workspaceID, threadID)
}

func (a *App) SetChatArchived(workspaceID, threadID string, archived bool) error {
	return a.recorder.SetChatArchived(workspaceID, threadID, archived)
}

// resolveResumeCwd picks the directory a stored task restarts in. The task's
// own directory wins, but a stale record (deleted worktree, moved folder)
// falls back to the workspace directory when that still exists, instead of
// handing the CLI a path that fails obscurely or resolves elsewhere.
func resolveResumeCwd(task domain.Task, workspaceCwd string) string {
	candidate := task.Dir(workspaceCwd)
	if isLiveDir(candidate) {
		return candidate
	}
	if isLiveDir(workspaceCwd) {
		return workspaceCwd
	}
	return candidate
}

func isLiveDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ResumeHistory restarts the agents of a stored session.
//
// Every task is attempted; the per-task outcome says whether the agent genuinely
// continued its old conversation, started fresh in the same directory, or could
// not start at all. Those are meaningfully different states and the caller is
// told which it got rather than left to assume.
func (a *App) ResumeHistory(workspaceID string) (session.ResumeResult, error) {
	return a.ResumeHistoryThread(workspaceID, "")
}

// ResumeHistoryThread restarts one stored agent (or every agent of the
// session when threadID is empty). Messaging one old chat must not launch a
// CLI for every other task that happened to share its workspace.
func (a *App) ResumeHistoryThread(workspaceID, threadID string) (session.ResumeResult, error) {
	loaded, err := a.historyStore.Load(workspaceID)
	if err != nil {
		return session.ResumeResult{}, err
	}

	requests := make([]session.ResumeRequest, 0, len(loaded.Meta.Tasks))
	for _, task := range loaded.Meta.Tasks {
		if threadID != "" && task.ThreadID != threadID && history.RootThreadID(task.ThreadID) != threadID {
			continue
		}
		cwd := resolveResumeCwd(task, loaded.Meta.Workspace.Cwd)
		resumeID := loaded.Meta.Resume[task.ThreadID]
		logger.Infof("App", "ResumeHistoryThread: thread=%s cwd=%q fellBack=%v hasResumeID=%v", task.ThreadID, cwd, cwd != task.Dir(loaded.Meta.Workspace.Cwd), resumeID != "")
		fork := false
		if loaded.Meta.Workspace.ImportedFrom == "claude-code" {
			original := importedClaudeSession(loaded.Meta.Workspace, task.ThreadID)
			if resumeID == "" {
				resumeID = original
			}
			fork = resumeID != "" && resumeID == original
		}
		requests = append(requests, session.ResumeRequest{
			ThreadID:          task.ThreadID,
			Title:             task.Title,
			Driver:            task.Driver,
			Account:           task.Account,
			Model:             task.Model,
			Options:           task.Options,
			Cwd:               cwd,
			ProviderSessionID: resumeID,
			Permission:        task.Permission,
			Fork:              fork,
			Transcript:        loaded.Transcripts[task.ThreadID],
		})
	}

	result := a.spawner.Resume(a.ctx, requests)
	result.WorkspaceID = workspaceID

	// Re-register the revived threads so their new events append to the same
	// session rather than starting a second copy of it.
	for _, outcome := range result.Outcomes {
		if !outcome.Live {
			continue
		}
		for _, task := range loaded.Meta.Tasks {
			if task.ThreadID == outcome.ThreadID {
				if live, ok := a.manager.ThreadSession(task.ThreadID); ok && live.Cwd != "" && task.Worktree == nil {
					task.Cwd = live.Cwd
					a.workspaces.SetTaskCwd(task.ThreadID, live.Cwd)
				}
				a.recorder.TrackTask(task)
				break
			}
		}
	}
	return result, nil
}
