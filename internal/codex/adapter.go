package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/jsonrpc"
	"composer/internal/process"
	"composer/internal/provider"
	"composer/internal/shell"
)

// Adapter drives one `codex app-server` process. The app-server multiplexes
// every thread over a single stdio connection, so the process is started lazily
// on first use and shared by all sessions this adapter owns.
type Adapter struct {
	settings domain.ProviderSettings
	emit     provider.Emitter

	mu       sync.RWMutex
	proc     *process.Process
	conn     *jsonrpc.Conn
	cancel   context.CancelFunc
	starting sync.Mutex

	threads    map[string]*thread
	byCodex    map[string]*thread
	codexTurns map[string]string
	pending    map[string]chan string
}

type thread struct {
	threadID string
	codexID  string
	cwd      string
	model    string
	effort   string

	mu     sync.Mutex
	turnID string
}

func NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) *Adapter {
	return &Adapter{
		settings: settings,
		emit:     emit,
		threads:  make(map[string]*thread),
		byCodex:  make(map[string]*thread),
		pending:  make(map[string]chan string),
	}
}

func (a *Adapter) Driver() domain.DriverKind { return domain.DriverCodex }

func (a *Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{SessionModelSwitch: true, Resume: true, Approvals: true, Plans: true}
}

func binary(settings domain.ProviderSettings) string {
	if settings.BinaryPath != "" {
		return settings.BinaryPath
	}
	return "codex"
}

func (a *Adapter) connection(ctx context.Context) (*jsonrpc.Conn, error) {
	a.mu.RLock()
	conn := a.conn
	a.mu.RUnlock()
	if conn != nil {
		select {
		case <-conn.Done():
		default:
			return conn, nil
		}
	}

	a.starting.Lock()
	defer a.starting.Unlock()

	a.mu.RLock()
	conn = a.conn
	a.mu.RUnlock()
	if conn != nil {
		select {
		case <-conn.Done():
		default:
			return conn, nil
		}
	}

	args := append([]string{"app-server"}, shell.TokenizeArgs(a.settings.LaunchArgs)...)
	env := provider.LaunchEnv(a.settings)

	procCtx, cancel := context.WithCancel(context.Background())
	proc, err := process.Start(procCtx, process.Spec{
		Command: binary(a.settings),
		Args:    args,
		Env:     env,

		DieWithParent: true,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	newConn := jsonrpc.NewConn(proc.Stdout(), proc.Stdin(), a.handleInbound)
	initParams := InitializeParams{ClientInfo: Implementation{Name: "composer", Title: "Composer"}}
	if err := newConn.Call(ctx, "initialize", initParams, nil); err != nil {
		cancel()
		_ = proc.Shutdown(time.Second)
		return nil, fmt.Errorf("codex initialize: %w (%s)", err, proc.StderrTail())
	}
	_ = newConn.Notify("initialized", struct{}{})

	a.mu.Lock()
	a.proc, a.conn, a.cancel = proc, newConn, cancel
	a.mu.Unlock()

	go a.watchExit(proc, newConn)
	return newConn, nil
}

func (a *Adapter) watchExit(proc *process.Process, conn *jsonrpc.Conn) {
	<-conn.Done()

	a.mu.Lock()
	threads := make([]*thread, 0, len(a.threads))
	for _, t := range a.threads {
		threads = append(threads, t)
	}
	a.threads = make(map[string]*thread)
	a.byCodex = make(map[string]*thread)
	if a.conn == conn {
		a.conn, a.proc = nil, nil
	}
	a.mu.Unlock()

	detail := proc.StderrTail()
	for _, t := range threads {
		t.mu.Lock()
		turnID := t.turnID
		t.turnID = ""
		t.mu.Unlock()
		if turnID != "" {
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
				Driver: domain.DriverCodex, Error: "codex app-server exited: " + detail,
			})
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventSessionStopped, ThreadID: t.threadID, Driver: domain.DriverCodex,
		})
	}
}

func (a *Adapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	conn, err := a.connection(ctx)
	if err != nil {
		return domain.Session{}, err
	}

	cwd := in.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	in.Cwd = cwd

	model := in.Model
	if model == "" {
		model = a.settings.Model
	}
	effort := ResolveEffort(in.Options, "")
	approval := approvalPolicy(in.Permission)

	t := &thread{threadID: in.ThreadID, cwd: in.Cwd, model: model, effort: effort}

	if in.Resume != "" {
		params := ThreadResumeParams{
			ThreadID: in.Resume, Cwd: in.Cwd, Model: model, Effort: effort, ApprovalPolicy: approval,
			DeveloperInstructions: provider.RuntimeInstructions,
		}
		var resumed ThreadStartResponse
		if err := conn.Call(ctx, "thread/resume", params, &resumed); err == nil {
			t.codexID = firstNonEmpty(resumed.Thread.ID, in.Resume)
		}
	}

	if t.codexID == "" {
		params := ThreadStartParams{Cwd: in.Cwd, Model: model, Effort: effort, ApprovalPolicy: approval, DeveloperInstructions: provider.RuntimeInstructions}
		var started ThreadStartResponse
		if err := conn.Call(ctx, "thread/start", params, &started); err != nil {
			return domain.Session{}, fmt.Errorf("codex thread/start: %w", err)
		}
		t.codexID = started.Thread.ID
	}

	a.mu.Lock()
	a.threads[in.ThreadID] = t
	a.byCodex[t.codexID] = t
	a.mu.Unlock()

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventSessionStarted, ThreadID: in.ThreadID, Driver: domain.DriverCodex,
	})

	return domain.Session{
		ThreadID:          in.ThreadID,
		InstanceID:        in.InstanceID,
		Driver:            domain.DriverCodex,
		ProviderSessionID: t.codexID,
		Cwd:               in.Cwd,
		Model:             model,
		StartedAt:         time.Now().UnixMilli(),
	}, nil
}

