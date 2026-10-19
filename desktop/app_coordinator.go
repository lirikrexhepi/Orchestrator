package main

import (
	"fmt"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/history"
	"composer/internal/logger"
	"composer/internal/session"
)

// CoordinatorSend starts or reuses the coordinator session for the given
// selection and sends one message, returning the turn id to correlate events.
func (a *App) CoordinatorSend(cfg session.Config, text string) (string, error) {
	return a.CoordinatorSendFiles(cfg, text, nil)
}

// CoordinatorSendFiles is CoordinatorSend with attachments.
func (a *App) CoordinatorSendFiles(
	cfg session.Config,
	text string,
	files []domain.FileRef,
) (string, error) {
	preview := text
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	logger.Infof("App", "CoordinatorSendFiles: driver=%s model=%s files=%d prompt=%q", cfg.Driver, cfg.Model, len(files), preview)
	var err error
	if cfg.Cwd, err = a.requireCwd(cfg.Cwd); err != nil {
		return "", err
	}
	// Context rides along for the model only; the transcript records exactly
	// what the user typed, which is what the UI's optimistic bubble shows.
	preamble := a.coordinatorPreamble(cfg.Cwd)
	a.orchestrator.Remember(cfg)
	turnID, err := a.coordinator.SendWithContext(a.ctx, cfg, text, preamble, files)
	if err != nil {
		logger.Errorf("App", "CoordinatorSendFiles failed: %v", err)
	} else {
		logger.Infof("App", "CoordinatorSendFiles dispatched turnID=%s", turnID)
	}
	return turnID, err
}

