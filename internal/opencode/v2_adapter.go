package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/provider"
)

const instructionsKey = "composer"

type V2Adapter struct {
	settings domain.ProviderSettings
	emit     provider.Emitter
	client   *http.Client

	starting    sync.Mutex
	mu          sync.RWMutex
	server      *server
	api         *httpClient
	stream      context.CancelFunc
	mcpEndpoint string
	mcpAuth     string

	threads   map[string]*v2Thread
	bySession map[string]*v2Thread
	pending   map[string]string
	forms     map[string]*pendingForm

	limitsMu       sync.Mutex
	limits         map[string]int64
	limitsAt       time.Time
	limitsFetching chan struct{}
}

type v2Thread struct {
	threadID   string
	sessionID  string
	permission domain.PermissionMode

	mu        sync.Mutex
	model     string
	options   domain.ModelOptions
	applied   string
	turnID    string
	stepRef   *v2ModelRef
	turnUsage domain.Usage
	toolName  map[string]string
	toolSeen  map[string]bool
	itemText  map[string]string
}

type pendingForm struct {
	sessionID string
	fields    []v2FormField
	values    []map[string]any
}

func NewV2Adapter(settings domain.ProviderSettings, emit provider.Emitter) *V2Adapter {
	return &V2Adapter{
		settings:  settings,
		emit:      emit,
		client:    &http.Client{Timeout: 0},
		threads:   make(map[string]*v2Thread),
		bySession: make(map[string]*v2Thread),
		pending:   make(map[string]string),
		forms:     make(map[string]*pendingForm),
	}
}

func (a *V2Adapter) Driver() domain.DriverKind { return domain.DriverOpenCode }

func (a *V2Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{SessionModelSwitch: true, Resume: true, Approvals: true}
}

func (a *V2Adapter) liveAPI() *httpClient {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.api == nil {
		return nil
	}
	if a.server != nil && a.server.proc != nil {
		select {
		case <-a.server.proc.Done():
			logger.Errorf("OpenCode", "v2 server exited; restarting")
			if a.stream != nil {
				a.stream()
			}
			a.server, a.api, a.stream = nil, nil, nil
			a.mcpEndpoint, a.mcpAuth = "", ""
			return nil
		default:
		}
	}
	return a.api
}

func (a *V2Adapter) ensureServer(ctx context.Context) (*httpClient, error) {
	if api := a.liveAPI(); api != nil {
		return api, nil
	}
	a.starting.Lock()
	defer a.starting.Unlock()
	if api := a.liveAPI(); api != nil {
		return api, nil
	}

	srv, api, err := startV2Server(ctx, a.settings, a.client, a.onServerError, a.onServerExit)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.server, a.api, a.stream = srv, api, cancel
	a.mu.Unlock()

	go a.streamEvents(streamCtx, api)
	return api, nil
}

func (a *V2Adapter) onServerError(sessionID, errMsg string) {
	var targets []*v2Thread
	a.mu.RLock()
	if sessionID != "" {
		if t := a.bySession[sessionID]; t != nil {
			targets = append(targets, t)
		}
	} else {
		for _, t := range a.threads {
			targets = append(targets, t)
		}
	}
	a.mu.RUnlock()

	for _, t := range targets {
		t.mu.Lock()
		turnID := t.turnID
		t.turnID = ""
		t.mu.Unlock()

		if turnID != "" {
			logger.Errorf("OpenCode v2", "Failing stuck turn due to OpenCode server error: turnID=%s threadID=%s error=%s", turnID, t.threadID, errMsg)
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
				Driver: domain.DriverOpenCode, Error: errMsg,
			})
		}
	}
}

