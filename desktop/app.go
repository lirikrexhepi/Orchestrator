package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"composer/internal/activity"
	"composer/internal/attachments"
	"composer/internal/claude"
	"composer/internal/devserver"
	"composer/internal/domain"
	"composer/internal/drivers"
	"composer/internal/history"
	"composer/internal/logger"
	"composer/internal/memory"
	"composer/internal/opencode"
	"composer/internal/projects"
	"composer/internal/provider"
	"composer/internal/remote"
	"composer/internal/servers"
	"composer/internal/service"
	"composer/internal/session"
	"composer/internal/storage/sqlite"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// runtimeEventsChannel carries batches of runtime events. Emitting one IPC
// message per streamed token made the webview re-render per token; batches
// are flushed every eventFlushInterval with consecutive deltas pre-merged.
const runtimeEventsChannel = "agent:events"

const eventFlushInterval = 16 * time.Millisecond

const eventBatchLimit = 256

const serversChangedChannel = "servers:changed"

const quotaChangedChannel = "usage:quota"

const orchestratorSpawnedChannel = "orchestrator:spawned"

const historyChangedChannel = "history:changed"

// projectsChangedChannel tells the window its saved project list changed
// somewhere else, such as a folder added from the phone.
const projectsChangedChannel = "projects:changed"

// cliUpdateChannel tells the window a newer Claude Code release is published,
// so it can offer the update. Nothing installs from this event alone.
const cliUpdateChannel = "cli:update-available"

type App struct {
	ctx           context.Context
	registry      *provider.Registry
	manager       *session.Manager
	coordinator   *session.Coordinator
	workspaces    *session.Workspaces
	spawner       *session.Spawner
	orchestrator  *session.Constructor
	usage         *session.UsageTracker
	quota         *claude.QuotaSource
	opencodeQuota *opencode.GoQuotaSource
	accountQuota  *accountQuotas
	scanner       *servers.Scanner
	devservers    *devserver.Manager
	control       *devserver.Control
	historyStore  *history.Store
	recorder      *history.Recorder
	projects      *projects.Store
	attachments   *attachments.Store
	memory        *memory.Store
	remoteServer  *remote.Server
	stopFeed      func()

	// headless is set when the app runs without a window (see headless.go).
	// Window-bound calls check it, since Wails' runtime exits the process when
	// handed a context it did not create.
	headless bool
	// requestStop ends a headless run; nil in the desktop app.
	requestStop func(reason string)

	db             *sqlite.DB
	sessionService *service.SessionService
	taskService    *service.TaskService
	prefService    *service.PreferenceService
	activityRepo   *sqlite.ActivityRepo
	prefRepo       *sqlite.PreferenceRepo
	activity       *activity.Tracker
	titles         chatTitles
}

