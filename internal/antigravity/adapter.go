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

const (
	defaultWakeupMessageSettleDelay  = 1500 * time.Millisecond
	defaultWakeupActivitySettleDelay = 15 * time.Second
)

type Adapter struct {
	settings domain.ProviderSettings
	emit     provider.Emitter

	wakeupMessageSettleDelay  time.Duration
	wakeupActivitySettleDelay time.Duration

	mu       sync.RWMutex
	sessions map[string]*session
}

func (a *Adapter) wakeupMessageDelay() time.Duration {
	if a.wakeupMessageSettleDelay > 0 {
		return a.wakeupMessageSettleDelay
	}
	return defaultWakeupMessageSettleDelay
}

func (a *Adapter) wakeupActivityDelay() time.Duration {
	if a.wakeupActivitySettleDelay > 0 {
		return a.wakeupActivitySettleDelay
	}
	return defaultWakeupActivitySettleDelay
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
	options    []acpPermissionOption
	reply      chan acpPermissionOutcome
	questionCh chan acpPermissionOutcome
	isQuestion bool
}

type session struct {
	threadID        string
	account         string
	authMethod      string
	cwd             string
	model           string
	options         domain.ModelOptions
	permission      domain.PermissionMode
	skills          *domain.SkillPolicy
	sessionID       string
	contextWindow   int64
	contextUsed     int64
	thoughtConfigID string
	hasConfigOpts   bool

	proc    *process.Process
	conn    *jsonrpc.Conn
	procCtx context.Context
	cancel  context.CancelFunc

	mu             sync.Mutex
	turnID         string
	turnSettled    chan struct{}
	wakeupTurn     bool
	wakeupSeq      uint64
	wakeupTimer    *time.Timer
	interrupting   bool
	loadingHistory bool

	activeKind string // "", "message", "thought"
	segmentSeq int

	tools           map[string]bool
	baselines       map[string]string
	nativeSubagents map[string]bool

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
	case domain.PermissionPlan:
		return "plan"
	default:
		return "default"
	}
}

func resolveSessionMode(desired string, rawModes json.RawMessage) (string, bool) {
	if desired == "" {
		return "", false
	}
	if len(rawModes) == 0 || string(rawModes) == "null" {
		return desired, true
	}
	var modes acpSessionModes
	if err := json.Unmarshal(rawModes, &modes); err != nil || len(modes.AvailableModes) == 0 {
		return desired, true
	}
	hasDefault := false
	for _, m := range modes.AvailableModes {
		if m.ID == desired {
			return desired, true
		}
		if m.ID == "default" {
			hasDefault = true
		}
	}
	if hasDefault {
		return "default", true
	}
	return "", false
}

func resolveThoughtConfigID(configOptions []acpConfigOption) (id string, hasOptions bool) {
	if len(configOptions) == 0 {
		return "thought_level", false
	}
	for _, opt := range configOptions {
		if opt.Category == "thought_level" {
			return opt.ID, true
		}
	}
	for _, opt := range configOptions {
		if opt.Category == "" && opt.ID == "thought_level" {
			return opt.ID, true
		}
	}
	return "", true
}

func pickAuthMethod(methods []acpAuthMethod, preferred string) string {
	if preferred != "" {
		for _, m := range methods {
			if m.ID == preferred {
				return m.ID
			}
		}
	}
	for _, m := range methods {
		if m.ID == "oauth-personal" {
			return m.ID
		}
	}
	return methods[0].ID
}

