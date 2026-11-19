package antigravity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"composer/internal/domain"
	"composer/internal/jsonrpc"
	"composer/internal/logger"
	"composer/internal/process"
	"composer/internal/provider"
	"composer/internal/shell"
)

type Adapter struct {
	settings domain.ProviderSettings
	emit     provider.Emitter

	mu       sync.RWMutex
	sessions map[string]*session
}

func NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) *Adapter {
	return &Adapter{
		settings: settings,
		emit:     emit,
		sessions: make(map[string]*session),
	}
}

func (a *Adapter) Driver() domain.DriverKind { return domain.DriverAntigravity }

func (a *Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{Resume: true, Plans: true, Approvals: true, SessionModelSwitch: true}
}

type pendingApproval struct {
	options []acpPermissionOption
	reply   chan acpPermissionOutcome
}

type session struct {
	threadID      string
	cwd           string
	model         string
	options       domain.ModelOptions
	permission    domain.PermissionMode
	skills        *domain.SkillPolicy
	sessionID     string
	contextWindow int64

	proc   *process.Process
	conn   *jsonrpc.Conn
	cancel context.CancelFunc

	mu           sync.Mutex
	turnID       string
	turnSettled  chan struct{}
	interrupting bool

	activeKind string // "", "message", "thought"
	segmentSeq int

	tools     map[string]bool
	baselines map[string]string

	reqSeq  atomic.Uint64
	pending map[string]*pendingApproval
}

func (a *Adapter) binary() string {
	if a.settings.BinaryPath != "" {
		return a.settings.BinaryPath
	}
	return "agy"
}

func permissionMode(mode domain.PermissionMode) string {
	switch mode {
	case domain.PermissionBypass:
		return "auto"
	case domain.PermissionAcceptEdits:
		return "accept-edits"
	case domain.PermissionPlan:
		return "plan"
	default:
		return "default"
	}
}

func (a *Adapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	env := provider.LaunchEnv(a.settings)
	if _, ok := shell.LookPath(a.binary(), env); !ok {
		return domain.Session{}, fmt.Errorf("%w: %s", process.ErrNotFound, a.binary())
	}

	cwd := in.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	in.Cwd = cwd

	args := []string{"acp", "--stdio"}
	if cwd != "" {
		args = append(args, "--add-dir", cwd)
	}
	if in.Permission == domain.PermissionBypass {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, shell.TokenizeArgs(a.settings.LaunchArgs)...)

	procCtx, cancel := context.WithCancel(context.Background())
	proc, err := process.Start(procCtx, process.Spec{
		Command:       a.binary(),
		Args:          args,
		Cwd:           cwd,
		Env:           env,
		DieWithParent: true,
	})
	if err != nil {
		cancel()
		return domain.Session{}, fmt.Errorf("start %s: %w", a.binary(), err)
	}

	model := firstNonEmpty(in.Model, a.settings.Model)
	s := &session{
		threadID:   in.ThreadID,
		cwd:        cwd,
		model:      model,
		options:    in.Options,
		permission: in.Permission,
		skills:     in.Skills,
		proc:       proc,
		cancel:     cancel,
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	conn := jsonrpc.NewConn(proc.Stdout(), proc.Stdin(), a.makeHandler(s))
	s.conn = conn

	if err := a.initSession(ctx, s, in); err != nil {
		conn.Close(err)
		cancel()
		_ = proc.Shutdown(2 * time.Second)
		return domain.Session{}, err
	}

	a.mu.Lock()
	a.sessions[in.ThreadID] = s
	a.mu.Unlock()

	go a.monitor(s)

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventSessionStarted,
		ThreadID: in.ThreadID,
		Driver:   domain.DriverAntigravity,
		Text:     s.sessionID,
	})

	return domain.Session{
		ThreadID:          in.ThreadID,
		InstanceID:        in.InstanceID,
		Driver:            domain.DriverAntigravity,
		ProviderSessionID: s.sessionID,
		Cwd:               cwd,
		Model:             s.model,
		StartedAt:         time.Now().UnixMilli(),
	}, nil
}