func NewApp() *App {
	registry := provider.NewRegistry(drivers.All()...)
	// Applied before anything probes, so a preferred model saved in an earlier
	// run is already in effect for the first CLI detection.
	registry.UsePrefs(provider.NewPrefs(configRoot()))
	registry.UseAccounts(provider.NewAccounts(configRoot()))
	manager := session.NewManager(registry)
	workspaces := session.NewWorkspaces()
	coordinator := session.NewCoordinator(manager)
	spawner := session.NewSpawner(manager, workspaces)

	store := history.New(historyRoot())
	recorder := history.NewRecorder(store)

	devservers := devserver.NewManager()

	// Persistence is attached rather than built in, so the session layer stays
	// testable without a filesystem.
	manager.SetRecorder(recorder)
	coordinator.SetSink(recorder)
	spawner.SetTracker(history.NewTracker(recorder, coordinator))
	constructor := session.NewConstructor(manager, coordinator, spawner, workspaces)
	constructor.SetResumeResolver(recorder.ResumeID)

	var db *sqlite.DB
	var sessionService *service.SessionService
	var taskService *service.TaskService
	var prefService *service.PreferenceService
	var activityRepo *sqlite.ActivityRepo
	var prefRepo *sqlite.PreferenceRepo

	if d, err := sqlite.Open(filepath.Join(configRoot(), "composer.db")); err == nil {
		db = d
		sRepo := sqlite.NewSessionRepo(d)
		tRepo := sqlite.NewTaskRepo(d)
		provRepo := sqlite.NewProviderRepo(d)
		prefRepo = sqlite.NewPreferenceRepo(d)
		activityRepo = sqlite.NewActivityRepo(d)

		sessionService = service.NewSessionService(sRepo)
		taskService = service.NewTaskService(tRepo)
		prefService = service.NewPreferenceService(prefRepo, provRepo)
	} else {
		fmt.Printf("Warning: failed to initialize SQLite database: %v\n", err)
	}

	control := devserver.NewControl(devservers)
	if taskService != nil {
		control.SetTaskService(taskService)
	}

	if sessionService != nil {
		recorder.OnProviderSession(func(threadID, providerSessionID string) {
			_ = sessionService.UpdateCLISession(context.Background(), threadID, providerSessionID)
		})
	}

	projectsStore := projects.New(configRoot())
	manager.SetAccountResolver(func(kind domain.DriverKind, cwd string) string {
		return projectsStore.AccountFor(cwd, string(kind))
	})
	remoteServer := remote.NewServer(4545, manager, coordinator, constructor, spawner, projectsStore, recorder, store)
	remoteServer.SetStoragePath(filepath.Join(configRoot(), "remote_auth.json"))

	app := &App{
		registry:       registry,
		manager:        manager,
		coordinator:    coordinator,
		workspaces:     workspaces,
		spawner:        spawner,
		orchestrator:   constructor,
		usage:          session.NewUsageTracker(),
		quota:          claude.NewQuotaSource(""),
		opencodeQuota:  opencode.NewGoQuotaSource(),
		accountQuota:   &accountQuotas{sources: make(map[string]*claude.QuotaSource)},
		scanner:        servers.NewScanner(),
		devservers:     devservers,
		control:        control,
		historyStore:   store,
		recorder:       recorder,
		projects:       projectsStore,
		attachments:    attachments.New(filepath.Join(configRoot(), "attachments")),
		memory:         memory.New(filepath.Join(configRoot(), "memory")),
		remoteServer:   remoteServer,
		db:             db,
		sessionService: sessionService,
		taskService:    taskService,
		prefService:    prefService,
		activityRepo:   activityRepo,
		prefRepo:       prefRepo,
	}
	if activityRepo != nil {
		app.activity = activity.NewTracker(activityRepo, app.activityThread)
	}
	app.wireRemote()
	manager.SetRefExpander(app.expandChatRefs)
	return app
}

// configRoot is where Composer keeps everything it remembers between runs,
// falling back to the working directory when the user config dir is unavailable.
func configRoot() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "composer")
	}
	return ".composer"
}

// historyRoot resolves where sessions are stored.
func historyRoot() string {
	return filepath.Join(configRoot(), "history")
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.WindowShow(ctx)
	a.startCore(ctx)
}

// startCore brings up everything that does not need a window: stored history,
// the phone gateway, and the event feed. The desktop and headless modes share it.
func (a *App) startCore(ctx context.Context) {
	a.enableManagedServers()
	a.rehydrateFromHistory()
	a.startActivity()
	go a.watchClaudeUpdates(ctx)
	if a.remoteServer != nil {
		a.wireRemote()
		_ = a.remoteServer.Start(ctx)
	}
	a.orchestrator.SetOnSpawn(func(result session.SpawnResult) {
		if a.sessionService != nil {
			for _, task := range result.Tasks {
				branch := ""
				if task.Worktree != nil {
					branch = task.Worktree.Branch
				}
				_ = a.sessionService.CreateSession(a.ctx, task.ThreadID, task.Title, task.Dir(result.Workspace.Cwd), string(task.Driver), task.Model, branch)
			}
		}
		a.emit(orchestratorSpawnedChannel, result)
	})

	a.devservers.OnChange(func() {
		a.emit(serversChangedChannel)
	})

	a.quota.OnUpdate(func() {
		a.emit(quotaChangedChannel)
	})
	a.opencodeQuota.OnUpdate(func() {
		a.emit(quotaChangedChannel)
	})

	if a.recorder != nil {
		a.recorder.OnChanged(func() {
			a.emit(historyChangedChannel)
		})
	}

	events, cancel := a.manager.Bus().Subscribe()
	a.stopFeed = cancel

	go a.pumpEvents(ctx, events)
}

