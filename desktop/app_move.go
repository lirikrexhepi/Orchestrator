package main

import (
	"fmt"
	"strings"

	"composer/internal/logger"
)

func (a *App) MoveChat(workspaceID, projectPath string) error {
	if strings.TrimSpace(projectPath) == "" {
		return fmt.Errorf("choose a project to move the chat to")
	}
	cwd, err := a.requireCwd(projectPath)
	if err != nil {
		return err
	}
	_ = a.recorder.FlushAll()
	meta, err := a.historyStore.LoadMeta(workspaceID)
	if err != nil {
		return err
	}
	if isSamePath(meta.Workspace.Cwd, cwd) {
		return nil
	}
	for _, task := range meta.Tasks {
		if a.manager.TurnInProgress(task.ThreadID) {
			return fmt.Errorf("wait for %q to finish before moving it", task.Title)
		}
	}

	for _, task := range meta.Tasks {
		if task.Worktree != nil {
			continue
		}
		sessionID := a.manager.ProviderSessionID(task.ThreadID)
		if sessionID == "" {
			sessionID = meta.Resume[task.ThreadID]
		}
		_ = a.manager.Stop(a.ctx, task.ThreadID)
		if sessionID == "" || (meta.Workspace.ImportedFrom == "claude-code" && importedClaudeSession(meta.Workspace, task.ThreadID) == sessionID) {
			continue
		}
		if err := a.manager.MoveProviderSession(task.Driver, task.Account, sessionID, cwd); err != nil {
			logger.Warnf("App", "MoveChat: could not move %s session %s to %s: %v", task.Driver, sessionID, cwd, err)
		}
	}

	a.workspaces.SetWorkspaceCwd(workspaceID, cwd)
	if err := a.recorder.MoveWorkspace(workspaceID, cwd); err != nil {
		return err
	}
	logger.Infof("App", "MoveChat: workspace %s moved to %s", workspaceID, cwd)
	a.emit(historyChangedChannel)
	return nil
}
