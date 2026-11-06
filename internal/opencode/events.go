package opencode

import (
	"context"
	"encoding/json"
	"strings"

	"composer/internal/domain"
	"composer/internal/logger"
)

func (a *Adapter) handleEvent(event Event) {
	logger.Debugf("OpenCode SSE", "Event type=%s", event.Type)
	switch event.Type {
	case "message.part.created", "message.part.updated":
		a.onPartUpdated(event.Properties)
	case "message.part.delta":
		a.onPartDelta(event.Properties)
	case "message.created", "message.updated":
		a.onMessageUpdated(event.Properties)
	case "session.idle":
		a.onSessionIdle(event.Properties)
	case "session.error":
		a.onSessionError(event.Properties)
	case "permission.updated", "permission.asked":
		a.onPermission(event.Properties)
	case "question.updated", "question.asked":
		a.onQuestion(event.Properties)
	}
}

func (a *Adapter) lookupBySession(sessionID string) *thread {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.bySession[sessionID]
}

func (a *Adapter) base(t *thread, kind domain.EventKind) domain.RuntimeEvent {
	t.mu.Lock()
	turnID := t.turnID
	t.mu.Unlock()
	return domain.RuntimeEvent{
		Kind: kind, ThreadID: t.threadID, TurnID: turnID, Driver: domain.DriverOpenCode,
	}
}

func (a *Adapter) onPartUpdated(raw json.RawMessage) {
	var props PartUpdatedProperties
	if json.Unmarshal(raw, &props) != nil {
		return
	}
	part := props.Part
	t := a.lookupBySession(part.SessionID)
	if t == nil {
		return
	}

	t.mu.Lock()
	role := ""
	if t.messageRoles != nil {
		role = t.messageRoles[part.MessageID]
	}
	lastUserText := t.lastUserText
	t.mu.Unlock()

	trimmedPart := strings.TrimSpace(part.Text)
	trimmedUser := strings.TrimSpace(lastUserText)

	// If the part belongs to a user message, ignore it so it doesn't get mirrored in the assistant feed
	if role == "user" {
		logger.Debugf("OpenCode", "Skipping user message part: msg=%s part=%s type=%s", part.MessageID, part.ID, part.Type)
		return
	}
	// Fallback check if the message event hasn't arrived yet or if delta matches user text
	if role == "" && part.Type == "text" && trimmedUser != "" && trimmedPart != "" {
		if trimmedPart == trimmedUser || strings.HasPrefix(trimmedUser, trimmedPart) || strings.HasPrefix(trimmedPart, trimmedUser) {
			t.mu.Lock()
			if t.messageRoles == nil {
				t.messageRoles = make(map[string]string)
			}
			t.messageRoles[part.MessageID] = "user"
			t.mu.Unlock()
			logger.Infof("OpenCode", "Filtered user echo part: msg=%s part=%s text=%q", part.MessageID, part.ID, trimmedPart)
			return
		}
	}

	t.mu.Lock()
	t.partTypes[part.ID] = part.Type
	t.mu.Unlock()

	switch part.Type {
	case "text":
		a.emitPartText(t, part.ID, domain.EventAgentMessage, part.Text)

	case "reasoning":
		a.emitPartText(t, part.ID, domain.EventAgentThought, part.Text)

	case "tool":
		a.onToolPart(t, part)
	}
}

// emitPartText turns OpenCode's cumulative part text into deltas: only the
// suffix beyond what was already sent is emitted, keyed by the part id. A
// rewrite that does not extend the previous text replaces the block.
func (a *Adapter) emitPartText(t *thread, partID string, kind domain.EventKind, full string) {
	if full == "" {
		return
	}
	t.mu.Lock()
	sent := t.partText[partID]
	t.partText[partID] = full
	t.mu.Unlock()

	event := a.base(t, kind)
	event.ItemID = partID
	switch {
	case full == sent:
		return
	case strings.HasPrefix(full, sent):
		event.Text = full[len(sent):]
		event.Delta = true
	default:
		event.Text = full
	}
	a.emit.Emit(event)
}

// onPartDelta handles message.part.delta, the incremental stream newer
// servers send for text and reasoning parts.
func (a *Adapter) onPartDelta(raw json.RawMessage) {
	var props PartDeltaProperties
	if json.Unmarshal(raw, &props) != nil || props.Delta == "" {
		return
	}
	if props.Field != "" && props.Field != "text" {
		return
	}
	t := a.lookupBySession(props.SessionID)
	if t == nil {
		return
	}
	t.mu.Lock()
	role := t.messageRoles[props.MessageID]
	partType := t.partTypes[props.PartID]
	if role == "user" {
		t.mu.Unlock()
		return
	}
	t.partText[props.PartID] += props.Delta
	t.mu.Unlock()

	kind := domain.EventAgentMessage
	if partType == "reasoning" {
		kind = domain.EventAgentThought
	}
	event := a.base(t, kind)
	event.ItemID = props.PartID
	event.Text = props.Delta
	event.Delta = true
	a.emit.Emit(event)
}

