package claude

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/process"
	"composer/internal/provider"
	"composer/internal/shell"
)

const defaultBinary = "claude"

// interruptGrace is how long an in-band interrupt may take before the process
// is killed as a fallback. The manager revives a killed session on next send.
const interruptGrace = 6 * time.Second

// maxInlineImageBytes caps images sent as base64 content blocks; larger files
// fall back to an @path mention the CLI reads itself.
const maxInlineImageBytes = 5 * 1024 * 1024

type Adapter struct {
	settings domain.ProviderSettings
	emit     provider.Emitter

	mu       sync.RWMutex
	sessions map[string]*session
}

func NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) *Adapter {
	return &Adapter{settings: settings, emit: emit, sessions: make(map[string]*session)}
}

func (a *Adapter) Driver() domain.DriverKind { return domain.DriverClaude }

func (a *Adapter) Capabilities() provider.Capabilities {
	return provider.Capabilities{Resume: true, Plans: true, Approvals: true, SessionModelSwitch: true}
}

// pendingControl is a can_use_tool request waiting for the user.
type pendingControl struct {
	toolName    string
	toolUseID   string
	input       map[string]any
	suggestions json.RawMessage
}

type session struct {
	threadID   string
	permission domain.PermissionMode
	proc       *process.Process
	cancel     context.CancelFunc
	encoder    *json.Encoder
	model      string
	options    domain.ModelOptions

	mu        sync.Mutex
	writeMu   sync.Mutex
	turnID    string
	sessionID string

	runningModel  string
	contextWindow int64

	// interrupting marks a turn the user stopped, so its result maps to a
	// cancelled completion rather than an error.
	interrupting bool
	// streamedMsgs holds assistant message ids whose text already arrived as
	// stream_event deltas; the full block that follows is then skipped.
	streamedMsgs map[string]bool
	currentMsgID string
	// hiddenTools are tool_use ids rendered through another surface
	// (AskUserQuestion becomes a question card), whose results are skipped.
	hiddenTools map[string]bool
	pending     map[string]*pendingControl
	badFrames   int
	ctrlSeq     atomic.Uint64
}

func (s *session) providerSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (a *Adapter) binary() string {
	if a.settings.BinaryPath != "" {
		return a.settings.BinaryPath
	}
	return defaultBinary
}

func (a *Adapter) buildArgs(in domain.SessionStartInput) []string {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		// Token-level text and thinking deltas arrive as stream_event frames.
		"--include-partial-messages",
		// Permission prompts (and AskUserQuestion) come back to us as
		// can_use_tool control requests instead of being auto-denied.
		"--permission-prompt-tool", "stdio",
	}

	if model := firstNonEmpty(in.Model, a.settings.Model); model != "" {
		args = append(args, "--model", ResolveModelID(model, in.Options))
	}
	if effort := in.Options.String(domain.OptionEffort); effort != "" {
		args = append(args, "--effort", effort)
	}
	if in.Permission == domain.PermissionBypass {
		args = append(args, "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions")
	} else if in.Permission != "" {
		args = append(args, "--permission-mode", string(in.Permission))
	} else {
		args = append(args, "--permission-mode", "default")
	}
	// An empty tool list denies every tool, which keeps a planning session from
	// exploring the repository instead of answering.
	if in.PlanOnly {
		args = append(args, "--tools", "")
	}
	if in.Resume != "" {
		args = append(args, "--resume", in.Resume)
		if in.Fork {
			args = append(args, "--fork-session")
		}
	}
	if in.Skills != nil && in.Skills.Mode == domain.SkillsNone {
		args = append(args, "--disable-slash-commands")
	}
	if prompt := systemPromptAppend(in); prompt != "" {
		args = append(args, "--append-system-prompt", prompt)
	}

	// `thinking` and `fastMode` are Claude Code settings rather than flags, so
	// they ride along as a --settings JSON blob.
	if settings := sessionSettings(in.Options, in.Skills); settings != "" {
		args = append(args, "--settings", settings)
	}
	if thinkingSummaries(in.Options) {
		args = append(args, "--thinking-display", "summarized")
	}
	return append(args, shell.TokenizeArgs(a.settings.LaunchArgs)...)
}

func skillPrompt(policy *domain.SkillPolicy) string {
	return policy.Instruction()
}

func systemPromptAppend(in domain.SessionStartInput) string {
	parts := []string{}
	if !in.PlanOnly {
		parts = append(parts, provider.RuntimeInstructions)
	}
	if skills := skillPrompt(in.Skills); skills != "" {
		parts = append(parts, skills)
	}
	return strings.Join(parts, "\n\n")
}

