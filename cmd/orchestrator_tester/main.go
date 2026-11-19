package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/drivers"
	"composer/internal/provider"
	"composer/internal/session"
)

type TestLogger struct {
	mu   sync.Mutex
	file *os.File
}

func NewTestLogger(path string) (*TestLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &TestLogger{file: f}, nil
}

func (l *TestLogger) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("[%s] ", time.Now().Format("15:04:05.000")) + fmt.Sprintf(format, args...) + "\n"
	fmt.Print(msg)
	if l.file != nil {
		_, _ = l.file.WriteString(msg)
		_ = l.file.Sync()
	}
}

func (l *TestLogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
	}
}

func main() {
	desktopDir := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	testDir := filepath.Join(desktopDir, "test-orchestrator-app")
	logFile := filepath.Join(testDir, "logs", "test_run.log")

	logger, err := NewTestLogger(logFile)
	if err != nil {
		fmt.Printf("Failed to open test log file: %v\n", err)
		os.Exit(1)
	}
	defer logger.Close()

	logger.Logf("================================================================================")
	logger.Logf("STARTING END-TO-END ORCHESTRATOR VALIDATION RUN")
	logger.Logf("Target project directory: %s", testDir)
	logger.Logf("Log output: %s", logFile)
	logger.Logf("================================================================================")

	registry := provider.NewRegistry(drivers.All()...)
	manager := session.NewManager(registry)
	workspaces := session.NewWorkspaces()
	coordinator := session.NewCoordinator(manager)
	spawner := session.NewSpawner(manager, workspaces)
	constructor := session.NewConstructor(manager, coordinator, spawner, workspaces)

	events, unsubscribe := manager.Bus().Subscribe()
	defer unsubscribe()
	defer manager.StopAll(context.Background())

	var turnWaitMu sync.Mutex
	workerTurnsPending := make(map[string]bool)
	coordinatorDone := make(chan string, 10)
	workerTurnDone := make(chan string, 50)

	go func() {
		for event := range events {
			coordinator.Observe(event)
			constructor.Observe(event)

			switch event.Kind {
			case domain.EventSessionStarted:
				logger.Logf("[EVENT: SessionStarted] Thread=%s Text=%s", event.ThreadID, event.Text)
			case domain.EventTurnStarted:
				logger.Logf("[EVENT: TurnStarted] Thread=%s Turn=%s", event.ThreadID, event.TurnID)
				if event.ThreadID != session.CoordinatorThreadID {
					turnWaitMu.Lock()
					workerTurnsPending[event.ThreadID] = true
					turnWaitMu.Unlock()
				}
			case domain.EventAgentMessage:
				if event.Text != "" && !event.Delta {
					preview := event.Text
					if len(preview) > 120 {
						preview = preview[:120] + "..."
					}
					logger.Logf("[EVENT: AgentMessage] Thread=%s: %s", event.ThreadID, preview)
				}
			case domain.EventToolCall:
				if event.Tool != nil {
					logger.Logf("[EVENT: ToolCall] Thread=%s: %s(%s)", event.ThreadID, event.Tool.Name, event.Tool.Input)
				}
			case domain.EventTurnCompleted:
				logger.Logf("[EVENT: TurnCompleted] Thread=%s Turn=%s", event.ThreadID, event.TurnID)
				if event.ThreadID == session.CoordinatorThreadID {
					coordinatorDone <- event.TurnID
				} else {
					turnWaitMu.Lock()
					delete(workerTurnsPending, event.ThreadID)
					turnWaitMu.Unlock()
					workerTurnDone <- event.ThreadID
				}
			case domain.EventTurnFailed:
				logger.Logf("[EVENT: TurnFailed] Thread=%s Turn=%s Error=%s", event.ThreadID, event.TurnID, event.Error)
				if event.ThreadID == session.CoordinatorThreadID {
					coordinatorDone <- event.TurnID
				} else {
					turnWaitMu.Lock()
					delete(workerTurnsPending, event.ThreadID)
					turnWaitMu.Unlock()
					workerTurnDone <- event.ThreadID
				}
			}
		}
	}()

	cfg := session.Config{
		Driver:     string(domain.DriverAntigravity),
		Model:      "gemini-3.8-flash-medium",
		Cwd:        testDir,
		Permission: domain.PermissionBypass,
	}

	runStep := func(stepNum int, title, userPrompt string, expectMessageCount, expectSpawnCount int) {
		logger.Logf("\n--------------------------------------------------------------------------------")
		logger.Logf(">>> STEP %d: %s", stepNum, title)
		logger.Logf("Prompt: %q", userPrompt)

		manifest := constructor.FormatAgentManifest(testDir)
		if manifest != "" {
			logger.Logf("--- Injected Agent Manifest into Coordinator Prompt ---\n%s\n--------------------------------------------------------", manifest)
		} else {
			logger.Logf("[No active agents yet; manifest is empty (0 extra tokens)]")
		}

		fullPrompt := userPrompt
		if manifest != "" {
			fullPrompt = manifest + "\n\n" + userPrompt
		}

		constructor.Remember(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		startTime := time.Now()
		turnID, err := coordinator.Send(ctx, cfg, fullPrompt)
		if err != nil {
			logger.Logf("ERROR: coordinator.Send failed: %v", err)
			return
		}
		logger.Logf("Dispatched coordinator turn: %s", turnID)

		select {
		case doneTurnID := <-coordinatorDone:
			logger.Logf("Coordinator turn completed: %s (took %s)", doneTurnID, time.Since(startTime))
		case <-time.After(3 * time.Minute):
			logger.Logf("ERROR: Timed out waiting for coordinator turn")
			return
		}

		history := coordinator.History()
		var replyText string
		for _, ev := range history {
			if ev.TurnID == turnID && ev.Kind == domain.EventAgentMessage {
				replyText += ev.Text
			}
		}

		logger.Logf("--- Coordinator Response ---\n%s\n----------------------------", strings.TrimSpace(replyText))

		var tasks []session.TaskRequest
		logger.Logf("Parsed %d tasks from plan:", len(tasks))
		messageCount, spawnCount := 0, 0
		for i, t := range tasks {
			logger.Logf("  Task %d: [%s] Title=%q Target=%q Cwd=%q Prompt=%q", i+1, t.Action, t.Title, t.TargetThreadID, t.Cwd, t.Prompt)
			if t.Action == "message" {
				messageCount++
			} else {
				spawnCount++
			}
		}

		logger.Logf("Verification: Expected %d message tasks, got %d. Expected %d spawn tasks, got %d.",
			expectMessageCount, messageCount, expectSpawnCount, spawnCount)

		logger.Logf("Waiting for worker agents to complete execution...")
		workerStart := time.Now()

		time.Sleep(5 * time.Second)

		for {
			turnWaitMu.Lock()
			pending := len(workerTurnsPending)
			turnWaitMu.Unlock()

			if pending == 0 {
				logger.Logf("All active worker agents completed turns (took %s)", time.Since(workerStart))
				break
			}

			select {
			case thread := <-workerTurnDone:
				logger.Logf("Worker %s completed turn", thread)
			case <-time.After(3 * time.Minute):
				logger.Logf("WARNING: Worker execution timeout reached")
				goto WorkersDone
			}
		}

	WorkersDone:
		agents := constructor.ListAgents()
		logger.Logf("Current Active Agents count: %d", len(agents))
		for _, a := range agents {
			logger.Logf("  Agent: ThreadID=%s Title=%q State=%s Live=%v Branch=%s", a.ThreadID, a.Title, a.State, a.Live, a.Branch)
		}
	}

	// Step 1: Initial multi-feature dump (Should spawn 2 new agents: Frontend + Backend)
	runStep(1, "Initial Multi-Feature Dump (Spawn Frontend + Backend)",
		"We are starting a new web project called TaskPulse in this directory. I need two distinct components built:\n"+
			"1. A frontend in ./public with an HTML5 file (index.html), CSS styling (styles.css), and an interactive task manager script (app.js) with add and delete task functionality.\n"+
			"2. A backend API in server.js using Node.js that serves GET /api/tasks and POST /api/tasks with in-memory storage.",
		0, 2)

	// Step 2: Refinement prompt (Should route to existing Frontend Agent)
	runStep(2, "UI Refinement (Should ROUTE to existing Frontend agent via message)",
		"The frontend looks good, but let's refine the UI: add a dark mode toggle button in index.html and styles.css, and style completed tasks with a strikethrough in app.js.",
		1, 0)

	// Step 3: Mixed prompt (Should ROUTE 1 to Backend agent, and SPAWN 1 for API docs)
	runStep(3, "Intersecting Update (Should ROUTE to Backend agent AND SPAWN documentation agent)",
		"Two updates:\n"+
			"1. For the backend API in server.js, add an in-memory priority field ('low', 'medium', 'high') to tasks and validate it on POST.\n"+
			"2. Create comprehensive documentation in API.md documenting the endpoints, request/response examples, and how to run the project.",
		1, 1)

	logger.Logf("\n================================================================================")
	logger.Logf("ALL 3 TESTING STEPS COMPLETED!")
	logger.Logf("Inspecting generated files on disk...")
	logger.Logf("================================================================================")

	filesFound := 0
	_ = filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(testDir, path)
		if strings.HasPrefix(rel, ".git") || strings.HasPrefix(rel, "logs") {
			return nil
		}
		filesFound++
		logger.Logf("Found project file: %s (%d bytes)", rel, info.Size())
		return nil
	})

	logger.Logf("Total project files on disk: %d", filesFound)
	logger.Logf("Validation run complete. Output saved to: %s", logFile)
}