func (a *Adapter) initSession(ctx context.Context, s *session, in domain.SessionStartInput) error {
	initCtx, cancelInit := context.WithTimeout(ctx, 20*time.Second)
	defer cancelInit()
	var initRes initializeResult
	if err := s.conn.Call(initCtx, "initialize", initializeParams{
		ProtocolVersion: 1,
		ClientCapabilities: acpClientCapabilities{
			Fs: acpClientFsCaps{
				ReadTextFile:  false,
				WriteTextFile: false,
			},
			Terminal: false,
		},
		ClientInfo: acpClientInfo{
			Name:    "orchestrator",
			Version: "0.1.0",
		},
	}, &initRes); err != nil {
		return fmt.Errorf("initialize %s: %w", a.binary(), err)
	}

	var appendParts []string
	if provider.RuntimeInstructions != "" {
		appendParts = append(appendParts, provider.RuntimeInstructions)
	}
	if in.Skills != nil {
		if inst := in.Skills.Instruction(); inst != "" {
			appendParts = append(appendParts, inst)
		}
	}
	appendPrompt := strings.Join(appendParts, "\n\n")

	var sessionID string
	loaded := false
	if in.Resume != "" {
		loadCtx, cancelLoad := context.WithTimeout(ctx, 20*time.Second)
		var loadRes sessionNewResult
		err := s.conn.Call(loadCtx, "session/load", sessionLoadParams{
			SessionID:  in.Resume,
			Cwd:        s.cwd,
			MCPServers: mcpServersFor(in.MCP),
		}, &loadRes)
		cancelLoad()
		if err == nil {
			sessionID = loadRes.SessionID
			if sessionID == "" {
				sessionID = in.Resume
			}
			loaded = true
		} else {
			logger.Warnf("Antigravity", "session/load failed for %s, falling back to session/new: %v", in.Resume, err)
		}
	}

	if !loaded {
		var meta map[string]any
		if appendPrompt != "" {
			meta = map[string]any{
				"antigravity": map[string]any{
					"systemPrompt": map[string]any{
						"append": appendPrompt,
					},
				},
			}
		}
		newCtx, cancelNew := context.WithTimeout(ctx, 20*time.Second)
		var newRes sessionNewResult
		err := s.conn.Call(newCtx, "session/new", sessionNewParams{
			Cwd:        s.cwd,
			MCPServers: mcpServersFor(in.MCP),
			Meta:       meta,
		}, &newRes)
		cancelNew()
		if err != nil {
			return fmt.Errorf("session/new %s: %w", a.binary(), err)
		}
		sessionID = newRes.SessionID
		if sessionID == "" && in.Resume != "" {
			sessionID = in.Resume
		}
	}
	s.sessionID = sessionID

	cfgCtx, cancelCfg := context.WithTimeout(ctx, 10*time.Second)
	defer cancelCfg()

	if s.model != "" {
		base, effort := SplitModelSelection(s.model, in.Options)
		if base != "" {
			if err := s.conn.Call(cfgCtx, "session/set_config_option", sessionSetConfigParams{
				SessionID: s.sessionID,
				ConfigID:  "model",
				Value:     base,
			}, nil); err != nil {
				logger.Warnf("Antigravity", "set initial model %q on thread %s: %v", base, in.ThreadID, err)
			}
		}
		if effort != "" {
			if err := s.conn.Call(cfgCtx, "session/set_config_option", sessionSetConfigParams{
				SessionID: s.sessionID,
				ConfigID:  "thought_level",
				Value:     effort,
			}, nil); err != nil {
				logger.Warnf("Antigravity", "set initial thought_level %q on thread %s: %v", effort, in.ThreadID, err)
			}
		}
	}

	if mode := permissionMode(in.Permission); mode != "" {
		if err := s.conn.Call(cfgCtx, "session/set_config_option", sessionSetConfigParams{
			SessionID: s.sessionID,
			ConfigID:  "mode",
			Value:     mode,
		}, nil); err != nil {
			logger.Warnf("Antigravity", "set initial mode %q on thread %s: %v", mode, in.ThreadID, err)
		}
	}

	return nil
}