func (a *V2Adapter) onServerExit(tail string) {
	a.mu.Lock()
	stream := a.stream
	a.server = nil
	a.api = nil
	a.stream = nil
	a.mcpEndpoint = ""
	a.mcpAuth = ""
	affected := make([]*v2Thread, 0, len(a.threads))
	for _, t := range a.threads {
		affected = append(affected, t)
	}
	a.threads = make(map[string]*v2Thread)
	a.bySession = make(map[string]*v2Thread)
	a.pending = make(map[string]string)
	a.forms = make(map[string]*pendingForm)
	a.mu.Unlock()

	if stream != nil {
		stream()
	}

	for _, t := range affected {
		t.mu.Lock()
		turnID := t.turnID
		t.turnID = ""
		t.turnUsage = domain.Usage{}
		t.toolName = make(map[string]string)
		t.toolSeen = make(map[string]bool)
		t.itemText = make(map[string]string)
		t.mu.Unlock()

		if turnID != "" {
			errMsg := "OpenCode v2 server terminated unexpectedly"
			if tail != "" {
				errMsg += ": " + tail
			}
			logger.Errorf("OpenCode v2", "Server exited while turn was in progress: turnID=%s threadID=%s", turnID, t.threadID)
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
				Driver: domain.DriverOpenCode, Error: errMsg,
			})
		}
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventSessionStopped, ThreadID: t.threadID, Driver: domain.DriverOpenCode,
		})
	}
}

func (a *V2Adapter) releaseUnusedServer() {
	a.mu.Lock()
	if len(a.threads) > 0 || a.server == nil {
		a.mu.Unlock()
		return
	}
	srv, stream := a.server, a.stream
	a.server, a.api, a.stream = nil, nil, nil
	a.mcpEndpoint, a.mcpAuth = "", ""
	a.mu.Unlock()

	if stream != nil {
		stream()
	}
	if srv != nil {
		srv.stop()
	}
}

func (a *V2Adapter) ensureMCP(ctx context.Context, api *httpClient, mcpCfg *domain.MCPConfig) error {
	if mcpCfg == nil || mcpCfg.Endpoint == "" {
		return nil
	}

	a.mu.RLock()
	same := a.mcpEndpoint == mcpCfg.Endpoint && a.mcpAuth == mcpCfg.AuthHeader
	a.mu.RUnlock()
	if same {
		return nil
	}

	var headers map[string]string
	if mcpCfg.AuthHeader != "" {
		headers = map[string]string{
			"Authorization": mcpCfg.AuthHeader,
		}
	}

	name := strings.TrimSpace(mcpCfg.ServerName)
	if name == "" {
		name = "orchestrator"
	}

	req := AddMcpRequest{
		Name: name,
		Config: McpRemoteConfig{
			Type:    "remote",
			URL:     mcpCfg.Endpoint,
			Enabled: true,
			Headers: headers,
			OAuth:   false,
		},
	}

	if err := api.do(ctx, http.MethodPost, "/api/mcp", req, nil); err != nil {
		if !strings.Contains(err.Error(), "404") {
			return err
		}
		if fallbackErr := api.do(ctx, http.MethodPost, "/mcp", req, nil); fallbackErr != nil {
			return fallbackErr
		}
	}

	a.mu.Lock()
	a.mcpEndpoint = mcpCfg.Endpoint
	a.mcpAuth = mcpCfg.AuthHeader
	a.mu.Unlock()

	logger.Infof("OpenCode", "Configured v2 loopback MCP server at %s", mcpCfg.Endpoint)
	return nil
}

