package session

import (
	"strings"
	"testing"

	"composer/internal/domain"
	"composer/internal/drivers"
	"composer/internal/provider"
)

func testConstructor() *Constructor {
	manager := NewManager(provider.NewRegistry(drivers.All()...))
	workspaces := NewWorkspaces()
	coordinator := NewCoordinator(manager)
	spawner := NewSpawner(manager, workspaces)
	return NewConstructor(manager, coordinator, spawner, workspaces)
}

func TestConstructorAutoDefault(t *testing.T) {
	c := testConstructor()
	if !c.Auto() {
		t.Fatal("constructor should auto-execute by default (Cursor parity)")
	}
}

func TestConstructorIgnoresQuestions(t *testing.T) {
	c := testConstructor()
	c.SetAuto(true)
	c.Remember(Config{Driver: "claude", Cwd: "/tmp"})
	turnID := "turn-1"
	c.Observe(domain.RuntimeEvent{Kind: domain.EventAgentMessage, ThreadID: CoordinatorThreadID, TurnID: turnID, Text: "I am fine, how can I help?"})
	c.Observe(domain.RuntimeEvent{Kind: domain.EventTurnCompleted, ThreadID: CoordinatorThreadID, TurnID: turnID})
	if latest := c.Latest(); latest != nil {
		t.Fatalf("question should not produce a plan, got %+v", latest)
	}
}

func TestConstructorListAgentsEmpty(t *testing.T) {
	c := testConstructor()
	if agents := c.ListAgents(); len(agents) != 0 {
		t.Fatalf("expected no agents, got %d", len(agents))
	}
}

func TestConstructorListAgentsSeesWorkers(t *testing.T) {
	manager := NewManager(provider.NewRegistry(drivers.All()...))
	workspaces := NewWorkspaces()
	coordinator := NewCoordinator(manager)
	spawner := NewSpawner(manager, workspaces)
	c := NewConstructor(manager, coordinator, spawner, workspaces)

	ws := workspaces.Create("demo", "do things", "/tmp")
	if _, ok := workspaces.AddTask(ws.ID, domain.Task{ThreadID: "task-1", Title: "Say hello", Driver: domain.DriverClaude, State: domain.TaskRunning}); !ok {
		t.Fatal("AddTask failed")
	}
	agents := c.ListAgents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].ThreadID != "task-1" || agents[0].Live {
		t.Fatalf("unexpected agent view: %+v", agents[0])
	}
}

func TestConstructorFormatAgentManifestWorktrees(t *testing.T) {
	manager := NewManager(provider.NewRegistry(drivers.All()...))
	workspaces := NewWorkspaces()
	coordinator := NewCoordinator(manager)
	spawner := NewSpawner(manager, workspaces)
	c := NewConstructor(manager, coordinator, spawner, workspaces)

	projectDir := `C:\Users\PC\Desktop\test-orchestrator-app`
	ws := workspaces.Create("test-app", "Build app", projectDir)

	// Agent 1: In a git worktree
	worktreePath := `C:\Users\PC\AppData\Local\composer\worktrees\test-app\frontend`
	workspaces.AddTask(ws.ID, domain.Task{
		ThreadID: "task-1-frontend",
		Title:    "Frontend UI",
		Driver:   domain.DriverAntigravity,
		State:    domain.TaskRunning,
		Worktree: &domain.Worktree{
			Path:   worktreePath,
			Branch: "composer/frontend-ui",
		},
	})

	// Agent 2: In the root project dir
	workspaces.AddTask(ws.ID, domain.Task{
		ThreadID: "task-2-backend",
		Title:    "Backend API",
		Driver:   domain.DriverAntigravity,
		State:    domain.TaskComplete,
		Summary:  "Implemented GET and POST /api/tasks",
	})

	// Agent 3: In an unrelated project
	otherWs := workspaces.Create("other-app", "Other", `C:\Users\PC\Desktop\other-project`)
	workspaces.AddTask(otherWs.ID, domain.Task{
		ThreadID: "task-3-unrelated",
		Title:    "Unrelated Task",
		Driver:   domain.DriverAntigravity,
		State:    domain.TaskRunning,
	})

	// Format manifest for projectDir
	manifest := c.FormatAgentManifest(projectDir)
	if manifest == "" {
		t.Fatal("manifest should not be empty for project with active agents")
	}

	// Verify task-1-frontend and task-2-backend are included
	if !strings.Contains(manifest, "task-1-frontend") {
		t.Errorf("manifest missing worktree agent task-1-frontend:\n%s", manifest)
	}
	if !strings.Contains(manifest, "composer/frontend-ui") {
		t.Errorf("manifest missing worktree branch info:\n%s", manifest)
	}
	if !strings.Contains(manifest, "task-2-backend") {
		t.Errorf("manifest missing backend agent task-2-backend:\n%s", manifest)
	}
	if !strings.Contains(manifest, "Implemented GET and POST") {
		t.Errorf("manifest missing agent summary note:\n%s", manifest)
	}

	// Verify unrelated agent from other project is excluded
	if strings.Contains(manifest, "task-3-unrelated") {
		t.Errorf("manifest incorrectly included agent from unrelated project:\n%s", manifest)
	}
}