func sessionSettings(options domain.ModelOptions, policy *domain.SkillPolicy) string {
	fields := map[string]any{}
	if policy != nil && policy.Mode == domain.SkillsOnly && len(policy.Denied) > 0 {
		deny := make([]string, 0, len(policy.Denied))
		for _, name := range policy.Denied {
			deny = append(deny, "Skill("+name+")")
		}
		fields["permissions"] = map[string]any{"deny": deny}
	}
	if _, ok := options[domain.OptionThinking]; ok {
		fields["alwaysThinkingEnabled"] = options.Bool(domain.OptionThinking)
	}
	if options.Bool(domain.OptionFastMode) {
		fields["fastMode"] = true
	}
	if thinkingSummaries(options) {
		fields["showThinkingSummaries"] = true
	}
	if len(fields) == 0 {
		return ""
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func thinkingSummaries(options domain.ModelOptions) bool {
	_, set := options[domain.OptionThinking]
	return !set || options.Bool(domain.OptionThinking)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *Adapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	cwd := in.Cwd
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	env := provider.LaunchEnv(a.settings)
	if in.Resume != "" {
		resolved, err := resumeCwd(sessionConfigDir(env), in.Resume, cwd)
		if err != nil {
			return domain.Session{}, err
		}
		if !samePath(resolved, cwd) {
			logger.Infof("Claude", "Resuming session %s in %s, where it was recorded, instead of %s", in.Resume, resolved, cwd)
			cwd = resolved
		}
	}
	in.Cwd = cwd

	procCtx, cancel := context.WithCancel(context.Background())

	proc, err := process.Start(procCtx, process.Spec{
		Command: a.binary(),
		Args:    a.buildArgs(in),
		Cwd:     cwd,
		Env:     env,

		DieWithParent: true,
	})
	if err != nil {
		cancel()
		return domain.Session{}, fmt.Errorf("start %s: %w", a.binary(), err)
	}

	s := &session{
		threadID:     in.ThreadID,
		permission:   in.Permission,
		proc:         proc,
		cancel:       cancel,
		encoder:      json.NewEncoder(proc.Stdin()),
		model:        in.Model,
		options:      in.Options,
		sessionID:    in.Resume,
		streamedMsgs: make(map[string]bool),
		hiddenTools:  make(map[string]bool),
		pending:      make(map[string]*pendingControl),
	}

	a.mu.Lock()
	a.sessions[in.ThreadID] = s
	a.mu.Unlock()

	go a.readLoop(s)

	// These CLIs stay silent until the first user message, so the session id
	// only appears in the init frame after SendTurn. Starting is therefore
	// non-blocking; SessionID is filled in later and surfaced via the
	// session.started event.
	return domain.Session{
		ThreadID:          in.ThreadID,
		InstanceID:        in.InstanceID,
		Driver:            domain.DriverClaude,
		ProviderSessionID: s.providerSessionID(),
		Cwd:               in.Cwd,
		Model:             in.Model,
		StartedAt:         time.Now().UnixMilli(),
	}, nil
}

func (a *Adapter) readLoop(s *session) {
	scanner := bufio.NewScanner(s.proc.Stdout())
	scanner.Buffer(make([]byte, 0, 128*1024), 32*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var envelope Envelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			// Protocol drift shows up here first; log a few, not every line.
			s.mu.Lock()
			s.badFrames++
			n := s.badFrames
			s.mu.Unlock()
			if n <= 5 {
				logger.Errorf("Claude", "unreadable frame on thread %s: %v (%s)", s.threadID, err, snippet(line))
			}
			continue
		}
		a.handleEnvelope(s, &envelope)
	}

	a.finish(s, scanner.Err())
}

func snippet(line []byte) string {
	if len(line) > 300 {
		return string(line[:300]) + "..."
	}
	return string(line)
}

func (a *Adapter) finish(s *session, err error) {
	<-s.proc.Done()
	logger.Infof("Claude", "CLI exited: thread=%s pid=%d read=%v exit=%v stderr=%q",
		s.threadID, s.proc.PID(), err, s.proc.ExitError(), s.proc.StderrTail())

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
	s.pending = make(map[string]*pendingControl)
	s.mu.Unlock()

	if turnID != "" {
		if interrupting {
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventTurnCompleted, ThreadID: s.threadID, TurnID: turnID,
				Driver: domain.DriverClaude, StopReason: domain.StopCancelled,
			})
		} else {
			detail := s.proc.StderrTail()
			if detail == "" && err != nil && !errors.Is(err, io.EOF) {
				detail = err.Error()
			}
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventTurnFailed, ThreadID: s.threadID, TurnID: turnID,
				Driver: domain.DriverClaude, Error: a.binary() + " exited: " + detail,
			})
		}
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventSessionStopped, ThreadID: s.threadID, Driver: domain.DriverClaude,
	})
	s.cancel()
}