func (a *Adapter) monitor(s *session) {
	if s.proc != nil {
		select {
		case <-s.conn.Done():
		case <-s.proc.Done():
		}
	} else {
		<-s.conn.Done()
	}
	a.finish(s)
}

func (a *Adapter) finish(s *session) {
	a.mu.Lock()
	if current, ok := a.sessions[s.threadID]; ok && current == s {
		delete(a.sessions, s.threadID)
	}
	a.mu.Unlock()

	s.mu.Lock()
	turnID := s.turnID
	interrupting := s.interrupting
	s.turnID = ""
	s.interrupting = false
	pending := s.pending
	s.pending = make(map[string]*pendingApproval)
	s.mu.Unlock()

	for _, p := range pending {
		select {
		case p.reply <- acpPermissionOutcome{Outcome: "cancelled"}:
		default:
		}
	}

	if turnID != "" {
		if interrupting {
			a.emit.Emit(domain.RuntimeEvent{
				Kind:       domain.EventTurnCompleted,
				ThreadID:   s.threadID,
				TurnID:     turnID,
				Driver:     domain.DriverAntigravity,
				StopReason: domain.StopCancelled,
			})
		} else {
			var detail string
			if s.proc != nil {
				detail = s.proc.StderrTail()
			}
			a.emit.Emit(domain.RuntimeEvent{
				Kind:     domain.EventTurnFailed,
				ThreadID: s.threadID,
				TurnID:   turnID,
				Driver:   domain.DriverAntigravity,
				Error:    a.binary() + " exited: " + detail,
			})
		}
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventSessionStopped,
		ThreadID: s.threadID,
		Driver:   domain.DriverAntigravity,
	})
	s.cancel()
}

func (a *Adapter) makeHandler(s *session) jsonrpc.Handler {
	return func(ctx context.Context, method string, rawParams json.RawMessage) (any, error) {
		switch method {
		case "session/update":
			var params sessionUpdateParams
			if err := json.Unmarshal(rawParams, &params); err != nil {
				logger.Warnf("Antigravity", "unmarshal session/update: %v", err)
				return nil, nil
			}
			s.mu.Lock()
			turnID := s.turnID
			s.mu.Unlock()
			a.handleSessionUpdate(s, turnID, &params.Update)
			return nil, nil

		case "session/request_permission":
			var params requestPermissionParams
			if err := json.Unmarshal(rawParams, &params); err != nil {
				return nil, fmt.Errorf("unmarshal request_permission: %w", err)
			}
			return a.handleRequestPermission(ctx, s, params)

		default:
			return nil, nil
		}
	}
}

