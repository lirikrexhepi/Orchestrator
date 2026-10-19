package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/git"
	"composer/internal/logger"
	"composer/internal/session"

)

func (a *App) StartSession(driver string, input domain.SessionStartInput) (domain.Session, error) {
	var err error
	if input.Cwd, err = a.requireCwd(input.Cwd); err != nil {
		return domain.Session{}, err
	}
	logger.Infof("App", "StartSession: driver=%s threadID=%s model=%s cwd=%s resume=%s", driver, input.ThreadID, input.Model, input.Cwd, input.Resume)
	sess, err := a.manager.Start(a.ctx, domain.DriverKind(driver), input)
	if err != nil {
		logger.Errorf("App", "StartSession failed: %v", err)
		return domain.Session{}, err
	}
	if a.sessionService != nil {
		title := input.ThreadID
		if title == "" {
			title = sess.ThreadID
		}
		_ = a.sessionService.CreateSession(a.ctx, sess.ThreadID, title, input.Cwd, driver, input.Model, "")
	}
	return sess, nil
}

func (a *App) SendTurn(input domain.SendTurnInput) error {
	preview := input.Text
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	logger.Infof("App", "SendTurn: threadID=%s turnID=%s files=%d prompt=%q", input.ThreadID, input.TurnID, len(input.Files), preview)
	a.manager.RecordUserMessage(input.ThreadID, input.TurnID, input.Text, input.Files...)
	err := a.manager.Send(a.ctx, input)
	if errors.Is(err, session.ErrNoSession) {
		err = a.resumeAndSend(input, err)
	}
	if err != nil {
		logger.Errorf("App", "SendTurn failed: %v", err)
	}
	return err
}

func (a *App) resumeAndSend(input domain.SendTurnInput, cause error) error {
	workspaceID := ""
	if task, ok := a.workspaces.TaskByThread(input.ThreadID); ok {
		workspaceID = task.WorkspaceID
	}
	if workspaceID == "" {
		workspaceID, _ = a.recorder.WorkspaceOf(input.ThreadID)
	}
	if workspaceID == "" {
		return cause
	}
	logger.Infof("App", "SendTurn: thread %s has no live session; resuming it", input.ThreadID)
	result, err := a.ResumeHistoryThread(workspaceID, input.ThreadID)
	if err != nil {
		return fmt.Errorf("resume %s: %w", input.ThreadID, err)
	}
	for _, outcome := range result.Outcomes {
		if outcome.ThreadID == input.ThreadID && !outcome.Live {
			return fmt.Errorf("resume %s: %s", input.ThreadID, outcome.Reason)
		}
	}
	return a.manager.Send(a.ctx, input)
}

func (a *App) InterruptTurn(threadID string) error {
	logger.Infof("App", "InterruptTurn: threadID=%s", threadID)
	return a.manager.Interrupt(a.ctx, threadID)
}

func (a *App) RespondToApproval(threadID, requestID, decision string) error {
	logger.Infof("App", "RespondToApproval: threadID=%s requestID=%s decision=%s", threadID, requestID, decision)
	return a.manager.Respond(a.ctx, threadID, requestID, domain.ApprovalDecision(decision))
}

func (a *App) RespondToQuestion(threadID, requestID string, answers []string) error {
	logger.Infof("App", "RespondToQuestion: threadID=%s requestID=%s answers=%v", threadID, requestID, answers)
	return a.manager.RespondQuestion(a.ctx, threadID, requestID, answers)
}

func (a *App) StopSession(threadID string) error {
	logger.Infof("App", "StopSession: threadID=%s", threadID)
	err := a.manager.Stop(a.ctx, threadID)
	// A closed card is a closed task: without this the coordinator's agent
	// manifest keeps offering it as a routing target.
	if _, ok := a.workspaces.TaskByThread(threadID); ok {
		a.workspaces.SetState(threadID, domain.TaskClosed)
		a.recorder.UpdateTaskState(threadID, domain.TaskClosed, "")
	}
	return err
}

func (a *App) ListSessions() []domain.Session {
	return a.manager.Sessions()
}

func (a *App) ThreadHistory(threadID string) []domain.RuntimeEvent {
	return a.manager.History(threadID)
}