// onToolPart emits a call the first time a tool id is seen and results
// thereafter, so the UI gets a single lifecycle per invocation.
func (a *Adapter) onToolPart(t *thread, part Part) {
	if part.State == nil {
		return
	}
	id := part.CallID
	if id == "" {
		id = part.ID
	}

	tool := &domain.ToolCall{ID: id, Name: part.Tool, Status: mapToolStatus(part.State.Status)}
	var inputMap map[string]any
	if len(part.State.Input) > 0 {
		_ = json.Unmarshal(part.State.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = make(map[string]any)
	}
	if part.State.Title != "" && inputMap["title"] == nil {
		inputMap["title"] = part.State.Title
	}
	tool.Input = inputMap
	tool.Output = part.State.Output
	if part.State.Error != "" {
		tool.Status = domain.ToolFailed
		tool.Output = part.State.Error
	}

	t.mu.Lock()
	seen := t.tools[id]
	t.tools[id] = part.State.Status
	t.mu.Unlock()

	if strings.ToLower(part.Tool) == "question" || part.Tool == "ask_question" {
		return
	}

	kind := domain.EventToolResult
	if seen == "" {
		kind = domain.EventToolCall
	}

	logger.Infof("OpenCode", "Tool event %s: id=%s name=%s status=%s title=%s", kind, id, tool.Name, tool.Status, part.State.Title)

	event := a.base(t, kind)
	event.Tool = tool
	event.ItemID = id
	a.emit.Emit(event)
}

func mapToolStatus(status string) domain.ToolStatus {
	switch status {
	case "running":
		return domain.ToolInProgress
	case "completed":
		return domain.ToolCompleted
	case "error":
		return domain.ToolFailed
	default:
		return domain.ToolPending
	}
}

func parseMessageError(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var errObj struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &errObj); err == nil {
		if errObj.Data.Message != "" {
			return errObj.Data.Message
		}
		if errObj.Message != "" {
			return errObj.Message
		}
		if errObj.Name != "" {
			return errObj.Name
		}
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil && str != "" {
		return str
	}
	return string(raw)
}

func (a *Adapter) onMessageUpdated(raw json.RawMessage) {
	var props MessageUpdatedProperties
	if json.Unmarshal(raw, &props) != nil {
		return
	}
	info := props.Info
	if info.SessionID == "" || info.ID == "" {
		return
	}
	t := a.lookupBySession(info.SessionID)
	if t == nil {
		return
	}

	t.mu.Lock()
	if t.messageRoles == nil {
		t.messageRoles = make(map[string]string)
	}
	if info.Role != "" {
		t.messageRoles[info.ID] = info.Role
	}
	t.mu.Unlock()

	logger.Infof("OpenCode", "Message updated: id=%s role=%s session=%s", info.ID, info.Role, info.SessionID)

	if len(info.Error) > 0 && string(info.Error) != "null" {
		errMsg := parseMessageError(info.Error)
		t.mu.Lock()
		turnID := t.turnID
		t.turnID = ""
		t.lastUserText = ""
		t.tools = make(map[string]string)
		t.partText = make(map[string]string)
		t.partTypes = make(map[string]string)
		t.mu.Unlock()

		if turnID != "" {
			if strings.Contains(errMsg, "Aborted") || strings.Contains(string(info.Error), "MessageAbortedError") {
				logger.Infof("OpenCode", "Turn aborted via message error: turnID=%s threadID=%s", turnID, t.threadID)
				a.emit.Emit(domain.RuntimeEvent{
					Kind: domain.EventTurnCompleted, ThreadID: t.threadID, TurnID: turnID,
					Driver: domain.DriverOpenCode, StopReason: domain.StopCancelled,
				})
			} else {
				logger.Errorf("OpenCode", "Turn failed via message error: turnID=%s threadID=%s error=%s", turnID, t.threadID, errMsg)
				a.emit.Emit(domain.RuntimeEvent{
					Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
					Driver: domain.DriverOpenCode, Error: errMsg,
				})
			}
		}
	}

	if info.Role != "assistant" || info.Tokens == nil {
		return
	}

	event := a.base(t, domain.EventUsage)
	event.Usage = &domain.Usage{
		InputTokens:      info.Tokens.Input,
		OutputTokens:     info.Tokens.Output,
		CacheReadTokens:  info.Tokens.Cache.Read,
		CacheWriteTokens: info.Tokens.Cache.Write,
		CostUSD:          info.Cost,
		ContextTokens:    info.Tokens.Input + info.Tokens.Output + info.Tokens.Reasoning + info.Tokens.Cache.Read + info.Tokens.Cache.Write,
		ContextWindow:    a.contextLimit(t, info.ProviderID, info.ModelID),
	}
	a.emit.Emit(event)
}

// onSessionIdle marks the end of a turn: OpenCode reports completion by going
// idle rather than by acknowledging the prompt call.
func (a *Adapter) onSessionIdle(raw json.RawMessage) {
	var props SessionIdleProperties
	if json.Unmarshal(raw, &props) != nil {
		return
	}
	t := a.lookupBySession(props.SessionID)
	if t == nil {
		return
	}

	t.mu.Lock()
	turnID := t.turnID
	t.turnID = ""
	t.lastUserText = ""
	t.tools = make(map[string]string)
	t.partText = make(map[string]string)
	t.partTypes = make(map[string]string)
	t.mu.Unlock()
	if turnID == "" {
		return
	}

	logger.Infof("OpenCode", "Session idle (turn completed): turnID=%s threadID=%s", turnID, t.threadID)

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventTurnCompleted, ThreadID: t.threadID, TurnID: turnID,
		Driver: domain.DriverOpenCode, StopReason: domain.StopEndTurn,
	})
}