func (a *V2Adapter) streamEvents(ctx context.Context, api *httpClient) {
	for {
		err := subscribeV2(ctx, api, a.handleEvent)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = io.EOF
		}
		logger.Debugf("OpenCode", "v2 event stream dropped: %v", err)
		a.reconcileThreads(api)
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (a *V2Adapter) reconcileThreads(api *httpClient) {
	if api == nil {
		return
	}

	var candidates []*v2Thread
	a.mu.RLock()
	for _, t := range a.threads {
		t.mu.Lock()
		inTurn := t.turnID != ""
		t.mu.Unlock()
		if inTurn {
			candidates = append(candidates, t)
		}
	}
	a.mu.RUnlock()

	if len(candidates) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, t := range candidates {
		t.mu.Lock()
		turnID := t.turnID
		sessionID := t.sessionID
		threadID := t.threadID
		t.mu.Unlock()

		if turnID == "" || sessionID == "" {
			continue
		}

		var sessionResp struct {
			Data      *v2Session   `json:"data,omitempty"`
			ID        string       `json:"id,omitempty"`
			Status    string       `json:"status,omitempty"`
			State     string       `json:"state,omitempty"`
			Execution *v2Execution `json:"execution,omitempty"`
			Error     *v2Error     `json:"error,omitempty"`
		}

		err := api.do(ctx, http.MethodGet, "/api/session/"+url.PathEscape(sessionID), nil, &sessionResp)
		if err != nil {
			// Fallback: query /api/session/status
			var statusMap map[string]struct {
				Type   string   `json:"type"`
				Status string   `json:"status"`
				Error  *v2Error `json:"error"`
			}
			if statusErr := api.do(ctx, http.MethodGet, "/api/session/status", nil, &statusMap); statusErr == nil {
				if entry, ok := statusMap[sessionID]; ok {
					sessionResp.Status = entry.Type
					if sessionResp.Status == "" {
						sessionResp.Status = entry.Status
					}
					sessionResp.Error = entry.Error
					err = nil
				} else {
					sessionResp.Status = "idle"
					err = nil
				}
			}
		}

		if err != nil {
			continue
		}

		status := sessionResp.Status
		if sessionResp.Data != nil && sessionResp.Data.Status != "" {
			status = sessionResp.Data.Status
		}
		if status == "" && sessionResp.State != "" {
			status = sessionResp.State
		}
		if status == "" && sessionResp.Data != nil && sessionResp.Data.State != "" {
			status = sessionResp.Data.State
		}
		var exec *v2Execution
		if sessionResp.Execution != nil {
			exec = sessionResp.Execution
		} else if sessionResp.Data != nil && sessionResp.Data.Execution != nil {
			exec = sessionResp.Data.Execution
		}

		if status == "" && exec == nil {
			var statusMap map[string]struct {
				Type   string   `json:"type"`
				Status string   `json:"status"`
				Error  *v2Error `json:"error"`
			}
			if statusErr := api.do(ctx, http.MethodGet, "/api/session/status", nil, &statusMap); statusErr == nil {
				if entry, ok := statusMap[sessionID]; ok {
					status = entry.Type
					if status == "" {
						status = entry.Status
					}
					if sessionResp.Error == nil {
						sessionResp.Error = entry.Error
					}
				} else {
					status = "idle"
				}
			}
		}

		statusLower := strings.ToLower(status)
		execStatus := ""
		if exec != nil {
			execStatus = strings.ToLower(exec.Status)
		}

		if statusLower == "busy" || statusLower == "running" || execStatus == "busy" || execStatus == "running" {
			continue
		}

		if statusLower == "" && exec == nil {
			continue
		}

		var errObj *v2Error
		if sessionResp.Error != nil {
			errObj = sessionResp.Error
		} else if sessionResp.Data != nil && sessionResp.Data.Error != nil {
			errObj = sessionResp.Data.Error
		} else if exec != nil && exec.Error != nil {
			errObj = exec.Error
		}

		isFailed := statusLower == "failed" || statusLower == "error" || execStatus == "failed" || execStatus == "error" || errObj != nil
		isInterrupted := statusLower == "interrupted" || statusLower == "cancelled" || execStatus == "interrupted" || execStatus == "cancelled"

		stopReason := domain.StopEndTurn
		if isInterrupted {
			stopReason = domain.StopCancelled
		}
		errMsg := ""
		if isFailed {
			if errObj != nil {
				errMsg = errObj.String()
			}
			if errMsg == "" {
				errMsg = "OpenCode turn failed"
			}
		}

		t.mu.Lock()
		if t.turnID != turnID {
			t.mu.Unlock()
			continue
		}
		t.turnID = ""
		t.turnUsage = domain.Usage{}
		t.toolName = make(map[string]string)
		t.toolSeen = make(map[string]bool)
		t.itemText = make(map[string]string)
		t.mu.Unlock()

		if isFailed {
			logger.Warnf("OpenCode v2", "Reconciled failed turn on SSE reconnect: turnID=%s error=%s", turnID, errMsg)
			a.emit.Emit(domain.RuntimeEvent{
				Kind:     domain.EventTurnFailed,
				ThreadID: threadID,
				TurnID:   turnID,
				Driver:   domain.DriverOpenCode,
				Error:    errMsg,
			})
		} else {
			logger.Infof("OpenCode v2", "Reconciled completed turn on SSE reconnect: turnID=%s reason=%s", turnID, stopReason)
			a.emit.Emit(domain.RuntimeEvent{
				Kind:       domain.EventTurnCompleted,
				ThreadID:   threadID,
				TurnID:     turnID,
				Driver:     domain.DriverOpenCode,
				StopReason: stopReason,
			})
		}
	}
}

func v2Rules(in domain.SessionStartInput) []v2Rule {
	var rules []v2Rule
	if in.Permission == domain.PermissionBypass {
		rules = append(rules, v2Rule{Action: "*", Resource: "*", Effect: "allow"})
	}
	if in.Skills != nil && in.Skills.Mode == domain.SkillsNone {
		rules = append(rules, v2Rule{Action: "skill", Resource: "*", Effect: "deny"})
	}
	if in.PlanOnly {
		rules = append(rules, v2Rule{Action: "*", Resource: "*", Effect: "deny"})
	}
	return rules
}

func v2Instructions(policy *domain.SkillPolicy) string {
	text := provider.RuntimeInstructions
	if policy != nil && policy.Mode != domain.SkillsNone {
		if instruction := policy.Instruction(); instruction != "" {
			text = strings.TrimSpace(text + "\n\n" + instruction)
		}
	}
	return text
}

func modelKey(model, variant string) string {
	return model + "#" + variant
}

func (a *V2Adapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	cwd := strings.TrimSpace(in.Cwd)
	if cwd == "" {
		return domain.Session{}, errors.New("working directory is required")
	}
	cwd = filepath.Clean(cwd)
	in.Cwd = cwd

	api, err := a.ensureServer(ctx)
	if err != nil {
		return domain.Session{}, err
	}

	if in.MCP != nil && in.MCP.Endpoint != "" && !in.PlanOnly {
		if err := a.ensureMCP(ctx, api, in.MCP); err != nil {
			a.releaseUnusedServer()
			return domain.Session{}, fmt.Errorf("configure opencode mcp: %w", err)
		}
	}
	go a.ensureLimits(api)

	model := in.Model
	if model == "" {
		model = a.settings.Model
	}
	variant := in.Options.String(domain.OptionEffort)
	rules := v2Rules(in)

	t := &v2Thread{
		threadID:   in.ThreadID,
		permission: in.Permission,
		model:      model,
		options:    in.Options,
		toolName:   make(map[string]string),
		toolSeen:   make(map[string]bool),
		itemText:   make(map[string]string),
	}

	if in.Resume != "" {
		var existing v2Envelope[v2Session]
		if err := api.do(ctx, http.MethodGet, "/api/session/"+url.PathEscape(in.Resume), nil, &existing); err != nil {
			a.releaseUnusedServer()
			return domain.Session{}, fmt.Errorf("resume opencode v2 session %s: %w", in.Resume, err)
		}
		if existing.Data.ID == "" {
			a.releaseUnusedServer()
			return domain.Session{}, fmt.Errorf("resume opencode v2 session %s: session not found", in.Resume)
		}
		t.sessionID = existing.Data.ID
		if in.Fork {
			var forked v2Envelope[v2Session]
			if err := api.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(t.sessionID)+"/fork", map[string]any{}, &forked); err != nil {
				a.releaseUnusedServer()
				return domain.Session{}, fmt.Errorf("fork opencode v2 session %s: %w", t.sessionID, err)
			}
			if forked.Data.ID != "" {
				t.sessionID = forked.Data.ID
			}
		}
		if len(rules) > 0 {
			if err := api.do(ctx, http.MethodPatch, "/api/session/"+url.PathEscape(t.sessionID), map[string]any{"permissions": rules}, nil); err != nil {
				logger.Errorf("OpenCode", "permission update for %s failed: %v", t.sessionID, err)
			}
		}
	} else {
		request := v2SessionCreate{
			Title:       in.ThreadID,
			Model:       v2ModelRefFor(model, variant),
			Location:    &v2Location{Directory: cwd},
			Permissions: rules,
		}
		if in.Permission == domain.PermissionPlan {
			request.Agent = "plan"
		}
		var created v2Envelope[v2Session]
		if err := api.do(ctx, http.MethodPost, "/api/session", request, &created); err != nil {
			a.releaseUnusedServer()
			return domain.Session{}, err
		}
		t.sessionID = created.Data.ID
		if request.Model != nil {
			t.applied = modelKey(model, variant)
		}
	}

	if text := v2Instructions(in.Skills); text != "" {
		path := "/api/experimental/session/" + url.PathEscape(t.sessionID) + "/instructions/entries/" + instructionsKey
		if err := api.do(ctx, http.MethodPut, path, map[string]string{"value": text}, nil); err != nil {
			logger.Errorf("OpenCode", "instructions for %s failed: %v", t.sessionID, err)
		}
	}

	a.mu.Lock()
	a.threads[in.ThreadID] = t
	a.bySession[t.sessionID] = t
	a.mu.Unlock()

	logger.Infof("OpenCode", "v2 session started: thread=%s session=%s model=%s", in.ThreadID, t.sessionID, model)
	a.emit.Emit(domain.RuntimeEvent{Kind: domain.EventSessionStarted, ThreadID: in.ThreadID, Driver: domain.DriverOpenCode})

	return domain.Session{
		ThreadID:          in.ThreadID,
		InstanceID:        in.InstanceID,
		Driver:            domain.DriverOpenCode,
		ProviderSessionID: t.sessionID,
		Cwd:               cwd,
		Model:             model,
		StartedAt:         time.Now().UnixMilli(),
	}, nil
}