// ParseTasks extracts a delegation plan from an orchestrator reply. Returns an
// empty list for ordinary conversational answers.
func (a *App) ParseTasks(text string) []session.TaskRequest {
	return session.ParseTasks(text)
}

// SpawnTasks starts one agent session per task, optionally isolating each in
// its own git worktree.
func (a *App) SpawnTasks(requests []session.SpawnRequest, opts session.SpawnOptions) (session.SpawnResult, error) {
	var err error
	if opts.Cwd, err = a.requireCwd(opts.Cwd); err != nil {
		// A plan that explicitly names a folder may be launched without an active
		// project. Use that directory as the workspace context, but never use the
		// desktop application's own process directory.
		for _, request := range requests {
			if strings.TrimSpace(request.Cwd) == "" {
				continue
			}
			if opts.Cwd, err = a.requireCwd(request.Cwd); err == nil {
				break
			}
		}
		if err != nil {
			return session.SpawnResult{}, err
		}
	}
	// A per-task directory still wins, so a plan that names another project
	// keeps working; only the unset ones inherit the active project.
	for i := range requests {
		if strings.TrimSpace(requests[i].Cwd) == "" {
			requests[i].Cwd = opts.Cwd
			continue
		}
		if requests[i].Cwd, err = a.requireCwd(requests[i].Cwd); err != nil {
			return session.SpawnResult{}, err
		}
	}
	for i := range requests {
		cwd := requests[i].Cwd
		if cwd == "" {
			cwd = opts.Cwd
		}
		if mem := a.projectMemory(cwd); mem != "" {
			requests[i].Preamble = "Project context (long-lived, applies to all work here):\n" + mem
		}
	}
	res, err := a.spawner.Spawn(a.ctx, requests, opts)
	if a.sessionService != nil && len(res.Tasks) > 0 {
		for _, task := range res.Tasks {
			branch := ""
			cwd := opts.Cwd
			if task.Worktree != nil {
				branch = task.Worktree.Branch
				if task.Worktree.Path != "" {
					cwd = task.Worktree.Path
				}
			}
			_ = a.sessionService.CreateSession(a.ctx, task.ThreadID, task.Title, task.Dir(cwd), string(task.Driver), task.Model, branch)
		}
	}
	a.emit(historyChangedChannel)
	return res, err
}

// SwitchTaskProvider continues one agent task on a different provider without
// losing context, the same way Zeron/Emdash keep per-task sessions first-class
// instead of tying the transcript to a CLI-native session id.
//
// The switch reuses the same thread id: the old CLI session is stopped and a
// new one starts on the new provider, so the card, its size, its transcript
// and its history file all stay one continuous conversation. A handoff built
// from the canonical transcript (live manager history first, history store
// fallback) rides along with the caller's message as a single turn, so no
// extra turn is burned. SQLite stays the metadata index, JSONL stays the
// transcript, the old CLI session is disposable cache.
func (a *App) SwitchTaskProvider(oldThreadID, driver, model string) (domain.Task, error) {
	return a.SwitchTaskProviderWithOptions(oldThreadID, driver, model, nil, "", "", "", nil)
}

func (a *App) SwitchTaskProviderWithOptions(oldThreadID, driver, model string, options domain.ModelOptions, notice, icon, text string, files []domain.FileRef) (domain.Task, error) {
	return a.SwitchTaskAccountWithOptions(oldThreadID, driver, "", model, options, notice, icon, text, files)
}

