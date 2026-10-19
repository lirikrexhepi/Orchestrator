package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"composer/internal/domain"
	"composer/internal/git"
	"composer/internal/logger"
)

// SpawnRequest is one task the orchestrator asked for. Driver and model are
// optional per task; when empty the plan-wide choice in SpawnOptions applies.
type SpawnRequest struct {
	Title   string              `json:"title"`
	Prompt  string              `json:"prompt"`
	Driver  domain.DriverKind   `json:"driver,omitempty"`
	Account string              `json:"account,omitempty"`
	Model   string              `json:"model,omitempty"`
	Options domain.ModelOptions `json:"options,omitempty"`
	// Cwd overrides the plan-wide directory, so a task naming another project
	// starts there instead of in the orchestrator's own folder.
	Cwd string `json:"cwd,omitempty"`
	// Preamble is context sent to the agent ahead of the prompt (project
	// memory) but not recorded as part of the user's message.
	Preamble string              `json:"preamble,omitempty"`
	Skills   *domain.SkillPolicy `json:"skills,omitempty"`
	Files    []domain.FileRef    `json:"files,omitempty"`
}

// SpawnOptions carries the choices the user makes once per plan.
type SpawnOptions struct {
	Driver      domain.DriverKind     `json:"driver"`
	Account     string                `json:"account,omitempty"`
	Model       string                `json:"model,omitempty"`
	Options     domain.ModelOptions   `json:"options,omitempty"`
	Cwd         string                `json:"cwd"`
	UseWorktree bool                  `json:"useWorktree"`
	WorkspaceID string                `json:"workspaceId,omitempty"`
	Title       string                `json:"title,omitempty"`
	Prompt      string                `json:"prompt,omitempty"`
	Permission  domain.PermissionMode `json:"permissionMode,omitempty"`
}

// Spawner turns a plan into running agent sessions, each optionally isolated in
// its own git worktree so parallel work cannot collide.
type Spawner struct {
	manager    *Manager
	workspaces *Workspaces

	seq atomic.Uint64
	mu  sync.Mutex

	// tracker persists the workspace and its tasks as they are created, so a
	// session is reopenable even if the app dies mid-run.
	tracker SpawnTracker
}

// SpawnTracker is notified as a plan becomes real, so history can record the
// workspace, its orchestrator transcript, and each task.
type SpawnTracker interface {
	// BindCoordinator attributes the orchestrator conversation to the new
	// workspace and returns the thread id it was stored under.
	BindCoordinator(workspaceID string) string
	OpenWorkspace(workspace domain.Workspace, coordinatorThreadID string)
	TrackTask(task domain.Task)
}

func NewSpawner(manager *Manager, workspaces *Workspaces) *Spawner {
	return &Spawner{manager: manager, workspaces: workspaces}
}

// SetTracker attaches history recording.
func (s *Spawner) SetTracker(tracker SpawnTracker) {
	s.mu.Lock()
	s.tracker = tracker
	s.mu.Unlock()
}

// SpawnResult reports what was created, including per-task failures so a
// partial plan still surfaces the tasks that did start.
type SpawnResult struct {
	Workspace domain.Workspace `json:"workspace"`
	Tasks     []domain.Task    `json:"tasks"`
	Errors    []string         `json:"errors,omitempty"`
}

// Spawn creates a workspace and one session per request. Failures are collected
// rather than aborting, so one bad task cannot sink the others.
func (s *Spawner) Spawn(ctx context.Context, requests []SpawnRequest, opts SpawnOptions) (SpawnResult, error) {
	if len(requests) == 0 {
		return SpawnResult{}, fmt.Errorf("no tasks to spawn")
	}

	cwd := opts.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
		logger.Warnf("Session", "Spawn without a directory; falling back to process directory %q", cwd)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var workspace *domain.Workspace
	if opts.WorkspaceID != "" {
		workspace = s.workspaces.Get(opts.WorkspaceID)
	}
	if workspace == nil {
		workspace = s.workspaces.Create(firstNonEmpty(opts.Title, requests[0].Title), opts.Prompt, cwd)
		// Recorded before any agent starts: a crash mid-spawn should still leave a
		// reopenable session containing the plan that caused it.
		if s.tracker != nil {
			coordinatorThreadID := s.tracker.BindCoordinator(workspace.ID)
			s.tracker.OpenWorkspace(*workspace, coordinatorThreadID)
		}
	}

	var repo *git.Repo
	if opts.UseWorktree {
		if found, ok := git.Open(ctx, cwd); ok {
			repo = found
		}
	}

	result := SpawnResult{Workspace: *workspace}
	for _, request := range requests {
		// A task pointing at another directory gets its own repo handle, so
		// worktrees are cut from the project it actually works on.
		taskCwd, taskRepo := cwd, repo
		if request.Cwd != "" && request.Cwd != cwd {
			taskCwd = request.Cwd
			taskRepo = nil
			if opts.UseWorktree {
				if found, ok := git.Open(ctx, taskCwd); ok {
					taskRepo = found
				}
			}
		}

		task, err := s.spawnOne(ctx, workspace, request, opts, taskCwd, taskRepo)
		if err != nil {
			result.Errors = append(result.Errors, request.Title+": "+err.Error())
			continue
		}
		if s.tracker != nil {
			s.tracker.TrackTask(*task)
		}
		result.Tasks = append(result.Tasks, *task)
	}

	if len(result.Tasks) == 0 {
		return result, fmt.Errorf("no tasks could be started")
	}
	return result, nil
}