func (a *Adapter) handleRequestPermission(ctx context.Context, s *session, params requestPermissionParams) (any, error) {
	s.mu.Lock()
	perm := s.permission
	s.mu.Unlock()

	if perm == domain.PermissionBypass {
		var selectedID string
		for _, opt := range params.Options {
			if opt.Kind == "allow_always" {
				selectedID = opt.OptionID
				break
			}
		}
		if selectedID == "" {
			for _, opt := range params.Options {
				if opt.Kind == "allow_once" {
					selectedID = opt.OptionID
					break
				}
			}
		}
		if selectedID == "" && len(params.Options) > 0 {
			selectedID = params.Options[0].OptionID
		}
		if selectedID != "" {
			return requestPermissionResult{
				Outcome: acpPermissionOutcome{
					Outcome:  "selected",
					OptionID: selectedID,
				},
			}, nil
		}
		return requestPermissionResult{
			Outcome: acpPermissionOutcome{
				Outcome: "cancelled",
			},
		}, nil
	}

	reqID := params.ToolCall.ToolCallID
	if reqID == "" {
		reqID = fmt.Sprintf("req-%d", s.reqSeq.Add(1))
	}

	replyCh := make(chan acpPermissionOutcome, 1)
	s.mu.Lock()
	s.pending[reqID] = &pendingApproval{
		options: params.Options,
		reply:   replyCh,
	}
	turnID := s.turnID
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, reqID)
		s.mu.Unlock()
	}()

	domainOpts := make([]domain.ApprovalOption, len(params.Options))
	for i, opt := range params.Options {
		domainOpts[i] = domain.ApprovalOption{
			ID:   opt.OptionID,
			Name: opt.Name,
			Kind: mapACPOptionKind(opt.Kind),
		}
	}

	tool := &domain.ToolCall{
		ID:    params.ToolCall.ToolCallID,
		Name:  params.ToolCall.Title,
		Kind:  params.ToolCall.Kind,
		Input: params.ToolCall.RawInput,
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventApprovalRequest,
		ThreadID: s.threadID,
		TurnID:   turnID,
		Driver:   domain.DriverAntigravity,
		Approval: &domain.ApprovalRequest{
			RequestID: reqID,
			Title:     params.ToolCall.Title,
			Detail:    params.ToolCall.Kind,
			Tool:      tool,
			Options:   domainOpts,
		},
	})

	var procDone <-chan struct{}
	if s.proc != nil {
		procDone = s.proc.Done()
	}

	select {
	case outcome := <-replyCh:
		return requestPermissionResult{Outcome: outcome}, nil
	case <-procDone:
		return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, nil
	case <-s.conn.Done():
		return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, nil
	case <-ctx.Done():
		return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, ctx.Err()
	}
}

func mapACPOptionKind(kind string) domain.ApprovalDecision {
	switch kind {
	case "allow_always":
		return domain.ApprovalAllowAlways
	case "allow_once":
		return domain.ApprovalAllowOnce
	case "reject_once", "reject_always":
		return domain.ApprovalDeny
	default:
		if strings.Contains(kind, "always") {
			return domain.ApprovalAllowAlways
		}
		if strings.Contains(kind, "allow") {
			return domain.ApprovalAllowOnce
		}
		return domain.ApprovalDeny
	}
}

const maxInlineImageBytes = 5 * 1024 * 1024

func buildPromptBlocks(text string, files []domain.FileRef) []any {
	blocks := make([]any, 0, len(files)+1)
	mentions := text
	for _, file := range files {
		if img, ok := buildImageBlock(file); ok {
			blocks = append(blocks, img)
			continue
		}
		mentions += "\n@" + file.Path
	}
	if strings.TrimSpace(mentions) != "" || len(blocks) == 0 {
		blocks = append(blocks, acpTextContent{Type: "text", Text: mentions})
	}
	return blocks
}

func buildImageBlock(file domain.FileRef) (acpImagePrompt, bool) {
	mime := strings.ToLower(file.MIME)
	if mime == "" {
		switch strings.ToLower(filepath.Ext(file.Path)) {
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".gif":
			mime = "image/gif"
		case ".webp":
			mime = "image/webp"
		}
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return acpImagePrompt{}, false
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.Size() > maxInlineImageBytes {
		return acpImagePrompt{}, false
	}
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return acpImagePrompt{}, false
	}
	return acpImagePrompt{
		Type:     "image",
		MIMEType: mime,
		Data:     base64.StdEncoding.EncodeToString(data),
	}, true
}