func (a *App) SwitchTaskAccountWithOptions(oldThreadID, driver, account, model string, options domain.ModelOptions, notice, icon, text string, files []domain.FileRef) (domain.Task, error) {
	newDriver := domain.DriverKind(driver)
	if newDriver == "" {
		return domain.Task{}, fmt.Errorf("provider is required")
	}
	oldTask, ok := a.workspaces.TaskByThread(oldThreadID)
	if !ok {
		return domain.Task{}, fmt.Errorf("no task for thread %s", oldThreadID)
	}
	workspace := a.workspaces.Get(oldTask.WorkspaceID)
	cwd, err := a.requireCwd(a.liveTaskCwd(oldTask))
	if err != nil {
		return domain.Task{}, err
	}
	projectCwd := cwd
	if workspace != nil && workspace.Cwd != "" {
		projectCwd = workspace.Cwd
	}
	if account == "" && newDriver == oldTask.Driver {
		account = domain.NormalizeAccount(oldTask.Account)
	}
	if account, err = a.manager.ResolveAccount(newDriver, projectCwd, account); err != nil {
		return domain.Task{}, err
	}
	oldAccount := domain.NormalizeAccount(oldTask.Account)

	events := a.threadTranscript(oldTask)
	handoff := session.BuildHandoffPrompt(oldTask.Driver, newDriver, events, cwd)

	_ = a.manager.Stop(a.ctx, oldThreadID)
	if _, err := a.manager.Start(a.ctx, newDriver, domain.SessionStartInput{
		ThreadID:   oldThreadID,
		Account:    account,
		Cwd:        cwd,
		Model:      model,
		Options:    options,
		Permission: session.TaskPermission(oldTask.Permission),
	}); err != nil {
		a.workspaces.SetState(oldThreadID, domain.TaskFailed)
		a.recorder.UpdateTaskState(oldThreadID, domain.TaskFailed, "")
		return *oldTask, err
	}
	a.workspaces.SetTaskModel(oldThreadID, newDriver, model, options)
	a.workspaces.SetTaskAccount(oldThreadID, account)
	a.workspaces.SetState(oldThreadID, domain.TaskRunning)
	updated, _ := a.workspaces.TaskByThread(oldThreadID)
	if updated != nil {
		a.recorder.TrackTask(*updated)
	}
	if notice == "" {
		notice = fmt.Sprintf("Switched from %s: %s to %s: %s",
			a.accountLabel(oldTask.Driver, oldAccount), oldTask.Model, a.accountLabel(newDriver, account), model)
	}
	a.manager.RecordNotice(oldThreadID, notice, icon)

	continuation := handoff + "\n\n--- Current request ---\n\n"
	if strings.TrimSpace(text) != "" {
		continuation += text
	} else {
		continuation += "Continue the task above. Preserve existing work in this directory."
	}
	turnID := fmt.Sprintf("%s-turn-%d", oldThreadID, time.Now().UnixMilli())
	// The transcript shows just the user's message; the handoff rides along
	// to the model inside the sent turn so the chat reads as one conversation.
	displayText := strings.TrimSpace(text)
	if displayText == "" {
		displayText = continuation
	}
	a.manager.RecordUserMessage(oldThreadID, turnID, displayText, files...)
	if err := a.manager.Send(a.ctx, domain.SendTurnInput{
		ThreadID: oldThreadID,
		TurnID:   turnID,
		Text:     continuation,
		Files:    files,
	}); err != nil {
		a.workspaces.SetState(oldThreadID, domain.TaskFailed)
		a.recorder.UpdateTaskState(oldThreadID, domain.TaskFailed, "")
		return *oldTask, err
	}
	if a.sessionService != nil {
		branch := ""
		if oldTask.Worktree != nil {
			branch = oldTask.Worktree.Branch
		}
		_ = a.sessionService.CreateSession(a.ctx, oldThreadID, oldTask.Title, cwd, string(newDriver), model, branch)
	}
	if updated, _ := a.workspaces.TaskByThread(oldThreadID); updated != nil {
		return *updated, nil
	}
	return *oldTask, nil
}

