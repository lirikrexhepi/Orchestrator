package session

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"composer/internal/domain"
)

// CoordinatorThreadID is the live orchestrator conversation.
//
// History binds a *copy* of this conversation to each workspace instead of
// giving every workspace its own live thread: the input bar is one continuous
// chat, and rebinding it per spawn would reset the user's context every time
// they delegated. See Coordinator.BindWorkspace.
const CoordinatorThreadID = "coordinator"

// CoordinatorThreadFor names the orchestrator transcript stored against a
// workspace. Reopening a session replays this alongside its agents, which is
// what makes a Composer session more than a single agent's chat log.
func CoordinatorThreadFor(workspaceID string) string {
	if workspaceID == "" {
		return CoordinatorThreadID
	}
	return CoordinatorThreadID + "-" + workspaceID
}

// Coordinator owns the single top-level conversation driven by the main input
// bar. Switching provider, model, or options restarts the underlying CLI
// session transparently, so the caller only ever sends messages.
type Coordinator struct {
	manager *Manager

	mu         sync.Mutex
	driver     domain.DriverKind
	account    string
	model      string
	options    domain.ModelOptions
	cwd        string
	permission domain.PermissionMode
	started    bool
	pendingHandoff string
	// pending holds the turns of the current conversation that have not yet been
	// attributed to a workspace. A spawn claims them, which is how the plan that
	// produced a set of agents ends up stored beside those agents.
	pending []domain.RuntimeEvent

	turnSeq atomic.Uint64
	sink    CoordinatorSink
}

// CoordinatorSink receives the orchestrator transcript once a spawn gives it a
// workspace to belong to.
type CoordinatorSink interface {
	RecordCoordinator(workspaceID, threadID string, events []domain.RuntimeEvent)
}

func NewCoordinator(manager *Manager) *Coordinator {
	return &Coordinator{manager: manager}
}

// SetSink attaches durable storage for orchestrator transcripts.
func (c *Coordinator) SetSink(sink CoordinatorSink) {
	c.mu.Lock()
	c.sink = sink
	c.mu.Unlock()
}

// BindWorkspace hands the conversation so far to a newly created workspace and
// starts a fresh one.
//
// Called at spawn time: everything the user and orchestrator said to reach this
// plan is stored under the workspace the plan created, then cleared so the next
// delegation records its own discussion rather than repeating this one.
func (c *Coordinator) BindWorkspace(workspaceID string) string {
	threadID := CoordinatorThreadFor(workspaceID)

	c.mu.Lock()
	sink := c.sink
	events := c.pending
	c.pending = nil
	c.mu.Unlock()

	if sink == nil {
		return threadID
	}

	// Re-stamped onto the workspace's own thread id so a replay groups them
	// with that session rather than with the live coordinator.
	stored := make([]domain.RuntimeEvent, 0, len(events))
	for _, event := range events {
		event.ThreadID = threadID
		stored = append(stored, event)
	}
	sink.RecordCoordinator(workspaceID, threadID, stored)
	return threadID
}

// Config is the frontend-facing selection for the coordinator thread.
type Config struct {
	Driver     string                `json:"driver"`
	Account    string                `json:"account,omitempty"`
	Model      string                `json:"model,omitempty"`
	Options    domain.ModelOptions   `json:"options,omitempty"`
	Cwd        string                `json:"cwd,omitempty"`
	Permission domain.PermissionMode `json:"permissionMode,omitempty"`
}

// Send ensures a session matching cfg is live, then delivers the message. The
// returned turn id lets the caller correlate streamed events.
func (c *Coordinator) Send(ctx context.Context, cfg Config, text string) (string, error) {
	return c.SendWithFiles(ctx, cfg, text, nil)
}

// SendWithFiles is Send with attachments. A turn carrying files may have no
// text of its own — an image pasted on its own is a complete message — so the
// empty check passes when either is present.
func (c *Coordinator) SendWithFiles(
	ctx context.Context,
	cfg Config,
	text string,
	files []domain.FileRef,
) (string, error) {
	return c.SendWithContext(ctx, cfg, text, "", files)
}