func (a *Adapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	env := provider.LaunchEnv(a.settings)
	if _, ok := shell.LookPath(a.binary(), env); !ok {
		return domain.Session{}, fmt.Errorf("%w: %s", process.ErrNotFound, a.binary())
	}

	cwd := strings.TrimSpace(in.Cwd)
	if cwd == "" {
		return domain.Session{}, errors.New("working directory is required")
	}
	in.Cwd = cwd

	normAccount := domain.NormalizeAccount(in.Account)
	args := []string{"acp", "--stdio", "--add-dir", cwd}
	if normAccount != domain.DefaultAccountID {
		if geminiDir := firstNonEmpty(env["GEMINI_DIR"], env["GEMINI_CONFIG_DIR"], env["AGY_GEMINI_DIR"]); geminiDir != "" {
			args = append(args, "--gemini-dir", geminiDir)
		}
	}
	if in.Permission == domain.PermissionBypass {
		args = append(args, "--dangerously-skip-permissions")
	}
	args = append(args, shell.TokenizeArgs(a.settings.LaunchArgs)...)

	authMethod := "oauth-personal"
	if env["GEMINI_API_KEY"] != "" {
		authMethod = "gemini-api-key"
	}

	var unlockSpawn func()
	if env["GEMINI_API_KEY"] == "" {
		SpawnMutex.Lock()
		activatedNonDefault := normAccount != domain.DefaultAccountID
		unlockSpawn = sync.OnceFunc(func() {
			if activatedNonDefault {
				if err := DefaultKeyringStore.RestoreDefault(); err != nil {
					logger.Warnf("Antigravity", "restore default keyring profile: %v", err)
				}
			}
			SpawnMutex.Unlock()
		})
		defer func() {
			if unlockSpawn != nil {
				unlockSpawn()
			}
		}()
		if err := DefaultKeyringStore.ActivateProfile(in.Account); err != nil {
			return domain.Session{}, fmt.Errorf("activate antigravity profile %q: %w", in.Account, err)
		}
	}

	procCtx, cancel := context.WithCancel(context.Background())
	proc, err := process.Start(procCtx, process.Spec{
		Command:       a.binary(),
		Args:          args,
		Cwd:           cwd,
		Env:           env,
		KillTree:      true,
		DieWithParent: true,
	})
	if err != nil {
		cancel()
		return domain.Session{}, fmt.Errorf("start %s: %w", a.binary(), err)
	}

	model := firstNonEmpty(in.Model, a.settings.Model)
	s := &session{
		threadID:        in.ThreadID,
		account:         normAccount,
		authMethod:      authMethod,
		cwd:             cwd,
		model:           model,
		options:         in.Options,
		permission:      in.Permission,
		skills:          in.Skills,
		thoughtConfigID: "thought_level",
		proc:            proc,
		procCtx:         procCtx,
		cancel:          cancel,
		tools:           make(map[string]bool),
		baselines:       make(map[string]string),
		nativeSubagents: make(map[string]bool),
		pending:         make(map[string]*pendingApproval),
	}

	conn := jsonrpc.NewConn(proc.Stdout(), proc.Stdin(), a.makeHandler(s))
	s.conn = conn

	if err := a.initSession(ctx, s, in); err != nil {
		conn.Close(err)
		cancel()
		_ = proc.Shutdown(2 * time.Second)
		return domain.Session{}, err
	}

	if unlockSpawn != nil {
		unlockSpawn()
		unlockSpawn = nil
	}

	a.mu.Lock()
	a.sessions[in.ThreadID] = s
	a.mu.Unlock()

	go a.monitor(s)

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventSessionStarted,
		ThreadID: in.ThreadID,
		Driver:   domain.DriverAntigravity,
		Account:  s.account,
		Text:     s.sessionID,
	})

	return domain.Session{
		ThreadID:          in.ThreadID,
		InstanceID:        in.InstanceID,
		Driver:            domain.DriverAntigravity,
		Account:           s.account,
		ProviderSessionID: s.sessionID,
		Cwd:               cwd,
		Model:             s.model,
		StartedAt:         time.Now().UnixMilli(),
	}, nil
}