func approvalPolicy(mode domain.PermissionMode) string {
	switch mode {
	case domain.PermissionBypass:
		return "never"
	case domain.PermissionAcceptEdits:
		return "onFailure"
	case domain.PermissionPlan:
		return "always"
	default:
		return "onRequest"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *Adapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error {
	a.mu.RLock()
	t := a.threads[in.ThreadID]
	conn := a.conn
	a.mu.RUnlock()
	if t == nil || conn == nil {
		return errors.New("no active session for thread " + in.ThreadID)
	}

	input := make([]UserInput, 0, len(in.Files)+1)
	if in.Text != "" {
		input = append(input, UserInput{Type: "text", Text: in.Text})
	}
	for _, file := range in.Files {
		input = append(input, UserInput{Type: "localImage", Path: file.Path})
	}

	t.mu.Lock()
	t.turnID = in.TurnID
	t.mu.Unlock()

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventTurnStarted, ThreadID: in.ThreadID, TurnID: in.TurnID, Driver: domain.DriverCodex,
	})

	params := TurnStartParams{ThreadID: t.codexID, Input: input, Model: t.model, Effort: t.effort, Cwd: t.cwd}
	var started TurnStartResponse
	if err := conn.Call(ctx, "turn/start", params, &started); err != nil {
		t.mu.Lock()
		t.turnID = ""
		t.mu.Unlock()
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventTurnFailed, ThreadID: in.ThreadID, TurnID: in.TurnID,
			Driver: domain.DriverCodex, Error: err.Error(),
		})
		return err
	}

	// The app-server assigns its own turn id; remember it so interrupts map
	// back to the caller's turn.
	if started.Turn.ID != "" {
		a.mu.Lock()
		if a.codexTurns == nil {
			a.codexTurns = make(map[string]string)
		}
		a.codexTurns[t.threadID] = started.Turn.ID
		a.mu.Unlock()
	}
	return nil
}

func (a *Adapter) InterruptTurn(ctx context.Context, threadID string) error {
	a.mu.RLock()
	t := a.threads[threadID]
	conn := a.conn
	codexTurn := a.codexTurns[threadID]
	a.mu.RUnlock()
	if t == nil || conn == nil || codexTurn == "" {
		return nil
	}
	return conn.Call(ctx, "turn/interrupt", TurnInterruptParams{ThreadID: t.codexID, TurnID: codexTurn}, nil)
}

func (a *Adapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	a.mu.Lock()
	reply, ok := a.pending[requestID]
	delete(a.pending, requestID)
	a.mu.Unlock()
	if !ok || reply == nil {
		return errors.New("unknown approval request " + requestID)
	}

	reply <- mapDecision(decision)
	return nil
}

func mapDecision(decision domain.ApprovalDecision) string {
	switch decision {
	case domain.ApprovalAllowAlways:
		return DecisionApprovedForSession
	case domain.ApprovalDeny:
		return DecisionDenied
	case domain.ApprovalCancel:
		return DecisionAbort
	default:
		return DecisionApproved
	}
}

func (a *Adapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	return nil
}

func (a *Adapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	t := a.threads[threadID]
	delete(a.threads, threadID)
	if t != nil {
		delete(a.byCodex, t.codexID)
		delete(a.codexTurns, threadID)
	}
	conn := a.conn
	a.mu.Unlock()
	if t == nil || conn == nil {
		return nil
	}
	_ = conn.Call(ctx, "thread/unsubscribe", map[string]string{"threadId": t.codexID}, nil)
	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventSessionStopped, ThreadID: threadID, Driver: domain.DriverCodex,
	})
	return nil
}

func (a *Adapter) StopAll(ctx context.Context) error {
	a.mu.Lock()
	proc, cancel := a.proc, a.cancel
	a.threads = make(map[string]*thread)
	a.byCodex = make(map[string]*thread)
	a.codexTurns = nil
	a.proc, a.conn, a.cancel = nil, nil, nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if proc != nil {
		return proc.Shutdown(2 * time.Second)
	}
	return nil
}

func (a *Adapter) HasSession(threadID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.threads[threadID]
	return ok
}

func (a *Adapter) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	a.mu.RLock()
	t, ok := a.threads[threadID]
	a.mu.RUnlock()
	if !ok || model == "" {
		return ok
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.model = model
	if effort := options.String(domain.OptionEffort); effort != "" {
		t.effort = effort
	}
	return true
}

// Session reports the thread's current session, including the codex thread id
// used to resume it in a later run.
func (a *Adapter) Session(threadID string) (domain.Session, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	t, ok := a.threads[threadID]
	if !ok {
		return domain.Session{}, false
	}
	return domain.Session{
		ThreadID:          threadID,
		Driver:            domain.DriverCodex,
		ProviderSessionID: t.codexID,
		Cwd:               t.cwd,
		Model:             t.model,
	}, true
}

func (a *Adapter) lookupByCodexID(codexThreadID string) *thread {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.byCodex[codexThreadID]
}

func decode[T any](params json.RawMessage) (T, bool) {
	var value T
	if err := json.Unmarshal(params, &value); err != nil {
		return value, false
	}
	return value, true
}