// pumpEvents feeds every runtime event to the backend observers immediately
// and forwards them to the webview in batches.
func (a *App) pumpEvents(ctx context.Context, events <-chan domain.RuntimeEvent) {
	ticker := time.NewTicker(eventFlushInterval)
	defer ticker.Stop()
	batch := make([]domain.RuntimeEvent, 0, eventBatchLimit)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		a.emit(runtimeEventsChannel, batch)
		batch = make([]domain.RuntimeEvent, 0, eventBatchLimit)
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				flush()
				return
			}
			a.observe(event)
			if a.headless {
				// No window to batch for; the phone gateway has its own feed.
				continue
			}
			// Consecutive deltas of one streamed item become a single event.
			if n := len(batch); n > 0 && event.Delta && sameStreamItem(batch[n-1], event) {
				batch[n-1].Text += event.Text
			} else {
				batch = append(batch, event)
			}
			if len(batch) >= eventBatchLimit {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func sameStreamItem(prev, next domain.RuntimeEvent) bool {
	return prev.Delta && prev.Kind == next.Kind && prev.ThreadID == next.ThreadID &&
		prev.TurnID == next.TurnID && prev.ItemID == next.ItemID &&
		(next.Kind == domain.EventAgentMessage || next.Kind == domain.EventAgentThought)
}

func (a *App) observe(event domain.RuntimeEvent) {
	if event.Kind == domain.EventAgentMessage || event.Kind == domain.EventAgentThought {
		logger.Debugf("EventBus", "kind=%s thread=%s delta=%v len=%d", event.Kind, event.ThreadID, event.Delta, len(event.Text))
	} else if event.Kind == domain.EventToolCall || event.Kind == domain.EventToolResult {
		name := ""
		if event.Tool != nil {
			name = event.Tool.Name
		}
		logger.Infof("EventBus", "kind=%s thread=%s tool=%s", event.Kind, event.ThreadID, name)
	} else {
		logger.Infof("EventBus", "kind=%s thread=%s turn=%s err=%s", event.Kind, event.ThreadID, event.TurnID, event.Error)
	}

	a.usage.Observe(event)
	a.activity.Observe(event)
	// Held until a spawn claims it, so the discussion that produced a
	// plan is stored with the agents that plan created.
	a.coordinator.Observe(event)
	// The Orchestrator constructor: executes coordinator plans itself
	// rather than returning JSON for the frontend to format.
	a.orchestrator.Observe(event)
	a.trackTaskState(event)
	if event.Kind == domain.EventUserMessage {
		a.noteChatMessage(event)
	}
}

func (a *App) domReady(ctx context.Context) {
	runtime.WindowShow(ctx)
}

// trackTaskState keeps a task's stored state in step with its turns, so a
// reopened session shows what finished rather than everything stuck at running.
func (a *App) trackTaskState(event domain.RuntimeEvent) {
	var state domain.TaskState
	switch event.Kind {
	case domain.EventTurnCompleted:
		state = domain.TaskComplete
	case domain.EventTurnFailed:
		state = domain.TaskFailed
	default:
		return
	}

	a.workspaces.SetState(event.ThreadID, state)
	a.recorder.UpdateTaskState(event.ThreadID, state, "")
	if workspaceID, ok := a.recorder.WorkspaceOf(event.ThreadID); ok {
		a.recorder.Touch(workspaceID, event.At)
	}
	if a.sessionService != nil {
		status := "completed"
		if state == domain.TaskFailed {
			status = "failed"
		}
		_ = a.sessionService.UpdateStatus(a.ctx, event.ThreadID, status)
	}
}

// shutdown stops every agent CLI so none outlive the window.
func (a *App) shutdown(ctx context.Context) {
	if !bindingsBuild && !a.headless && a.keepRunningOnClose() && a.ownsGateway() {
		a.handOffToBackground()
	}
	if a.remoteServer != nil {
		a.remoteServer.Stop()
	}
	if a.stopFeed != nil {
		a.stopFeed()
	}
	a.manager.StopAll(ctx)
	a.devservers.StopAll()
	a.control.Close()
	// Last write wins the race with process exit: buffered transcript tails are
	// only durable once this returns.
	if a.recorder != nil {
		_ = a.recorder.Close()
	}
	a.closeTitles()
	a.attachments.Cleanup()
	_ = a.activity.Close()
	if a.db != nil {
		_ = a.db.Close()
	}
}

// resolveCwd returns an explicit directory, or the active project when a caller
// sends no directory. It deliberately does not fall back to the app's launch
// directory: a desktop app is commonly started from a user home directory,
// which is never an implicit project choice.
//
// Defaulting here rather than in the frontend means every path into the session
// layer — orchestrator, spawn, and anything added later — lands in the chosen
// project without each one having to remember to ask for it.
func (a *App) resolveCwd(requested string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return filepath.Clean(requested)
	}
	if a.projects != nil {
		if act := a.projects.ActivePath(); act != "" {
			return act
		}
	}
	return ""
}

// requireCwd turns a missing or stale project selection into a useful UI error
// before a provider can silently inherit Composer's process directory.
func (a *App) requireCwd(requested string) (string, error) {
	cwd := a.resolveCwd(requested)
	if cwd == "" {
		return "", fmt.Errorf("choose a project folder before starting an agent")
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("working directory is unavailable: %s", cwd)
	}
	return cwd, nil
}