func (a *Adapter) initSession(ctx context.Context, s *session, in domain.SessionStartInput) error {
	initCtx, cancelInit := context.WithTimeout(ctx, 60*time.Second)
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

	if len(initRes.AuthMethods) > 0 && !initRes.Authenticated {
		methodID := pickAuthMethod(initRes.AuthMethods, s.authMethod)
		if err := s.conn.Call(initCtx, "authenticate", authenticateParams{MethodID: methodID}, nil); err != nil {
			return fmt.Errorf("authenticate %s (%s): %w", a.binary(), methodID, err)
		}
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
	var sessRes sessionNewResult
	if in.Resume != "" {
		// Prefer ACP session/resume so agy reattaches to the persisted session
		// without replaying historical session/update chunks (Orchestrator already
		// holds the canonical JSONL transcript). Fall back to session/load only if
		// the server does not support session/resume.
		s.mu.Lock()
		s.loadingHistory = true
		s.mu.Unlock()
		loadCtx, cancelLoad := context.WithTimeout(ctx, 20*time.Second)
		params := sessionLoadParams{
			SessionID:  in.Resume,
			Cwd:        s.cwd,
			MCPServers: mcpServersFor(in.MCP),
		}
		var err error
		if initRes.supportsResume() {
			err = s.conn.Call(loadCtx, "session/resume", params, &sessRes)
			if err != nil {
				logger.Infof("Antigravity", "session/resume failed (%v), falling back to session/load", err)
				err = s.conn.Call(loadCtx, "session/load", params, &sessRes)
			}
		} else {
			err = s.conn.Call(loadCtx, "session/load", params, &sessRes)
		}
		cancelLoad()
		s.mu.Lock()
		s.loadingHistory = false
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("session/load %s: %w", a.binary(), err)
		}
		sessionID = sessRes.SessionID
		if sessionID == "" {
			sessionID = in.Resume
		}
	} else {
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
		err := s.conn.Call(newCtx, "session/new", sessionNewParams{
			Cwd:        s.cwd,
			MCPServers: mcpServersFor(in.MCP),
			Meta:       meta,
		}, &sessRes)
		cancelNew()
		if err != nil {
			return fmt.Errorf("session/new %s: %w", a.binary(), err)
		}
		sessionID = sessRes.SessionID
	}
	s.sessionID = sessionID

	thoughtID, hasCfg := resolveThoughtConfigID(sessRes.ConfigOptions)
	s.mu.Lock()
	s.thoughtConfigID = thoughtID
	s.hasConfigOpts = hasCfg
	s.mu.Unlock()

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
		if effort != "" && thoughtID != "" {
			if err := s.conn.Call(cfgCtx, "session/set_config_option", sessionSetConfigParams{
				SessionID: s.sessionID,
				ConfigID:  thoughtID,
				Value:     effort,
			}, nil); err != nil {
				logger.Warnf("Antigravity", "set initial %s %q on thread %s: %v", thoughtID, effort, in.ThreadID, err)
			}
		}
	}

	if mode, ok := resolveSessionMode(permissionMode(in.Permission), sessRes.Modes); ok {
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

func (s *session) claimTurnSettlement(turnID string) (interrupting bool, ctxWindow int64, ctxUsed int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if turnID == "" || s.turnID != turnID {
		return false, 0, 0, false
	}
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
	interrupting = s.interrupting
	ctxWindow = s.contextWindow
	ctxUsed = s.contextUsed
	s.turnID = ""
	s.wakeupTurn = false
	s.interrupting = false
	s.activeKind = ""
	return interrupting, ctxWindow, ctxUsed, true
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
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
	if turnID != "" {
		s.turnID = ""
		s.wakeupTurn = false
		s.interrupting = false
		s.activeKind = ""
	}
	pending := s.pending
	s.pending = make(map[string]*pendingApproval)
	s.mu.Unlock()

	for _, p := range pending {
		if p.reply != nil {
			select {
			case p.reply <- acpPermissionOutcome{Outcome: "cancelled"}:
			default:
			}
		}
		if p.questionCh != nil {
			select {
			case p.questionCh <- acpPermissionOutcome{Outcome: "cancelled"}:
			default:
			}
		}
	}

	if turnID != "" {
		if interrupting {
			a.emit.Emit(domain.RuntimeEvent{
				Kind:       domain.EventTurnCompleted,
				ThreadID:   s.threadID,
				TurnID:     turnID,
				Driver:     domain.DriverAntigravity,
				Account:    s.account,
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
				Account:  s.account,
				Error:    a.binary() + " exited: " + detail,
			})
		}
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventSessionStopped,
		ThreadID: s.threadID,
		Driver:   domain.DriverAntigravity,
		Account:  s.account,
	})
	s.cancel()
}

func isWakeupSessionUpdate(update *acpSessionUpdate) bool {
	if update == nil {
		return false
	}
	switch update.SessionUpdate {
	case "agent_message_chunk", "agent_thought_chunk":
		var chunk acpTextContent
		if err := json.Unmarshal(update.Content, &chunk); err != nil {
			return false
		}
		return strings.TrimSpace(chunk.Text) != ""
	case "tool_call", "tool_call_update":
		return !hiddenTools[update.Name]
	case "plan":
		return len(update.Entries) > 0
	default:
		return false
	}
}

func (a *Adapter) armWakeupTimerLocked(s *session, turnID string, delay time.Duration) {
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
	if !s.wakeupTurn || s.turnID != turnID || len(s.pending) > 0 {
		return
	}
	s.wakeupTimer = time.AfterFunc(delay, func() {
		a.settleWakeupTurn(s, turnID)
	})
}