func (a *V2Adapter) thread(threadID string) (*v2Thread, *httpClient) {
	a.mu.RLock()
	t := a.threads[threadID]
	api := a.api
	a.mu.RUnlock()
	return t, api
}

func fileURI(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func (a *V2Adapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error {
	t, api := a.thread(in.ThreadID)
	if t == nil || api == nil {
		return errors.New("no active session for thread " + in.ThreadID)
	}

	t.mu.Lock()
	model := t.model
	variant := t.options.String(domain.OptionEffort)
	switchTo := ""
	if model != "" && modelKey(model, variant) != t.applied {
		switchTo = modelKey(model, variant)
	}
	t.mu.Unlock()

	if switchTo != "" {
		path := "/api/session/" + url.PathEscape(t.sessionID) + "/model"
		if err := api.do(ctx, http.MethodPost, path, map[string]any{"model": v2ModelRefFor(model, variant)}, nil); err != nil {
			logger.Errorf("OpenCode", "model switch to %s failed: %v", switchTo, err)
		} else {
			t.mu.Lock()
			t.applied = switchTo
			t.mu.Unlock()
		}
	}

	prompt := v2Prompt{Text: in.Text}
	for _, file := range in.Files {
		prompt.Files = append(prompt.Files, v2PromptFile{URI: fileURI(file.Path), Name: filepath.Base(file.Path)})
	}

	t.mu.Lock()
	t.turnID = in.TurnID
	t.turnUsage = domain.Usage{}
	t.mu.Unlock()
	a.emit.Emit(domain.RuntimeEvent{Kind: domain.EventTurnStarted, ThreadID: in.ThreadID, TurnID: in.TurnID, Driver: domain.DriverOpenCode})

	logger.Infof("OpenCode", "v2 SendTurn: thread=%s turn=%s files=%d model=%s", in.ThreadID, in.TurnID, len(in.Files), model)
	if err := api.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(t.sessionID)+"/prompt", prompt, nil); err != nil {
		t.mu.Lock()
		t.turnID = ""
		t.turnUsage = domain.Usage{}
		t.mu.Unlock()
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventTurnFailed, ThreadID: in.ThreadID, TurnID: in.TurnID,
			Driver: domain.DriverOpenCode, Error: err.Error(),
		})
		return err
	}
	return nil
}