// coordinatorPreamble joins the agent manifest and project memory the
// coordinator needs to plan, without putting them in the user's message.
func (a *App) coordinatorPreamble(cwd string) string {
	parts := make([]string, 0, 2)
	if a.orchestrator != nil {
		if manifest := a.orchestrator.FormatAgentManifest(cwd); manifest != "" {
			parts = append(parts, manifest)
		}
	}
	if memory := a.projectMemory(cwd); memory != "" {
		parts = append(parts, "Project context (long-lived, applies to all work here):\n"+memory)
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) CoordinatorInterrupt() error {
	logger.Infof("App", "CoordinatorInterrupt")
	return a.coordinator.Interrupt(a.ctx)
}

func (a *App) CoordinatorReset() error {
	logger.Infof("App", "CoordinatorReset")
	return a.coordinator.Reset(a.ctx)
}

// NewChat ends every running agent and starts a fresh orchestrator conversation.
//
// Stopping the agents is the point rather than a side effect: they hold CLI
// processes, worktrees and dev servers, and leaving them running while their
// windows disappear would strand work the user can no longer see or reach.
// Whatever they produced is already recorded, so the session stays in history.
func (a *App) NewChat() error {
	threads := a.manager.Sessions()
	stopping := make([]string, 0, len(threads))
	for _, live := range threads {
		if live.ThreadID == session.CoordinatorThreadID {
			continue
		}
		stopping = append(stopping, live.ThreadID)
	}
	logger.Infof("App", "NewChat: stopping %d agent threads %q (reason=explicit new chat)", len(stopping), strings.Join(stopping, ","))
	for _, live := range threads {
		if live.ThreadID == session.CoordinatorThreadID {
			continue
		}
		_ = a.manager.Stop(a.ctx, live.ThreadID)
		a.workspaces.SetState(live.ThreadID, domain.TaskClosed)
		a.recorder.UpdateTaskState(live.ThreadID, domain.TaskClosed, "")
	}
	logger.Infof("App", "NewChat: stopped %d threads, resetting coordinator", len(stopping))

	// Flushed before the transcript is cut loose, so the session that just ended
	// is complete on disk the moment it leaves the screen.
	if a.recorder != nil {
		_ = a.recorder.FlushAll()
	}
	return a.coordinator.Reset(a.ctx)
}

// ProjectSwitched resets the coordinator conversation for the newly active
// project without stopping any agent. Worker CLIs keep running across
// projects; ending them here would kill background subagents the user can
// still reach from the deck. Confirmation before discarding work stays a
// frontend concern; this path never discards.
func (a *App) ProjectSwitched() error {
	active := ""
	if a.projects != nil {
		active = a.projects.ActivePath()
	}
	preserved := 0
	for _, live := range a.manager.Sessions() {
		if live.ThreadID == session.CoordinatorThreadID {
			continue
		}
		preserved++
	}
	logger.Infof("App", "ProjectSwitched: active=%q preserving %d agent threads (reason=project switch, no agents stopped)", active, preserved)
	if a.recorder != nil {
		_ = a.recorder.FlushAll()
	}
	return a.coordinator.Reset(a.ctx)
}

func (a *App) CoordinatorHistory() []domain.RuntimeEvent {
	live := a.coordinator.History()
	if len(live) > 0 || a.historyStore == nil {
		return live
	}
	active := ""
	if a.projects != nil {
		active = a.projects.ActivePath()
	}
	if active == "" {
		return live
	}
	metas, err := a.historyStore.List()
	if err != nil {
		return live
	}
	var best *history.Meta
	for i := range metas {
		if !isSamePath(metas[i].Workspace.Cwd, active) {
			continue
		}
		if best == nil || metas[i].Workspace.UpdatedAt > best.Workspace.UpdatedAt {
			best = &metas[i]
		}
	}
	if best == nil || best.CoordinatorThreadID == "" {
		return live
	}
	loaded, err := a.historyStore.Load(best.Workspace.ID)
	if err != nil {
		return live
	}
	return loaded.Transcripts[best.CoordinatorThreadID]
}

// OrchestratorAgents reports every worker The Orchestrator owns, newest
// workspace first with live attachment state. This is the coordinator's agent
// list Cursor shows per Project: what is running, on which provider, in which
// directory, and whether its CLI is still attached.
func (a *App) OrchestratorAgents() []session.AgentView {
	if a.orchestrator == nil {
		return nil
	}
	return a.orchestrator.ListAgents()
}

// OrchestratorAuto reports whether the constructor executes coordinator plans
// itself (Cursor behavior) rather than waiting for a frontend confirm.
func (a *App) OrchestratorAuto() bool {
	if a.orchestrator == nil {
		return false
	}
	return a.orchestrator.Auto()
}

// OrchestratorSetAuto toggles constructor execution. Turn it off to go back to
// manual plan-card confirms; direct-agent DM is unaffected either way.
func (a *App) OrchestratorSetAuto(auto bool) {
	if a.orchestrator == nil {
		return
	}
	a.orchestrator.SetAuto(auto)
}

// OrchestratorLatest returns the most recent coordinator plan and whether The
// Orchestrator executed it, so the frontend can reconcile instead of spawning
// duplicates after a manual confirm.
func (a *App) OrchestratorLatest() *session.LatestPlan {
	if a.orchestrator == nil {
		return nil
	}
	return a.orchestrator.Latest()
}

// OrchestratorStopAgent stops one worker by thread id. The transcript stays in
// history for resume or provider switch.
func (a *App) OrchestratorStopAgent(threadID string) error {
	logger.Infof("App", "OrchestratorStopAgent: threadID=%s", threadID)
	if err := a.manager.Stop(a.ctx, threadID); err != nil {
		return err
	}
	a.workspaces.SetState(threadID, domain.TaskClosed)
	a.recorder.UpdateTaskState(threadID, domain.TaskClosed, "")
	return nil
}

// OrchestratorMessageAgent sends a follow-up turn to a running worker —
// steering, feedback, or review comments after The Orchestrator brings finished
// work back. Model/provider switch mid-thread goes through SwitchTaskProvider.
func (a *App) OrchestratorMessageAgent(threadID, text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("message is empty")
	}
	turnID := threadID + "-turn-" + time.Now().Format("150405")
	a.manager.RecordUserMessage(threadID, turnID, trimmed)
	return a.manager.Send(a.ctx, domain.SendTurnInput{
		ThreadID: threadID,
		TurnID:   turnID,
		Text:     trimmed,
	})
}

// projectMemory is long-lived per-project context (human-editable, stored
// under configRoot/memory) given to agents so they need no re-onboarding.
func (a *App) projectMemory(cwd string) string {
	if a.memory == nil || strings.TrimSpace(cwd) == "" {
		return ""
	}
	mem := strings.TrimSpace(a.memory.LoadProjectMemory(cwd))
	if len(mem) > 4000 {
		mem = mem[:4000] + "\n[truncated]"
	}
	return mem
}

func isSamePath(p1, p2 string) bool {
	if p1 == "" || p2 == "" {
		return false
	}
	n1 := strings.ToLower(strings.TrimRight(strings.ReplaceAll(p1, "\\", "/"), "/"))
	n2 := strings.ToLower(strings.TrimRight(strings.ReplaceAll(p2, "\\", "/"), "/"))
	return n1 == n2
}