// write serialises one JSON line onto the CLI's stdin.
func (s *session) write(value any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.encoder.Encode(value)
}

// control sends a control_request (interrupt, set_model, …). Responses are
// not awaited; the CLI's resulting frames carry the outcome.
func (s *session) control(request any) error {
	id := "composer-" + strconv.FormatUint(s.ctrlSeq.Add(1), 10)
	return s.write(ControlRequestOut{Type: "control_request", RequestID: id, Request: request})
}

func (a *Adapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error {
	s, ok := a.lookup(in.ThreadID)
	if !ok {
		return errors.New("no active session for thread " + in.ThreadID)
	}

	s.mu.Lock()
	s.turnID = in.TurnID
	s.interrupting = false
	s.mu.Unlock()

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventTurnStarted, ThreadID: in.ThreadID, TurnID: in.TurnID, Driver: domain.DriverClaude,
	})

	if err := s.write(buildUserMessage(in.Text, in.Files)); err != nil {
		return fmt.Errorf("write turn: %w", err)
	}
	return nil
}

// buildUserMessage sends images as image content blocks so they do not depend
// on @-mention expansion; other files are referenced by path.
func buildUserMessage(text string, files []domain.FileRef) InputMessage {
	content := make([]any, 0, len(files)+1)
	mentions := text
	for _, file := range files {
		if block, ok := imageBlock(file); ok {
			content = append(content, block)
			continue
		}
		mentions += "\n@" + file.Path
	}
	if strings.TrimSpace(mentions) != "" || len(content) == 0 {
		content = append(content, textInput{Type: "text", Text: mentions})
	}
	return InputMessage{Type: "user", Message: InputContent{Role: "user", Content: content}}
}

func imageBlock(file domain.FileRef) (imageInput, bool) {
	mime := strings.ToLower(file.MIME)
	if mime == "" {
		switch strings.ToLower(file.Path[strings.LastIndex(file.Path, ".")+1:]) {
		case "png":
			mime = "image/png"
		case "jpg", "jpeg":
			mime = "image/jpeg"
		case "gif":
			mime = "image/gif"
		case "webp":
			mime = "image/webp"
		}
	}
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return imageInput{}, false
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.Size() > maxInlineImageBytes {
		return imageInput{}, false
	}
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return imageInput{}, false
	}
	return imageInput{Type: "image", Source: imageSource{
		Type: "base64", MediaType: mime, Data: base64.StdEncoding.EncodeToString(data),
	}}, true
}

// InterruptTurn cancels the running turn in-band, keeping the process (and
// its prompt cache) alive. If the CLI does not settle within the grace period
// the process is killed; the manager revives it on the next send.
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
	s.mu.Unlock()

	if err := s.control(map[string]any{"subtype": "interrupt"}); err != nil {
		s.cancel()
		return s.proc.Shutdown(time.Second)
	}

	go func() {
		timer := time.NewTimer(interruptGrace)
		defer timer.Stop()
		select {
		case <-s.proc.Done():
			return
		case <-timer.C:
		}
		s.mu.Lock()
		stuck := s.turnID == turnID
		s.mu.Unlock()
		if stuck {
			logger.Infof("Claude", "interrupt did not settle on %s; killing process", threadID)
			s.cancel()
			_ = s.proc.Shutdown(time.Second)
		}
	}()
	return nil
}

func (a *Adapter) respond(s *session, requestID string, response any) error {
	return s.write(ControlResponse{
		Type:     "control_response",
		Response: ControlResponseBody{Subtype: "success", RequestID: requestID, Response: response},
	})
}

func (s *session) takePending(requestID string) *pendingControl {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending[requestID]
	delete(s.pending, requestID)
	return pending
}

func (a *Adapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	s, ok := a.lookup(threadID)
	if !ok {
		return errors.New("no active session for thread " + threadID)
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return errors.New("unknown approval request " + requestID)
	}

	behavior := "allow"
	var response map[string]any
	switch decision {
	case domain.ApprovalDeny, domain.ApprovalCancel:
		behavior = "deny"
		response = map[string]any{
			"behavior":  "deny",
			"message":   "The user denied permission for this action.",
			"interrupt": decision == domain.ApprovalCancel,
		}
	default:
		response = map[string]any{"behavior": "allow", "updatedInput": inputOrEmpty(pending.input)}
		if decision == domain.ApprovalAllowAlways && len(pending.suggestions) > 0 && string(pending.suggestions) != "null" {
			response["updatedPermissions"] = pending.suggestions
		}
	}

	if err := a.respond(s, requestID, response); err != nil {
		return fmt.Errorf("write control response: %w", err)
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventApprovalResolved,
		ThreadID: threadID,
		TurnID:   a.event(s, "").TurnID,
		Driver:   domain.DriverClaude,
		Approval: &domain.ApprovalRequest{RequestID: requestID},
		Text:     behavior,
	})
	return nil
}