func (a *V2Adapter) InterruptTurn(ctx context.Context, threadID string) error {
	t, api := a.thread(threadID)
	if t == nil || api == nil {
		return nil
	}
	err := api.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(t.sessionID)+"/interrupt", nil, nil)
	t.mu.Lock()
	turnID := t.turnID
	t.turnID = ""
	t.turnUsage = domain.Usage{}
	t.mu.Unlock()
	if turnID != "" {
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventTurnCompleted, ThreadID: t.threadID, TurnID: turnID,
			Driver: domain.DriverOpenCode, StopReason: domain.StopCancelled,
		})
	}
	return err
}

func (a *V2Adapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	a.mu.Lock()
	sessionID, ok := a.pending[requestID]
	delete(a.pending, requestID)
	api := a.api
	a.mu.Unlock()
	if !ok || api == nil {
		return errors.New("unknown approval request " + requestID)
	}

	reply := "once"
	switch decision {
	case domain.ApprovalAllowAlways:
		reply = "always"
	case domain.ApprovalDeny, domain.ApprovalCancel:
		reply = "reject"
	}
	path := "/api/session/" + url.PathEscape(sessionID) + "/permission/" + url.PathEscape(requestID) + "/reply"
	if err := api.do(ctx, http.MethodPost, path, map[string]string{"decision": reply}, nil); err != nil {
		return err
	}
	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventApprovalResolved, ThreadID: threadID, Driver: domain.DriverOpenCode,
		Approval: &domain.ApprovalRequest{RequestID: requestID}, Text: reply,
	})
	return nil
}