// UpdateTaskModel switches model/options on the same provider without losing
// the thread. In-place adapters apply it directly; long-lived procs (Claude)
// are restarted on the same thread with their resume id so the CLI keeps its
// history. A notice divider is recorded into the transcript so reopening
// history shows the switch.
func (a *App) UpdateTaskModel(threadID, driver, model string, options domain.ModelOptions, notice, icon string) (domain.Task, error) {
	task, ok := a.workspaces.TaskByThread(threadID)
	if !ok {
		return domain.Task{}, fmt.Errorf("no task for thread %s", threadID)
	}
	live, alive := a.manager.ThreadSession(threadID)
	if !alive {
		return domain.Task{}, fmt.Errorf("no active session for thread %s", threadID)
	}
	newDriver := domain.DriverKind(driver)
	if newDriver != "" && newDriver != live.Driver {
		return domain.Task{}, fmt.Errorf("provider changed: use SwitchTaskProvider")
	}
	oldModel := live.Model
	if oldModel == "" {
		oldModel = task.Model
	}
	if model != "" && model == oldModel && sameModelOptions(task.Options, options) {
		return *task, nil
	}
	if notice == "" {
		notice = fmt.Sprintf("Switched from %s: %s to %s: %s", domain.DriverLabel(live.Driver), oldModel, domain.DriverLabel(live.Driver), model)
	}
	if a.manager.UpdateModel(threadID, model, options) {
		a.workspaces.SetTaskModel(threadID, live.Driver, model, options)
		if updated, found := a.workspaces.TaskByThread(threadID); found && updated != nil {
			a.recorder.TrackTask(*updated)
		}
		a.manager.RecordNotice(threadID, notice, icon)
		if updated, found := a.workspaces.TaskByThread(threadID); found && updated != nil {
			return *updated, nil
		}
		return *task, nil
	}
	cwd, err := a.requireCwd(a.liveTaskCwd(task))
	if err != nil {
		return domain.Task{}, err
	}
	resume := a.manager.ProviderSessionID(threadID)
	transcript := a.threadTranscript(task)
	_ = a.manager.Stop(a.ctx, threadID)
	if _, err := a.manager.StartResumed(a.ctx, live.Driver, domain.SessionStartInput{
		ThreadID:   threadID,
		Account:    domain.NormalizeAccount(live.Account),
		Cwd:        cwd,
		Model:      model,
		Options:    options,
		Permission: session.TaskPermission(task.Permission),
		Resume:     resume,
	}, transcript); err != nil {
		return *task, err
	}
	a.workspaces.SetTaskModel(threadID, live.Driver, model, options)
	if updated, found := a.workspaces.TaskByThread(threadID); found && updated != nil {
		a.recorder.TrackTask(*updated)
	}
	a.manager.RecordNotice(threadID, notice, icon)
	if updated, found := a.workspaces.TaskByThread(threadID); found && updated != nil {
		return *updated, nil
	}
	return *task, nil
}

func (a *App) liveTaskCwd(task *domain.Task) string {
	if live, ok := a.manager.ThreadSession(task.ThreadID); ok && live.Cwd != "" {
		return live.Cwd
	}
	workspaceCwd := ""
	if workspace := a.workspaces.Get(task.WorkspaceID); workspace != nil {
		workspaceCwd = workspace.Cwd
	}
	return task.Dir(workspaceCwd)
}

func (a *App) threadTranscript(task *domain.Task) []domain.RuntimeEvent {
	events := a.manager.History(task.ThreadID)
	if len(events) == 0 && a.historyStore != nil {
		if loaded, err := a.historyStore.Load(task.WorkspaceID); err == nil {
			events = loaded.Transcripts[task.ThreadID]
		}
	}
	return events
}

func sameModelOptions(a, b domain.ModelOptions) bool {
	if len(a) != len(b) {
		return false
	}
	for key, valueA := range a {
		valueB, ok := b[key]
		if !ok || fmt.Sprint(valueA) != fmt.Sprint(valueB) {
			return false
		}
	}
	return true
}

func (a *App) ListWorkspaces() []domain.Workspace {
	return a.workspaces.List()
}

func (a *App) WorkspaceTasks(workspaceID string) []domain.Task {
	return a.workspaces.Tasks(workspaceID)
}

// TaskHandoff summarises a finished task's branch: size, commit count, and
// whether it merges cleanly. Reporting only — Composer never merges for you.
func (a *App) TaskHandoff(threadID string) (domain.TaskHandoff, error) {
	task, ok := a.workspaces.TaskByThread(threadID)
	if !ok {
		return domain.TaskHandoff{}, fmt.Errorf("no task for thread %s", threadID)
	}
	if task.Worktree == nil {
		return domain.TaskHandoff{TaskID: task.ID, Summary: task.Summary}, nil
	}

	repo, ok := git.Open(a.ctx, task.Worktree.Path)
	if !ok {
		return domain.TaskHandoff{}, fmt.Errorf("worktree %s is not a git repository", task.Worktree.Path)
	}

	handoff, err := repo.Handoff(a.ctx, task.Worktree)
	if err != nil {
		return domain.TaskHandoff{}, err
	}
	handoff.TaskID = task.ID
	handoff.Summary = task.Summary
	return handoff, nil
}