func inputOrEmpty(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}

// RespondToQuestion answers an AskUserQuestion permission request. The CLI
// reads the answers from updatedInput.answers, keyed by question text; an
// empty answer list denies the tool so the model continues without it.
func (a *Adapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	s, ok := a.lookup(threadID)
	if !ok {
		return errors.New("no active session for thread " + threadID)
	}
	pending := s.takePending(requestID)
	if pending == nil {
		return errors.New("unknown question " + requestID)
	}

	var response map[string]any
	if len(answers) == 0 {
		response = map[string]any{"behavior": "deny", "message": "The user skipped this question."}
	} else {
		input := inputOrEmpty(pending.input)
		keyed := map[string]string{}
		questions, _ := input["questions"].([]any)
		for i, raw := range questions {
			if i >= len(answers) {
				break
			}
			q, _ := raw.(map[string]any)
			text, _ := q["question"].(string)
			if text == "" {
				continue
			}
			keyed[text] = matchOptionLabel(q, answers[i])
		}
		updated := make(map[string]any, len(input)+1)
		for k, v := range input {
			updated[k] = v
		}
		updated["answers"] = keyed
		response = map[string]any{"behavior": "allow", "updatedInput": updated}
	}

	if err := a.respond(s, requestID, response); err != nil {
		return fmt.Errorf("write control response: %w", err)
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventQuestionAnswered,
		ThreadID: threadID,
		Driver:   domain.DriverClaude,
		Text:     strings.Join(answers, " / "),
		Question: &domain.QuestionRequest{RequestID: requestID},
	})
	return nil
}

// matchOptionLabel maps a displayed choice ("Label — description") back to
// the option label the model offered; free text passes through unchanged.
func matchOptionLabel(question map[string]any, answer string) string {
	options, _ := question["options"].([]any)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		label, _ := option["label"].(string)
		if label == "" {
			continue
		}
		if answer == label || strings.HasPrefix(answer, label+" — ") {
			return label
		}
	}
	return answer
}

func (a *Adapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	s, ok := a.sessions[threadID]
	delete(a.sessions, threadID)
	a.mu.Unlock()
	if !ok {
		return nil
	}
	s.cancel()
	return s.proc.Shutdown(2 * time.Second)
}

func (a *Adapter) StopAll(ctx context.Context) error {
	a.mu.Lock()
	sessions := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = make(map[string]*session)
	a.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *session) {
			defer wg.Done()
			s.cancel()
			_ = s.proc.Shutdown(2 * time.Second)
		}(s)
	}
	wg.Wait()
	return nil
}

func (a *Adapter) HasSession(threadID string) bool {
	_, ok := a.lookup(threadID)
	return ok
}

// UpdateModel switches the model in place via set_model. Option changes
// (effort, thinking, context window) are launch flags, so those report false
// and the caller restarts the session with --resume.
func (a *Adapter) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	s, ok := a.lookup(threadID)
	if !ok {
		return false
	}
	s.mu.Lock()
	sameOpts := sameOptions(s.options, options)
	current := s.model
	s.mu.Unlock()
	if options != nil && !sameOpts {
		return false
	}
	if model == "" || model == current {
		return true
	}
	if err := s.control(map[string]any{"subtype": "set_model", "model": ResolveModelID(model, options)}); err != nil {
		return false
	}
	s.mu.Lock()
	s.model = model
	s.mu.Unlock()
	return true
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

// SessionPID reports the CLI process backing a thread, so servers it spawns can
// be traced back to this agent.
func (a *Adapter) SessionPID(threadID string) (int, bool) {
	s, ok := a.lookup(threadID)
	if !ok {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return 0, false
	}
	return s.proc.PID(), true
}

// Session reports the thread's current session. The provider session id is only
// known once the CLI announces it, which is after StartSession returns.
func (a *Adapter) Session(threadID string) (domain.Session, bool) {
	s, ok := a.lookup(threadID)
	if !ok {
		return domain.Session{}, false
	}
	return domain.Session{
		ThreadID:          threadID,
		Driver:            domain.DriverClaude,
		ProviderSessionID: s.providerSessionID(),
	}, true
}

func (a *Adapter) lookup(threadID string) (*session, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.sessions[threadID]
	return s, ok
}
