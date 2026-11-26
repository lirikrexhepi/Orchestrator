package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"composer/internal/domain"
	"composer/internal/git"
	"composer/internal/history"
	"composer/internal/logger"
	"composer/internal/projects"
	"composer/internal/session"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// IsGitRepo reports whether a directory can back worktree isolation, so the UI
// can ask about it only when the answer is meaningful. An empty path means the
// active project; without one, worktree isolation is unavailable.
func (a *App) IsGitRepo(dir string) bool {
	dir = a.resolveCwd(dir)
	if dir == "" {
		return false
	}
	_, ok := git.Open(a.ctx, dir)
	return ok
}

// ListProjects reports every saved project, newest added first.
func (a *App) ListProjects() []projects.Project {
	return a.projects.List()
}

// ActiveProject reports the project agents currently start in, if any.
func (a *App) ActiveProject() *projects.Project {
	project, ok := a.projects.Active()
	if !ok {
		return nil
	}
	return &project
}

// ChooseProject opens the OS folder picker and saves what was chosen.
//
// Returns nil when the dialog is cancelled: backing out of a file picker is a
// normal thing to do, not an error worth surfacing to the user.
func (a *App) ChooseProject() (*projects.Project, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a project folder",
	})
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, nil
	}
	return a.AddProject(dir)
}