func (a *Adapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error {
	s, ok := a.lookup(in.ThreadID)
	if !ok {
		return errors.New("no active session for thread " + in.ThreadID)
	}

	s.mu.Lock()
	if s.turnID != "" {
		s.interrupting = true
		prevSettled := s.turnSettled
		sessionID := s.sessionID
		s.mu.Unlock()

		_ = s.conn.Notify("session/cancel", sessionCancelParams{SessionID: sessionID})
		if prevSettled != nil {
			select {
			case <-prevSettled:
			case <-time.After(3 * time.Second):
			}
		}
		s.mu.Lock()
	}

	s.turnID = in.TurnID
	s.interrupting = false
	s.activeKind = ""
	s.segmentSeq = 0
	settled := make(chan struct{})
	s.turnSettled = settled
	turnID := in.TurnID
	s.mu.Unlock()

	promptBlocks := buildPromptBlocks(in.Text, in.Files)

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventTurnStarted,
		ThreadID: in.ThreadID,
		TurnID:   in.TurnID,
		Driver:   domain.DriverAntigravity,
	})

	go func() {
		defer close(settled)

		var res sessionPromptResult
		err := s.conn.Call(context.Background(), "session/prompt", sessionPromptParams{
			SessionID: s.sessionID,
			Prompt:    promptBlocks,
		}, &res)

		s.mu.Lock()
		interrupting := s.interrupting
		s.turnID = ""
		s.interrupting = false
		s.activeKind = ""
		ctxWindow := s.contextWindow
		s.mu.Unlock()

		if err != nil {
			if interrupting || errors.Is(err, context.Canceled) {
				a.emit.Emit(domain.RuntimeEvent{
					Kind:       domain.EventTurnCompleted,
					ThreadID:   s.threadID,
					TurnID:     turnID,
					Driver:     domain.DriverAntigravity,
					StopReason: domain.StopCancelled,
				})
			} else {
				a.emit.Emit(domain.RuntimeEvent{
					Kind:     domain.EventTurnFailed,
					ThreadID: s.threadID,
					TurnID:   turnID,
					Driver:   domain.DriverAntigravity,
					Error:    err.Error(),
				})
			}
			return
		}

		if res.Usage != nil {
			contextTokens := res.Usage.TotalTokens
			if contextTokens == 0 {
				contextTokens = res.Usage.InputTokens + res.Usage.OutputTokens
			}
			a.emit.Emit(domain.RuntimeEvent{
				Kind:     domain.EventUsage,
				ThreadID: s.threadID,
				TurnID:   turnID,
				Driver:   domain.DriverAntigravity,
				Usage: &domain.Usage{
					InputTokens:      res.Usage.InputTokens,
					OutputTokens:     res.Usage.OutputTokens,
					CacheReadTokens:  res.Usage.CachedReadTokens,
					CacheWriteTokens: res.Usage.CachedWriteTokens,
					ContextTokens:    contextTokens,
					ContextWindow:    ctxWindow,
				},
			})
		}

		stopReason := mapStopReason(res.StopReason)
		if interrupting {
			stopReason = domain.StopCancelled
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind:       domain.EventTurnCompleted,
			ThreadID:   s.threadID,
			TurnID:     turnID,
			Driver:     domain.DriverAntigravity,
			StopReason: stopReason,
		})
	}()

	return nil
}

func mapStopReason(reason string) domain.StopReason {
	switch reason {
	case "end_turn":
		return domain.StopEndTurn
	case "max_tokens":
		return domain.StopMaxTokens
	case "refusal":
		return domain.StopRefusal
	case "cancelled":
		return domain.StopCancelled
	default:
		return domain.StopEndTurn
	}
}

func (a *Adapter) InterruptTurn(ctx context.Context, threadID string) error {
	s, ok := a.lookup(threadID)
	if !ok {
		return nil
	}

	s.mu.Lock()
	turnID := s.turnID
	if turnID == "" {
		s.mu.Unlock()
		return nil
	}
	s.interrupting = true
	sessionID := s.sessionID
	s.mu.Unlock()

	_ = s.conn.Notify("session/cancel", sessionCancelParams{SessionID: sessionID})

	go func() {
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()

		if s.proc != nil {
			select {
			case <-s.proc.Done():
				return
			case <-timer.C:
			}
		} else {
			select {
			case <-s.conn.Done():
				return
			case <-timer.C:
			}
		}

		s.mu.Lock()
		stuck := s.turnID == turnID
		s.mu.Unlock()
		if stuck {
			logger.Infof("Antigravity", "interrupt did not settle on %s; killing process", threadID)
			s.cancel()
			if s.proc != nil {
				_ = s.proc.Shutdown(time.Second)
			}
			s.conn.Close(errors.New("interrupt timed out"))
		}
	}()

	return nil
}

