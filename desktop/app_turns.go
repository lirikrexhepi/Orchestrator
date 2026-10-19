package main

import (
	"errors"
	"fmt"
	"strings"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/session"
)

func turnMatchKey(text string) string {
	key := strings.Join(strings.Fields(text), " ")
	if len(key) > 200 {
		key = key[:200]
	}
	return key
}

func turnToRemove(events []domain.RuntimeEvent, turnID string) (string, int, bool) {
	index := -1
	for i, event := range events {
		if event.Kind == domain.EventUserMessage && event.TurnID == turnID {
			index = i
			break
		}
	}
	if index < 0 {
		return "", 0, false
	}
	text := events[index].Text
	key := turnMatchKey(text)
	occurrence := 0
	for _, event := range events[index+1:] {
		if event.Kind == domain.EventUserMessage && key != "" && strings.Contains(turnMatchKey(event.Text), key) {
			occurrence++
		}
	}
	return text, occurrence, true
}

func (a *App) fullTranscript(task *domain.Task) []domain.RuntimeEvent {
	if a.historyStore != nil {
		if loaded, err := a.historyStore.Load(task.WorkspaceID); err == nil {
			if events := loaded.Transcripts[task.ThreadID]; len(events) > 0 {
				return events
			}
		}
	}
	return a.manager.History(task.ThreadID)
}

func (a *App) isImportedOriginal(task *domain.Task, sessionID string) bool {
	workspace := a.workspaces.Get(task.WorkspaceID)
	if workspace == nil || workspace.ImportedFrom != "claude-code" {
		return false
	}
	return importedClaudeSession(*workspace, task.ThreadID) == sessionID
}

func (a *App) DeleteTurn(threadID, turnID string) ([]domain.RuntimeEvent, error) {
	task, ok := a.workspaces.TaskByThread(threadID)
	if !ok {
		return nil, fmt.Errorf("no task for thread %s", threadID)
	}
	if a.manager.TurnInProgress(threadID) {
		return nil, fmt.Errorf("wait for the agent to finish before deleting a message")
	}
	_ = a.recorder.FlushAll()
	text, occurrence, found := turnToRemove(a.fullTranscript(task), turnID)
	if !found {
		return nil, fmt.Errorf("message not found in this chat")
	}

	sessionID := a.manager.ProviderSessionID(threadID)
	if sessionID == "" {
		sessionID = a.recorder.ResumeID(threadID)
	}
	if sessionID != "" && a.isImportedOriginal(task, sessionID) {
		return nil, fmt.Errorf("this message belongs to a Claude Code chat imported from outside the app, which is left untouched")
	}
	_ = a.manager.Stop(a.ctx, threadID)

	err := session.ErrTurnRemovalUnsupported
	if sessionID != "" {
		err = a.manager.RemoveProviderTurn(task.Driver, task.Account, sessionID, text, occurrence)
	}
	switch {
	case err == nil:
		logger.Infof("App", "DeleteTurn: removed turn %s from %s session %s", turnID, task.Driver, sessionID)
	case errors.Is(err, session.ErrTurnRemovalUnsupported):
		logger.Infof("App", "DeleteTurn: %s cannot edit its session; thread %s will restart from the cleaned transcript", task.Driver, threadID)
		a.recorder.ForgetProviderSession(threadID)
	default:
		return nil, err
	}

	if err := a.recorder.RemoveTurn(threadID, turnID); err != nil {
		return nil, err
	}
	a.manager.ForgetTurn(threadID, turnID)
	a.emit(historyChangedChannel)
	return a.fullTranscript(task), nil
}