func (a *V2Adapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	a.mu.Lock()
	form := a.forms[requestID]
	delete(a.forms, requestID)
	api := a.api
	a.mu.Unlock()
	if form == nil || api == nil {
		return errors.New("unknown question " + requestID)
	}

	base := "/api/session/" + url.PathEscape(form.sessionID) + "/form/" + url.PathEscape(requestID)
	if len(answers) == 0 || (len(answers) == 1 && answers[0] == "skip") {
		if err := api.do(ctx, http.MethodDelete, base, nil, nil); err != nil {
			return err
		}
	} else {
		answer := make(map[string]any)
		for i, field := range form.fields {
			if i >= len(answers) {
				break
			}
			if value, ok := formValue(field, form.values[i], answers[i]); ok {
				answer[field.Key] = value
			}
		}
		if err := api.do(ctx, http.MethodPost, base+"/reply", map[string]any{"answer": answer}, nil); err != nil {
			return err
		}
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventQuestionAnswered, ThreadID: threadID, Driver: domain.DriverOpenCode,
		Text:     strings.Join(answers, " / "),
		Question: &domain.QuestionRequest{RequestID: requestID},
	})
	return nil
}

func (a *V2Adapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	t := a.threads[threadID]
	delete(a.threads, threadID)
	if t != nil {
		delete(a.bySession, t.sessionID)
	}
	api := a.api
	var idle *server
	var stream context.CancelFunc
	if len(a.threads) == 0 && a.server != nil {
		idle, stream = a.server, a.stream
		a.server, a.api, a.stream = nil, nil, nil
		a.mcpEndpoint, a.mcpAuth = "", ""
	}
	a.mu.Unlock()
	if t == nil {
		return nil
	}

	t.mu.Lock()
	busy := t.turnID != ""
	t.mu.Unlock()
	if busy && api != nil {
		_ = api.do(ctx, http.MethodPost, "/api/session/"+url.PathEscape(t.sessionID)+"/interrupt", nil, nil)
	}
	if stream != nil {
		stream()
	}
	if idle != nil {
		go idle.stop()
	}
	a.emit.Emit(domain.RuntimeEvent{Kind: domain.EventSessionStopped, ThreadID: threadID, Driver: domain.DriverOpenCode})
	return nil
}