func (s *Spawner) spawnOne(
	ctx context.Context,
	workspace *domain.Workspace,
	request SpawnRequest,
	opts SpawnOptions,
	cwd string,
	repo *git.Repo,
) (*domain.Task, error) {
	threadID := "task-" + strconv.FormatUint(s.seq.Add(1), 10) + "-" + strconv.FormatInt(time.Now().UnixMilli(), 36)

	// Each task may target its own agent; the plan-wide selection is the default.
	driver := request.Driver
	if driver == "" {
		driver = opts.Driver
	}
	model := request.Model
	options := request.Options
	if model == "" {
		model, options = opts.Model, opts.Options
	}
	requested := request.Account
	if requested == "" && driver == opts.Driver {
		requested = opts.Account
	}
	account, err := s.manager.ResolveAccount(driver, cwd, requested)
	if err != nil {
		return nil, err
	}

	workdir := cwd
	var worktree *domain.Worktree
	if repo != nil {
		created, err := s.makeWorktree(ctx, repo, request.Title)
		if err != nil {
			return nil, err
		}
		worktree, workdir = created, created.Path
	}

	permission := opts.Permission
	if permission == "" {
		permission = domain.PermissionBypass
	}

	task, ok := s.workspaces.AddTask(workspace.ID, domain.Task{
		Permission: permission,
		ThreadID: threadID,
		Title:    request.Title,
		Prompt:   request.Prompt,
		Driver:   driver,
		Account:  account,
		Model:    model,
		Options:  options,
		State:    domain.TaskRunning,
		Worktree: worktree,
		Cwd:      workdir,
	})
	if !ok {
		return nil, fmt.Errorf("workspace %s is gone", workspace.ID)
	}

	// Register task with history tracker before starting the session or recording
	// user messages, so r.workspaceOf[threadID] is set and the initial turn prompt
	// is recorded in the transcript rather than dropped.
	if s.tracker != nil {
		s.tracker.TrackTask(*task)
	}

	if _, err := s.manager.Start(ctx, driver, domain.SessionStartInput{
		ThreadID:   threadID,
		Account:    account,
		Cwd:        workdir,
		Model:      model,
		Options:    options,
		Permission: permission,
		Skills:     request.Skills,
	}); err != nil {
		s.workspaces.SetState(threadID, domain.TaskFailed)
		return nil, err
	}

	turnID := threadID + "-turn-1"
	s.manager.RecordUserMessage(threadID, turnID, request.Prompt, request.Files...)

	body := request.Prompt
	if request.Preamble != "" {
		body = request.Preamble + "\n\n--- Current request ---\n\n" + request.Prompt
	}
	if err := s.manager.Send(ctx, domain.SendTurnInput{
		ThreadID: threadID,
		TurnID:   turnID,
		Text:     body,
		Files:    request.Files,
	}); err != nil {
		s.workspaces.SetState(threadID, domain.TaskFailed)
		return nil, err
	}

	return task, nil
}

// makeWorktree places the checkout under the user's app data rather than beside
// the project, so working across many repos never litters their directories.
func (s *Spawner) makeWorktree(ctx context.Context, repo *git.Repo, title string) (*domain.Worktree, error) {
	root, err := git.WorktreeRoot(repo.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	slug := git.Slug(title)
	branch := repo.UniqueBranch("composer/", slug)
	path := filepath.Join(root, filepath.Base(branch))

	// A leftover directory from a previous run would make `worktree add` fail.
	if _, err := os.Stat(path); err == nil {
		path = path + "-" + strconv.FormatInt(time.Now().UnixMilli(), 36)
	}

	worktree, err := repo.AddWorktree(ctx, path, branch, "")
	if err != nil {
		return nil, err
	}
	worktree.CreatedAt = time.Now().UnixMilli()
	return worktree, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