func (a *Adapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	a.mu.RLock()
	s, ok := a.sessions[threadID]
	a.mu.RUnlock()
	if !ok {
		return errors.New("no active session for thread " + threadID)
	}

	s.mu.Lock()
	pending, ok := s.pending[requestID]
	s.mu.Unlock()
	if !ok {
		return errors.New("no pending approval for " + requestID)
	}

	outcome := resolveApprovalOutcome(pending.options, decision)
	select {
	case pending.reply <- outcome:
	default:
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventApprovalResolved,
		ThreadID: threadID,
		Driver:   domain.DriverAntigravity,
		Approval: &domain.ApprovalRequest{RequestID: requestID},
		Text:     string(decision),
	})
	return nil
}

func resolveApprovalOutcome(options []acpPermissionOption, decision domain.ApprovalDecision) acpPermissionOutcome {
	if decision == domain.ApprovalCancel {
		return acpPermissionOutcome{Outcome: "cancelled"}
	}

	targetKind := "allow_once"
	switch decision {
	case domain.ApprovalAllowAlways:
		targetKind = "allow_always"
	case domain.ApprovalAllowOnce:
		targetKind = "allow_once"
	case domain.ApprovalDeny:
		targetKind = "reject_once"
	}

	for _, opt := range options {
		if opt.Kind == targetKind {
			return acpPermissionOutcome{Outcome: "selected", OptionID: opt.OptionID}
		}
	}
	if decision == domain.ApprovalAllowAlways {
		for _, opt := range options {
			if opt.Kind == "allow_once" {
				return acpPermissionOutcome{Outcome: "selected", OptionID: opt.OptionID}
			}
		}
	} else if decision == domain.ApprovalAllowOnce {
		for _, opt := range options {
			if opt.Kind == "allow_always" {
				return acpPermissionOutcome{Outcome: "selected", OptionID: opt.OptionID}
			}
		}
	} else if decision == domain.ApprovalDeny {
		for _, opt := range options {
			if opt.Kind == "reject_always" {
				return acpPermissionOutcome{Outcome: "selected", OptionID: opt.OptionID}
			}
		}
	}

	if len(options) > 0 && decision != domain.ApprovalDeny {
		return acpPermissionOutcome{Outcome: "selected", OptionID: options[0].OptionID}
	}
	return acpPermissionOutcome{Outcome: "cancelled"}
}

func (a *Adapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	a.mu.RLock()
	s, ok := a.sessions[threadID]
	a.mu.RUnlock()
	if !ok {
		return errors.New("no active session for thread " + threadID)
	}

	s.mu.Lock()
	pending, ok := s.pending[requestID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending question for %s", requestID)
	}

	if len(answers) == 0 {
		select {
		case pending.reply <- acpPermissionOutcome{Outcome: "cancelled"}:
		default:
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind:     domain.EventQuestionAnswered,
			ThreadID: threadID,
			Driver:   domain.DriverAntigravity,
			Text:     "",
			Question: &domain.QuestionRequest{RequestID: requestID},
		})
		return nil
	}

	answerText := answers[0]
	selectedID := ""
	for _, opt := range pending.options {
		if strings.EqualFold(opt.OptionID, answerText) || strings.EqualFold(opt.Name, answerText) {
			selectedID = opt.OptionID
			break
		}
	}
	if selectedID == "" {
		for _, opt := range pending.options {
			if opt.Kind == "allow_once" || opt.Kind == "allow_always" {
				selectedID = opt.OptionID
				break
			}
		}
	}
	if selectedID == "" && len(pending.options) > 0 {
		selectedID = pending.options[0].OptionID
	}

	outcome := acpPermissionOutcome{Outcome: "cancelled"}
	if selectedID != "" {
		outcome = acpPermissionOutcome{Outcome: "selected", OptionID: selectedID}
	}

	select {
	case pending.reply <- outcome:
	default:
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventQuestionAnswered,
		ThreadID: threadID,
		Driver:   domain.DriverAntigravity,
		Text:     answerText,
		Question: &domain.QuestionRequest{RequestID: requestID},
	})
	return nil
}

