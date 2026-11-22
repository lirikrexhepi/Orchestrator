package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"composer/internal/domain"
)

// AgentView is what The Orchestrator shows for every worker it owns, mirroring
// how Cursor's coordinator lists the agents running under a Project. Live
// reflects whether the CLI process is still attached; the transcript survives
// in history either way so a dead agent can be resumed or continued elsewhere.
type AgentView struct {
	ThreadID    string            `json:"threadId"`
	Title       string            `json:"title"`
	Driver      domain.DriverKind `json:"driver"`
	Account     string            `json:"account,omitempty"`
	Model       string            `json:"model,omitempty"`
	State       domain.TaskState  `json:"state"`
	Cwd         string            `json:"cwd"`
	ProjectCwd  string            `json:"projectCwd,omitempty"`
	WorkspaceID string            `json:"workspaceId"`
	Branch      string            `json:"branch,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Live        bool              `json:"live"`
}

// LatestPlan is the most recent coordinator plan the constructor executed (or
// skipped). The frontend uses it to reconcile: if the user confirms a plan the
// constructor already launched, it can show the agents instead of spawning
// duplicates.
type LatestPlan struct {
	TurnID      string        `json:"turnId"`
	Tasks       []TaskRequest `json:"tasks"`
	WorkspaceID string        `json:"workspaceId,omitempty"`
	Executed    bool          `json:"executed"`
	At          int64         `json:"at"`
}

// Constructor is The Orchestrator backend: a Cursor Projects style coordinator
// engine. The coordinator CLI plans in prose plus a machine-readable task
// block; the constructor — not the frontend — executes that block, tracks
// every worker, and stays continuous per project.
//
// The frontend stays a thin view: it sends messages and renders events. Direct
// agent DM is untouched (deck sends straight to Manager), so unlike Cursor the
// user can still talk to any worker themselves.
type Constructor struct {
	manager     *Manager
	coordinator *Coordinator
	spawner     *Spawner
	workspaces  *Workspaces

	mu       sync.Mutex
	auto     bool
	lastCfg  Config
	executed map[string]bool
	latest   *LatestPlan
	onSpawn  func(SpawnResult)
	// resumeID resolves a thread's stored provider session id (history meta),
	// used to revive a worker whose CLI is no longer running.
	resumeID func(threadID string) string
}

// SetResumeResolver attaches the lookup for stored provider session ids.
func (c *Constructor) SetResumeResolver(fn func(threadID string) string) {
	c.mu.Lock()
	c.resumeID = fn
	c.mu.Unlock()
}

// LastConfig reports the coordinator selection remembered from the latest send.
func (c *Constructor) LastConfig() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCfg
}

func NewConstructor(manager *Manager, coordinator *Coordinator, spawner *Spawner, workspaces *Workspaces) *Constructor {
	return &Constructor{
		manager:     manager,
		coordinator: coordinator,
		spawner:     spawner,
		workspaces:  workspaces,
		auto:        true,
		executed:    make(map[string]bool),
	}
}

func (c *Constructor) SetAuto(auto bool) {
	c.mu.Lock()
	c.auto = auto
	c.mu.Unlock()
}

func (c *Constructor) Auto() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auto
}

func (c *Constructor) SetOnSpawn(fn func(SpawnResult)) {
	c.mu.Lock()
	c.onSpawn = fn
	c.mu.Unlock()
}

// Remember stores the coordinator's active driver/model/cwd so a completed
// plan can be executed with the same selection without the frontend resending
// it. Called on every coordinator send.
func (c *Constructor) Remember(cfg Config) {
	c.mu.Lock()
	c.lastCfg = cfg
	c.mu.Unlock()
}

// MarkExecuted records a turn the frontend launched manually (plan-card
// confirm), so the backend does not launch it a second time.
func (c *Constructor) MarkExecuted(turnID string) {
	if turnID == "" {
		return
	}
	c.mu.Lock()
	c.executed[turnID] = true
	c.mu.Unlock()
}

func (c *Constructor) Latest() *LatestPlan {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return nil
	}
	cp := *c.latest
	cp.Tasks = append([]TaskRequest(nil), c.latest.Tasks...)
	return &cp
}

// Observe tracks coordinator events. Legacy ParseTasks scraping and auto-execution
// was disabled so quoting a code block in chat can never trigger accidental spawns.
func (c *Constructor) Observe(event domain.RuntimeEvent) {
	if event.ThreadID != CoordinatorThreadID {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
}

// EnsureLive starts a worker whose CLI is not running (after an app restart
// or a stop), resuming its stored provider session with its own permission
// mode. A live thread is left alone; the manager revives dead processes of
// registered threads itself on send.
func (c *Constructor) EnsureLive(ctx context.Context, threadID string) error {
	if c.manager.HasSession(threadID) {
		return nil
	}
	task, ok := c.workspaces.TaskByThread(threadID)
	if !ok {
		return fmt.Errorf("no task for thread %s", threadID)
	}
	workspaceCwd := ""
	if ws := c.workspaces.Get(task.WorkspaceID); ws != nil {
		workspaceCwd = ws.Cwd
	}
	cwd := task.Dir(workspaceCwd)
	c.mu.Lock()
	resolve := c.resumeID
	c.mu.Unlock()
	resume := ""
	if resolve != nil {
		resume = resolve(threadID)
	}
	_, err := c.manager.StartResumed(ctx, task.Driver, domain.SessionStartInput{
		ThreadID:   threadID,
		Account:    domain.NormalizeAccount(task.Account),
		Cwd:        cwd,
		Model:      task.Model,
		Options:    task.Options,
		Permission: TaskPermission(task.Permission),
		Resume:     resume,
	}, nil)
	return err
}

// ListAgents returns every worker The Orchestrator owns across all workspaces,
// newest workspace first, with live attachment state. This is the coordinator's
// view Cursor shows per Project; here it spans projects and can be filtered by
// cwd on the caller side.
func (c *Constructor) ListAgents() []AgentView {
	live := make(map[string]domain.Session)
	for _, sess := range c.manager.Sessions() {
		if sess.ThreadID == CoordinatorThreadID {
			continue
		}
		live[sess.ThreadID] = sess
	}
	out := make([]AgentView, 0)
	for _, workspace := range c.workspaces.List() {
		for _, task := range c.workspaces.Tasks(workspace.ID) {
			view := AgentView{
				ThreadID:    task.ThreadID,
				Title:       task.Title,
				Driver:      task.Driver,
				Account:     domain.NormalizeAccount(task.Account),
				Model:       task.Model,
				State:       task.State,
				WorkspaceID: task.WorkspaceID,
				Cwd:         task.Dir(workspace.Cwd),
				ProjectCwd:  workspace.Cwd,
				Summary:     task.Summary,
			}
			if task.Worktree != nil {
				view.Branch = task.Worktree.Branch
			}
			if sess, ok := live[task.ThreadID]; ok {
				view.Live = true
				if view.Model == "" {
					view.Model = sess.Model
				}
				if sess.Cwd != "" && task.Worktree == nil {
					view.Cwd = sess.Cwd
				}
			}
			out = append(out, view)
		}
	}
	return out
}

// FormatAgentManifest produces a compact, token-efficient summary of active agents
// in the project (mirroring Cursor's agent roster and Claude Projects context)
// to inject into the coordinator's prompt without blowing up token usage.
func (c *Constructor) FormatAgentManifest(cwd string) string {
	agents := c.ListAgents()
	if len(agents) == 0 {
		return ""
	}

	cleanCwd := filepath.Clean(strings.TrimSpace(cwd))
	var active []AgentView
	for _, a := range agents {
		// Only include live or recently active agents (not closed unless live)
		if a.State == domain.TaskClosed && !a.Live {
			continue
		}
		// Filter by cwd if specified (match against ProjectCwd or Cwd)
		if cleanCwd != "" {
			match := false
			for _, checkDir := range []string{a.ProjectCwd, a.Cwd} {
				if checkDir == "" {
					continue
				}
				if isWithin(filepath.Clean(checkDir), cleanCwd) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		active = append(active, a)
	}

	if len(active) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Active Agents in this Project:\n")
	for _, a := range active {
		status := "IDLE"
		if a.State == domain.TaskRunning {
			status = "RUNNING"
		} else if a.State == domain.TaskFailed {
			status = "FAILED"
		} else if a.State == domain.TaskComplete {
			status = "COMPLETED"
		}

		branchInfo := ""
		if a.Branch != "" {
			branchInfo = fmt.Sprintf(" | Branch: %s", a.Branch)
		}
		summaryInfo := ""
		if a.Summary != "" {
			s := a.Summary
			if len(s) > 60 {
				s = s[:60] + "..."
			}
			summaryInfo = fmt.Sprintf(" | Last note: %s", s)
		}

		b.WriteString(fmt.Sprintf("- [ID: %s] Title: %q | Status: %s%s%s\n", a.ThreadID, a.Title, status, branchInfo, summaryInfo))
	}
	b.WriteString("(You can route follow-ups to an existing agent by setting \"action\":\"message\" and \"targetThreadId\":\"<ID>\" in composer:tasks, or spawn a new agent with \"action\":\"spawn\".)")
	return b.String()
}

// isWithin reports whether path is dir or lies beneath it (case-insensitive,
// separator-aware, so C:\proj does not match C:\project).
func isWithin(path, dir string) bool {
	p, d := strings.ToLower(path), strings.ToLower(dir)
	if p == d {
		return true
	}
	if !strings.HasPrefix(p, d) {
		return false
	}
	if strings.HasSuffix(d, "/") || strings.HasSuffix(d, "\\") {
		return true
	}
	next := p[len(d)]
	return next == '/' || next == '\\'
}