// AddProject saves a directory by path, for callers that already know it.
//
// Whether the directory is a git repo is resolved here rather than in the
// store, so the projects package stays free of a dependency on git.
func (a *App) AddProject(dir string) (*projects.Project, error) {
	_, isGit := git.Open(a.ctx, dir)
	project, err := a.projects.Add(dir, isGit)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// SelectProject makes a saved project the one agents start in.
func (a *App) SelectProject(id string) (*projects.Project, error) {
	project, err := a.projects.Activate(id)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// MoveProject places a saved project at an explicit picker position.
func (a *App) MoveProject(id string, toIndex int) error {
	return a.projects.Move(id, toIndex)
}

// RemoveProject forgets a project. The directory itself is never touched.
func (a *App) RemoveProject(id string) error {
	return a.projects.Remove(id)
}

// ChooseProjectLocation opens the OS folder picker to point an existing saved
// project at its new location on disk, migrating its chats and project memory.
// Returns nil when the dialog is cancelled.
func (a *App) ChooseProjectLocation(id string) (*projects.Project, error) {
	title := "Locate project folder"
	if existing, ok := a.projects.ByID(id); ok && existing.Name != "" {
		title = fmt.Sprintf("Locate %s folder", existing.Name)
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, nil
	}
	return a.RelocateProject(id, dir)
}

// RelocateProject updates a saved project's path in place and migrates every
// stored chat workspace, Claude session file, and project memory bound to the
// old path so existing chats stay attached and resumable.
func (a *App) RelocateProject(id, newDir string) (*projects.Project, error) {
	if strings.TrimSpace(newDir) == "" {
		return nil, fmt.Errorf("choose a new folder for the project")
	}
	cwd, err := a.requireCwd(newDir)
	if err != nil {
		return nil, err
	}
	existing, ok := a.projects.ByID(id)
	if !ok {
		return nil, projects.ErrNotFound
	}
	if isSamePath(existing.Path, cwd) {
		return &existing, nil
	}

	if a.recorder != nil {
		_ = a.recorder.FlushAll()
	}

	var toMigrate []history.Meta
	if a.historyStore != nil {
		metas, err := a.historyStore.List()
		if err != nil {
			return nil, err
		}
		for _, meta := range metas {
			if !isSamePath(meta.Workspace.Cwd, existing.Path) {
				continue
			}
			for _, task := range meta.Tasks {
				if a.manager != nil && a.manager.TurnInProgress(task.ThreadID) {
					return nil, fmt.Errorf("wait for %q to finish before relocating %s", task.Title, existing.Name)
				}
			}
			toMigrate = append(toMigrate, meta)
		}
	}

	_, isGit := git.Open(a.ctx, cwd)
	oldPath, updated, err := a.projects.Relocate(id, cwd, isGit)
	if err != nil {
		return nil, err
	}

	moved := 0
	for _, meta := range toMigrate {
		if err := a.moveWorkspaceMeta(meta, cwd); err != nil {
			logger.Warnf("App", "RelocateProject: failed to migrate workspace %s to %s: %v", meta.Workspace.ID, cwd, err)
			continue
		}
		moved++
	}
	if a.memory != nil {
		if err := a.memory.MoveProjectMemory(oldPath, cwd); err != nil {
			logger.Warnf("App", "RelocateProject: failed to migrate project memory from %s to %s: %v", oldPath, cwd, err)
		}
	}

	logger.Infof("App", "RelocateProject: %s moved from %s to %s (%d/%d workspaces migrated)", updated.ID, oldPath, cwd, moved, len(toMigrate))
	a.emit(projectsChangedChannel)
	if moved > 0 {
		a.emit(historyChangedChannel)
	}
	return &updated, nil
}

// ProjectIcon reports a saved project's app icon as a data URL, or an empty
// string when the project has no detectable favicon. The desktop picker shows
// it in place of the folder glyph when the user enables project favicons.
func (a *App) ProjectIcon(id string) string {
	var path string
	for _, project := range a.projects.List() {
		if project.ID == id {
			path = project.Path
			break
		}
	}
	if path == "" {
		return ""
	}
	data, contentType, err := projects.FindCached(path)
	if err != nil {
		return ""
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (a *App) GetProjectMemory(cwd string) string {
	if a.memory == nil {
		return ""
	}
	return a.memory.LoadProjectMemory(a.resolveCwd(cwd))
}

func (a *App) SaveProjectMemory(cwd, content string) error {
	if a.memory == nil {
		return fmt.Errorf("memory store not initialized")
	}
	dir := a.resolveCwd(cwd)
	if dir == "" {
		return fmt.Errorf("choose a project folder first")
	}
	return a.memory.SaveProjectMemory(dir, content)
}

func (a *App) GetWorkspaceRecap(workspaceID string) string {
	if a.memory == nil {
		return ""
	}
	return a.memory.LoadRecap(workspaceID)
}

// ContextStatus reports how full a thread's context is, mirroring Zeron's
// context ring (amber 75%, red 90%) and Orchestrator's /status. Chars are the local
// proxy; token usage refines it when the driver reports a context window.
func (a *App) ContextStatus(threadID string) map[string]any {
	events := a.manager.History(threadID)
	chars := 0
	for _, event := range events {
		chars += len(event.Text)
		if event.Tool != nil {
			chars += len(event.Tool.Output)
		}
	}
	usedPercent := float64(chars) / 100000.0 * 100.0
	if usedPercent > 100 {
		usedPercent = 100
	}
	status := "ok"
	if usedPercent >= 90 {
		status = "red"
	} else if usedPercent >= 75 {
		status = "amber"
	}
	needsCompact := len(events) >= 1500 || chars >= 100000
	return map[string]any{
		"events":       len(events),
		"chars":        chars,
		"usedPercent":  usedPercent,
		"status":       status,
		"needsCompact": needsCompact,
	}
}

// CompactWorkspace builds a structured recap of a workspace conversation and
// stores it as RECAP.md, like Orchestrator's recap panel and Cursor's growing shared
// context. Rule-based today (no extra model call); pass llmText from the caller
// when a summarizer turn has produced one to store that instead.
func (a *App) CompactWorkspace(workspaceID, llmText string) (string, error) {
	loaded, err := a.historyStore.Load(workspaceID)
	if err != nil {
		return "", err
	}
	var events []domain.RuntimeEvent
	for _, list := range loaded.Transcripts {
		events = append(events, list...)
	}
	cwd := loaded.Meta.Workspace.Cwd
	recap := strings.TrimSpace(llmText)
	if recap == "" {
		recap = session.RuleFallbackRecap(events, cwd)
	}
	if recap == "" {
		return "", fmt.Errorf("nothing to compact")
	}
	if err := a.memory.SaveRecap(workspaceID, recap); err != nil {
		return "", err
	}
	return recap, nil
}

// RecapPrompt returns the prompt to send to a summarizer turn for a workspace,
// so the frontend (or a background job) can run /compact through any provider.
func (a *App) RecapPrompt(workspaceID string) (string, error) {
	loaded, err := a.historyStore.Load(workspaceID)
	if err != nil {
		return "", err
	}
	var events []domain.RuntimeEvent
	for _, list := range loaded.Transcripts {
		events = append(events, list...)
	}
	return session.BuildRecapPrompt(events, loaded.Meta.Workspace.Cwd), nil
}