func (a *Adapter) onSessionError(raw json.RawMessage) {
	var props SessionErrorProperties
	if json.Unmarshal(raw, &props) != nil {
		return
	}
	t := a.lookupBySession(props.SessionID)
	if t == nil {
		return
	}

	t.mu.Lock()
	turnID := t.turnID
	t.turnID = ""
	t.lastUserText = ""
	t.mu.Unlock()

	logger.Errorf("OpenCode", "Session error: %s (turnID=%s threadID=%s)", string(props.Error), turnID, t.threadID)

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
		Driver: domain.DriverOpenCode, Error: string(props.Error),
	})
}

func (a *Adapter) onPermission(raw json.RawMessage) {
	var props PermissionProperties
	if json.Unmarshal(raw, &props) != nil || props.ID == "" {
		return
	}
	t := a.lookupBySession(props.SessionID)
	if t == nil {
		return
	}

	a.mu.Lock()
	a.pending[props.ID] = props.SessionID
	a.mu.Unlock()

	if t.permission == domain.PermissionBypass {
		logger.Infof("OpenCode", "Auto-approving permission request %s for session %s (bypass active)", props.ID, props.SessionID)
		go func() {
			if err := a.RespondToApproval(context.Background(), t.threadID, props.ID, domain.ApprovalAllowAlways); err != nil {
				logger.Errorf("OpenCode", "Failed to auto-approve permission %s: %v", props.ID, err)
			}
		}()
		return
	}

	title := props.Title
	if title == "" {
		title = props.Type
	}

	event := a.base(t, domain.EventApprovalRequest)
	event.Approval = &domain.ApprovalRequest{
		RequestID: props.ID,
		Title:     title,
		Detail:    props.Pattern,
		Options: []domain.ApprovalOption{
			{ID: "once", Name: "Allow once", Kind: domain.ApprovalAllowOnce},
			{ID: "always", Name: "Always allow", Kind: domain.ApprovalAllowAlways},
			{ID: "reject", Name: "Deny", Kind: domain.ApprovalDeny},
		},
	}
	a.emit.Emit(event)
}

type OpenCodeQuestionItem struct {
	Header   string `json:"header"`
	Question string `json:"question"`
	Options  []any  `json:"options"`
}

type OpenCodeQuestionProps struct {
	ID        string                 `json:"id"`
	RequestID string                 `json:"requestID"`
	SessionID string                 `json:"sessionID"`
	Questions []OpenCodeQuestionItem `json:"questions"`
}

func (a *Adapter) onQuestion(raw json.RawMessage) {
	var props OpenCodeQuestionProps
	if json.Unmarshal(raw, &props) != nil {
		return
	}
	reqID := props.RequestID
	if reqID == "" {
		reqID = props.ID
	}
	if reqID == "" {
		return
	}
	t := a.lookupBySession(props.SessionID)
	if t == nil {
		return
	}

	a.mu.Lock()
	if a.pendingQuestions == nil {
		a.pendingQuestions = make(map[string]string)
	}
	a.pendingQuestions[reqID] = props.SessionID
	a.mu.Unlock()

	var domainQuestions []domain.QuestionItem
	for _, q := range props.Questions {
		text := q.Question
		if text == "" {
			text = q.Header
		} else if q.Header != "" && q.Header != text {
			text = q.Header + ": " + text
		}
		domainQuestions = append(domainQuestions, domain.QuestionItem{
			Question: text,
			Options:  formatQuestionOptions(q.Options),
		})
	}
	if len(domainQuestions) == 0 {
		domainQuestions = []domain.QuestionItem{
			{Question: "Agent requires input"},
		}
	}

	event := a.base(t, domain.EventQuestionAsked)
	event.Question = &domain.QuestionRequest{
		RequestID: reqID,
		Questions: domainQuestions,
	}
	a.emit.Emit(event)
}

func formatQuestionOptions(opts []any) []string {
	out := make([]string, 0, len(opts))
	for _, opt := range opts {
		switch v := opt.(type) {
		case string:
			if v != "" {
				out = append(out, v)
			}
		case map[string]any:
			label := ""
			if s, ok := v["label"].(string); ok && s != "" {
				label = s
			} else if s, ok := v["value"].(string); ok && s != "" {
				label = s
			}
			if d, ok := v["description"].(string); ok && d != "" {
				if label != "" {
					label = label + " — " + d
				} else {
					label = d
				}
			}
			if label != "" {
				out = append(out, label)
			}
		}
	}
	return out
}