func (a *Adapter) refreshWakeupTimer(s *session, turnID string) {
	s.mu.Lock()
	if !s.wakeupTurn || s.turnID != turnID {
		s.mu.Unlock()
		return
	}
	delay := a.wakeupActivityDelay()
	if s.activeKind == "message" {
		delay = a.wakeupMessageDelay()
	}
	a.armWakeupTimerLocked(s, turnID, delay)
	s.mu.Unlock()
}

func (a *Adapter) settleWakeupTurn(s *session, turnID string) {
	s.mu.Lock()
	if !s.wakeupTurn || s.turnID != turnID || len(s.pending) > 0 {
		s.mu.Unlock()
		return
	}
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
	interrupting := s.interrupting
	s.turnID = ""
	s.wakeupTurn = false
	s.interrupting = false
	s.activeKind = ""
	s.mu.Unlock()

	stopReason := domain.StopEndTurn
	if interrupting {
		stopReason = domain.StopCancelled
	}
	a.emit.Emit(domain.RuntimeEvent{
		Kind:       domain.EventTurnCompleted,
		ThreadID:   s.threadID,
		TurnID:     turnID,
		Driver:     domain.DriverAntigravity,
		Account:    s.account,
		StopReason: stopReason,
	})
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
			loading := s.loadingHistory
			turnID := s.turnID
			wakeup := s.wakeupTurn
			var startedWakeup string
			if !loading && turnID == "" && isWakeupSessionUpdate(&params.Update) {
				s.wakeupSeq++
				turnID = fmt.Sprintf("wakeup-%d", s.wakeupSeq)
				s.turnID = turnID
				s.wakeupTurn = true
				s.interrupting = false
				s.activeKind = ""
				s.segmentSeq = 0
				s.turnSettled = nil
				wakeup = true
				startedWakeup = turnID
			}
			s.mu.Unlock()
			if loading {
				return nil, nil
			}
			if turnID == "" && params.Update.SessionUpdate != "usage_update" {
				return nil, nil
			}
			if startedWakeup != "" {
				a.emit.Emit(domain.RuntimeEvent{
					Kind:     domain.EventTurnStarted,
					ThreadID: s.threadID,
					TurnID:   startedWakeup,
					Driver:   domain.DriverAntigravity,
					Account:  s.account,
				})
			}
			a.handleSessionUpdate(s, turnID, &params.Update)
			if wakeup {
				a.refreshWakeupTimer(s, turnID)
			}
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

func isQuestionPermission(tc acpToolCallPermission) bool {
	idLower := strings.ToLower(tc.ToolCallID)
	titleLower := strings.ToLower(tc.Title)
	kindLower := strings.ToLower(tc.Kind)
	return strings.HasPrefix(idLower, "interaction_") ||
		strings.HasPrefix(titleLower, "interaction_") ||
		strings.HasPrefix(kindLower, "interaction_") ||
		titleLower == "ask_question" ||
		kindLower == "ask_question" ||
		titleLower == "askuserquestion"
}

func buildQuestionRequest(reqID string, params requestPermissionParams) *domain.QuestionRequest {
	req := &domain.QuestionRequest{RequestID: reqID}
	optNames := make([]string, 0, len(params.Options))
	for _, opt := range params.Options {
		if opt.Name != "" {
			optNames = append(optNames, opt.Name)
		} else if opt.OptionID != "" {
			optNames = append(optNames, opt.OptionID)
		}
	}

	input := params.ToolCall.RawInput
	if rawQuestions, ok := input["questions"].([]any); ok && len(rawQuestions) > 0 {
		for _, entry := range rawQuestions {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			item := domain.QuestionItem{}
			if q, ok := m["question"].(string); ok && q != "" {
				item.Question = q
			} else if h, ok := m["header"].(string); ok && h != "" {
				item.Question = h
			}
			if h, ok := m["header"].(string); ok && h != "" {
				if q, ok := m["question"].(string); ok && q != "" && q != h {
					item.Question = h + ": " + q
				}
			}
			if multi, ok := m["multiSelect"].(bool); ok {
				item.MultiSelect = multi
			}
			if opts, ok := m["options"].([]any); ok {
				for _, o := range opts {
					switch v := o.(type) {
					case string:
						if v != "" {
							item.Options = append(item.Options, v)
						}
					case map[string]any:
						label, _ := v["label"].(string)
						if label == "" {
							label, _ = v["value"].(string)
						}
						if desc, _ := v["description"].(string); desc != "" {
							if label != "" {
								label = label + " — " + desc
							} else {
								label = desc
							}
						}
						if label != "" {
							item.Options = append(item.Options, label)
						}
					}
				}
			}
			if len(item.Options) == 0 && len(optNames) > 0 {
				item.Options = append([]string(nil), optNames...)
			}
			if item.Question != "" {
				req.Questions = append(req.Questions, item)
			}
		}
		if len(req.Questions) > 0 {
			return req
		}
	}

	qText, _ := input["question"].(string)
	if qText == "" {
		qText, _ = input["prompt"].(string)
	}
	if qText == "" {
		qText = firstNonEmpty(params.ToolCall.Title, "Agent requires input")
	}
	var itemOpts []string
	if opts, ok := input["options"].([]any); ok {
		for _, o := range opts {
			if str, ok := o.(string); ok && str != "" {
				itemOpts = append(itemOpts, str)
			} else if m, ok := o.(map[string]any); ok {
				if label, _ := m["label"].(string); label != "" {
					itemOpts = append(itemOpts, label)
				}
			}
		}
	}
	if len(itemOpts) == 0 {
		itemOpts = optNames
	}
	req.Questions = []domain.QuestionItem{{
		Question: qText,
		Options:  itemOpts,
	}}
	return req
}

func (a *Adapter) handleRequestPermission(ctx context.Context, s *session, params requestPermissionParams) (any, error) {
	reqID := params.ToolCall.ToolCallID
	if reqID == "" {
		reqID = fmt.Sprintf("req-%d", s.reqSeq.Add(1))
	}

	var procDone <-chan struct{}
	if s.proc != nil {
		procDone = s.proc.Done()
	}

	if isQuestionPermission(params.ToolCall) {
		questionCh := make(chan acpPermissionOutcome, 1)
		s.mu.Lock()
		s.pending[reqID] = &pendingApproval{
			options:    params.Options,
			questionCh: questionCh,
			isQuestion: true,
		}
		if s.wakeupTimer != nil {
			s.wakeupTimer.Stop()
			s.wakeupTimer = nil
		}
		turnID := s.turnID
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.pending, reqID)
			if s.wakeupTurn && s.turnID != "" && len(s.pending) == 0 {
				a.armWakeupTimerLocked(s, s.turnID, a.wakeupActivityDelay())
			}
			s.mu.Unlock()
		}()

		a.emit.Emit(domain.RuntimeEvent{
			Kind:     domain.EventQuestionAsked,
			ThreadID: s.threadID,
			TurnID:   turnID,
			Driver:   domain.DriverAntigravity,
			Account:  s.account,
			ItemID:   reqID,
			Question: buildQuestionRequest(reqID, params),
		})

		select {
		case outcome := <-questionCh:
			return requestPermissionResult{Outcome: outcome}, nil
		case <-procDone:
			return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, nil
		case <-s.conn.Done():
			return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, nil
		case <-ctx.Done():
			return requestPermissionResult{Outcome: acpPermissionOutcome{Outcome: "cancelled"}}, ctx.Err()
		}
	}

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

	replyCh := make(chan acpPermissionOutcome, 1)
	s.mu.Lock()
	s.pending[reqID] = &pendingApproval{
		options: params.Options,
		reply:   replyCh,
	}
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
	turnID := s.turnID
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, reqID)
		if s.wakeupTurn && s.turnID != "" && len(s.pending) == 0 {
			a.armWakeupTimerLocked(s, s.turnID, a.wakeupActivityDelay())
		}
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
		Account:  s.account,
		Approval: &domain.ApprovalRequest{
			RequestID: reqID,
			Title:     params.ToolCall.Title,
			Detail:    params.ToolCall.Kind,
			Tool:      tool,
			Options:   domainOpts,
		},
	})

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
		prevTurnID := s.turnID
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
		if _, _, _, claimed := s.claimTurnSettlement(prevTurnID); claimed {
			a.emit.Emit(domain.RuntimeEvent{
				Kind:       domain.EventTurnCompleted,
				ThreadID:   s.threadID,
				TurnID:     prevTurnID,
				Driver:     domain.DriverAntigravity,
				Account:    s.account,
				StopReason: domain.StopCancelled,
			})
		}
		s.mu.Lock()
	}

	s.turnID = in.TurnID
	s.wakeupTurn = false
	if s.wakeupTimer != nil {
		s.wakeupTimer.Stop()
		s.wakeupTimer = nil
	}
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
		Account:  s.account,
	})

	go func() {
		defer close(settled)

		var res sessionPromptResult
		err := s.conn.Call(context.Background(), "session/prompt", sessionPromptParams{
			SessionID: s.sessionID,
			Prompt:    promptBlocks,
		}, &res)

		interrupting, ctxWindow, ctxUsed, claimed := s.claimTurnSettlement(turnID)
		if !claimed {
			return
		}

		if err != nil {
			if interrupting || errors.Is(err, context.Canceled) {
				a.emit.Emit(domain.RuntimeEvent{
					Kind:       domain.EventTurnCompleted,
					ThreadID:   s.threadID,
					TurnID:     turnID,
					Driver:     domain.DriverAntigravity,
					Account:    s.account,
					StopReason: domain.StopCancelled,
				})
			} else {
				a.emit.Emit(domain.RuntimeEvent{
					Kind:     domain.EventTurnFailed,
					ThreadID: s.threadID,
					TurnID:   turnID,
					Driver:   domain.DriverAntigravity,
					Account:  s.account,
					Error:    err.Error(),
				})
			}
			return
		}

		if res.Usage != nil {
			// session/prompt returns cumulative token totals across all tool-call
			// loops in the turn, whereas current context window fill comes from
			// usage_update (ctxUsed). Never overwrite ContextTokens with cumulative
			// TotalTokens that can exceed the context window.
			contextTokens := ctxUsed
			if contextTokens == 0 {
				candidate := res.Usage.TotalTokens
				if candidate == 0 {
					candidate = res.Usage.InputTokens + res.Usage.OutputTokens
				}
				if ctxWindow == 0 || candidate <= ctxWindow {
					contextTokens = candidate
				}
			}
			a.emit.Emit(domain.RuntimeEvent{
				Kind:     domain.EventUsage,
				ThreadID: s.threadID,
				TurnID:   turnID,
				Driver:   domain.DriverAntigravity,
				Account:  s.account,
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
			Account:    s.account,
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
	wakeup := s.wakeupTurn
	s.interrupting = true
	sessionID := s.sessionID
	s.mu.Unlock()

	_ = s.conn.Notify("session/cancel", sessionCancelParams{SessionID: sessionID})

	if wakeup {
		if _, _, _, claimed := s.claimTurnSettlement(turnID); claimed {
			a.emit.Emit(domain.RuntimeEvent{
				Kind:       domain.EventTurnCompleted,
				ThreadID:   s.threadID,
				TurnID:     turnID,
				Driver:     domain.DriverAntigravity,
				Account:    s.account,
				StopReason: domain.StopCancelled,
			})
		}
		return nil
	}

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
	turnID := s.turnID
	s.mu.Unlock()
	if !ok || pending.isQuestion {
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
		TurnID:   turnID,
		Driver:   domain.DriverAntigravity,
		Account:  s.account,
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
	turnID := s.turnID
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending question for %s", requestID)
	}

	ch := pending.questionCh
	if ch == nil {
		ch = pending.reply
	}

	if len(answers) == 0 {
		if ch != nil {
			select {
			case ch <- acpPermissionOutcome{Outcome: "cancelled"}:
			default:
			}
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind:     domain.EventQuestionAnswered,
			ThreadID: threadID,
			TurnID:   turnID,
			Driver:   domain.DriverAntigravity,
			Account:  s.account,
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

	if ch != nil {
		select {
		case ch <- outcome:
		default:
		}
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventQuestionAnswered,
		ThreadID: threadID,
		TurnID:   turnID,
		Driver:   domain.DriverAntigravity,
		Account:  s.account,
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
		Account:  s.account,
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
			Account:  s.account,
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
	thoughtID := s.thoughtConfigID
	if thoughtID == "" && !s.hasConfigOpts {
		thoughtID = "thought_level"
	}
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
	if effort != "" && thoughtID != "" {
		if err := s.conn.Call(ctx, "session/set_config_option", sessionSetConfigParams{
			SessionID: sessionID,
			ConfigID:  thoughtID,
			Value:     effort,
		}, nil); err != nil {
			logger.Warnf("Antigravity", "update %s failed on thread %s: %v", thoughtID, threadID, err)
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
		Account:           s.account,
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