// SendWithContext sends text with a context preamble (project memory, agent
// manifest) that the model sees but the transcript does not: the user's
// bubble shows exactly what they typed, which is also what the UI's
// optimistic bubble is matched against.
func (c *Coordinator) SendWithContext(
	ctx context.Context,
	cfg Config,
	text string,
	preamble string,
	files []domain.FileRef,
) (string, error) {
	if text == "" && len(files) == 0 {
		return "", fmt.Errorf("message is empty")
	}
	if err := c.ensureSession(ctx, cfg); err != nil {
		return "", err
	}

	turnID := "turn-" + strconv.FormatUint(c.turnSeq.Add(1), 10)

	c.mu.Lock()
	body := text
	if preamble != "" {
		body = preamble + "\n\n--- Current request ---\n\n" + text
	}
	handoff := c.pendingHandoff
	c.pendingHandoff = ""
	if handoff != "" {
		body = handoff + "\n\n--- Current request ---\n\n" + body
	}
	c.mu.Unlock()

	// Record the user's prompt as a durable event so CoordinatorHistory()
	// replays include it. Without this, user messages only exist client-side
	// and vanish on any component remount or history replay.
	c.manager.RecordUserMessage(CoordinatorThreadID, turnID, text, files...)

	if err := c.manager.Send(ctx, domain.SendTurnInput{
		ThreadID: CoordinatorThreadID,
		TurnID:   turnID,
		Text:     body,
		Files:    files,
	}); err != nil {
		return "", err
	}
	return turnID, nil
}

func (c *Coordinator) ensureSession(ctx context.Context, cfg Config) error {
	driver := domain.DriverKind(cfg.Driver)
	cwd := cfg.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	account, err := c.manager.ResolveAccount(driver, cwd, cfg.Account)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started && c.matches(driver, account, cfg, cwd) {
		return nil
	}
	if c.started {
		prior := c.manager.History(CoordinatorThreadID)
		prevDriver := c.driver
		prevModel := c.model
		_ = c.manager.Stop(ctx, CoordinatorThreadID)
		c.started = false
		if len(prior) > 0 {
			c.pendingHandoff = BuildHandoffPrompt(prevDriver, driver, prior, cwd)
		}
		c.mu.Unlock()
		if len(prior) > 0 {
			c.manager.RecordNotice(CoordinatorThreadID, fmt.Sprintf("Switched from %s: %s to %s: %s", domain.DriverLabel(prevDriver), prevModel, domain.DriverLabel(driver), cfg.Model), "")
		}
		c.mu.Lock()
	}

	if _, err := c.manager.Start(ctx, driver, domain.SessionStartInput{
		ThreadID:   CoordinatorThreadID,
		Account:    account,
		Cwd:        cwd,
		Model:      cfg.Model,
		Options:    cfg.Options,
		Permission: cfg.Permission,
		PlanOnly:   false,
	}); err != nil {
		return err
	}

	c.driver, c.account, c.model, c.options, c.permission, c.cwd, c.started = driver, account, cfg.Model, cfg.Options, cfg.Permission, cwd, true
	return nil
}

// CurrentConfig reports the selection the live coordinator session runs with,
// so other surfaces (the phone) reuse it instead of forcing a restart.
func (c *Coordinator) CurrentConfig() (Config, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started {
		return Config{}, false
	}
	return Config{
		Driver: string(c.driver), Account: c.account, Model: c.model, Options: c.options,
		Cwd: c.cwd, Permission: c.permission,
	}, true
}

func (c *Coordinator) matches(driver domain.DriverKind, account string, cfg Config, cwd string) bool {
	return c.driver == driver && c.account == account && c.model == cfg.Model && c.cwd == cwd &&
		c.permission == cfg.Permission &&
		sameOptions(c.options, cfg.Options)
}

func sameOptions(a, b domain.ModelOptions) bool {
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

func (c *Coordinator) Interrupt(ctx context.Context) error {
	return c.manager.Interrupt(ctx, CoordinatorThreadID)
}

func (c *Coordinator) Reset(ctx context.Context) error {
	c.mu.Lock()
	c.started = false
	// The unattributed transcript belongs to the conversation being discarded;
	// carrying it forward would file it under whatever the next request spawns.
	c.pending = nil
	c.mu.Unlock()
	return c.manager.Stop(ctx, CoordinatorThreadID)
}

func (c *Coordinator) History() []domain.RuntimeEvent {
	return c.manager.History(CoordinatorThreadID)
}

// Observe collects an orchestrator event for the workspace a later spawn will
// create. Cheap and lock-scoped: it runs for every streamed token.
func (c *Coordinator) Observe(event domain.RuntimeEvent) {
	if event.ThreadID != CoordinatorThreadID {
		return
	}

	c.mu.Lock()
	// A conversation that never delegates would otherwise grow without bound.
	// The cap is generous; only the tail matters for reconstructing a plan.
	if len(c.pending) < coordinatorPendingLimit {
		c.pending = append(c.pending, event)
	}
	c.mu.Unlock()
}

// coordinatorPendingLimit bounds the unattributed transcript held in memory
// between spawns.
const coordinatorPendingLimit = 4000