func (a *V2Adapter) StopAll(ctx context.Context) error {
	a.mu.Lock()
	srv, stream := a.server, a.stream
	a.server, a.api, a.stream = nil, nil, nil
	a.mcpEndpoint, a.mcpAuth = "", ""
	a.threads = make(map[string]*v2Thread)
	a.bySession = make(map[string]*v2Thread)
	a.pending = make(map[string]string)
	a.forms = make(map[string]*pendingForm)
	a.mu.Unlock()
	if stream != nil {
		stream()
	}
	if srv != nil {
		srv.stop()
	}
	return nil
}

func (a *V2Adapter) HasSession(threadID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.threads[threadID]
	return ok
}

func (a *V2Adapter) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	t, _ := a.thread(threadID)
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if model != "" {
		t.model = model
	}
	if options != nil {
		t.options = options
	}
	return true
}

func (a *V2Adapter) Session(threadID string) (domain.Session, bool) {
	t, _ := a.thread(threadID)
	if t == nil {
		return domain.Session{}, false
	}
	return domain.Session{ThreadID: threadID, Driver: domain.DriverOpenCode, ProviderSessionID: t.sessionID}, true
}

func (a *V2Adapter) contextLimit(ref *v2ModelRef) int64 {
	if ref == nil {
		return 0
	}
	key := ref.ProviderID + "/" + ref.ID
	a.limitsMu.Lock()
	if limit, ok := a.limits[key]; ok {
		a.limitsMu.Unlock()
		return limit
	}
	wait := a.limitsFetching
	if wait == nil {
		if time.Since(a.limitsAt) < limitRefetchAfter {
			a.limitsMu.Unlock()
			return 0
		}
		a.mu.RLock()
		api := a.api
		a.mu.RUnlock()
		if api == nil {
			a.limitsMu.Unlock()
			return 0
		}
		wait = a.startLimitsFetchLocked(api)
	}
	a.limitsMu.Unlock()

	select {
	case <-wait:
	case <-time.After(2 * time.Second):
	}

	a.limitsMu.Lock()
	limit := a.limits[key]
	a.limitsMu.Unlock()
	return limit
}

func (a *V2Adapter) ensureLimits(api *httpClient) {
	if api == nil {
		return
	}
	a.limitsMu.Lock()
	if len(a.limits) > 0 || a.limitsFetching != nil || time.Since(a.limitsAt) < limitRefetchAfter {
		a.limitsMu.Unlock()
		return
	}
	wait := a.startLimitsFetchLocked(api)
	a.limitsMu.Unlock()
	<-wait
}

func (a *V2Adapter) startLimitsFetchLocked(api *httpClient) chan struct{} {
	a.limitsAt = time.Now()
	done := make(chan struct{})
	a.limitsFetching = done
	go func() {
		defer close(done)
		a.fetchLimits(api)
		a.limitsMu.Lock()
		if a.limitsFetching == done {
			a.limitsFetching = nil
		}
		a.limitsMu.Unlock()
	}()
	return done
}

func (a *V2Adapter) fetchLimits(api *httpClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := fetchV2Models(ctx, api)
	if err != nil {
		return
	}
	table := make(map[string]int64, len(models))
	for _, model := range models {
		if model.Limit.Context > 0 {
			table[model.ProviderID+"/"+model.ID] = model.Limit.Context
		}
	}
	a.limitsMu.Lock()
	a.limits = table
	a.limitsMu.Unlock()
}