func (a *Adapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	s, ok := a.sessions[threadID]
	delete(a.sessions, threadID)
	a.mu.Unlock()
	if !ok {
		return nil
	}

	s.conn.Close(nil)
	s.cancel()
	if s.proc != nil {
		_ = s.proc.Shutdown(2 * time.Second)
	}
	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventSessionStopped,
		ThreadID: threadID,
		Driver:   domain.DriverAntigravity,
	})
	return nil
}

func (a *Adapter) StopAll(ctx context.Context) error {
	a.mu.Lock()
	sessions := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = make(map[string]*session)
	a.mu.Unlock()

	for _, s := range sessions {
		s.conn.Close(nil)
		s.cancel()
		if s.proc != nil {
			_ = s.proc.Shutdown(2 * time.Second)
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind:     domain.EventSessionStopped,
			ThreadID: s.threadID,
			Driver:   domain.DriverAntigravity,
		})
	}
	return nil
}

func (a *Adapter) HasSession(threadID string) bool {
	_, ok := a.lookup(threadID)
	return ok
}

func (a *Adapter) lookup(threadID string) (*session, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.sessions[threadID]
	return s, ok
}

func (a *Adapter) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	s, ok := a.lookup(threadID)
	if !ok {
		return false
	}

	s.mu.Lock()
	if model != "" {
		s.model = model
	}
	if options != nil {
		s.options = options
	}
	currentModel := s.model
	currentOpts := s.options
	sessionID := s.sessionID
	s.mu.Unlock()

	base, effort := SplitModelSelection(currentModel, currentOpts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if base != "" {
		if err := s.conn.Call(ctx, "session/set_config_option", sessionSetConfigParams{
			SessionID: sessionID,
			ConfigID:  "model",
			Value:     base,
		}, nil); err != nil {
			logger.Warnf("Antigravity", "update model failed on thread %s: %v", threadID, err)
			return false
		}
	}
	if effort != "" {
		if err := s.conn.Call(ctx, "session/set_config_option", sessionSetConfigParams{
			SessionID: sessionID,
			ConfigID:  "thought_level",
			Value:     effort,
		}, nil); err != nil {
			logger.Warnf("Antigravity", "update thought_level failed on thread %s: %v", threadID, err)
			return false
		}
	}
	return true
}

func (a *Adapter) Session(threadID string) (domain.Session, bool) {
	a.mu.RLock()
	s, ok := a.sessions[threadID]
	a.mu.RUnlock()
	if !ok {
		return domain.Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.Session{
		ThreadID:          threadID,
		Driver:            domain.DriverAntigravity,
		ProviderSessionID: s.sessionID,
		Cwd:               s.cwd,
		Model:             s.model,
	}, true
}

func (a *Adapter) SessionPID(threadID string) (int, bool) {
	a.mu.RLock()
	s, ok := a.sessions[threadID]
	a.mu.RUnlock()
	if !ok {
		return 0, false
	}
	if s.proc == nil {
		return 0, false
	}
	return s.proc.PID(), true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func mcpServersFor(cfg *domain.MCPConfig) []any {
	if cfg == nil || cfg.Endpoint == "" {
		return []any{}
	}
	return []any{
		map[string]any{
			"type": "http",
			"name": firstNonEmpty(cfg.ServerName, "orchestrator"),
			"url":  cfg.Endpoint,
			"headers": []map[string]string{
				{"name": "Authorization", "value": cfg.AuthHeader},
			},
		},
	}
}
