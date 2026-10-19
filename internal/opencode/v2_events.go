package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"composer/internal/domain"
	"composer/internal/logger"
)

func (a *V2Adapter) handleEvent(event v2Event) {
	switch event.Type {
	case "session.text.delta":
		a.onStreamDelta(event.Data, domain.EventAgentMessage, "text")
	case "session.reasoning.delta":
		a.onStreamDelta(event.Data, domain.EventAgentThought, "reasoning")
	case "session.text.ended":
		a.onStreamEnded(event.Data, domain.EventAgentMessage, "text")
	case "session.reasoning.ended":
		a.onStreamEnded(event.Data, domain.EventAgentThought, "reasoning")
	case "session.step.started":
		a.onStepStarted(event.Data)
	case "session.step.ended":
		a.onStepEnded(event.Data)
	case "session.tool.input.started":
		a.onToolInputStarted(event.Data)
	case "session.tool.called":
		a.onToolCalled(event.Data)
	case "session.tool.success":
		a.onToolSettled(event.Data, false)
	case "session.tool.failed":
		a.onToolSettled(event.Data, true)
	case "session.execution.succeeded":
		a.onExecutionEnded(event.Data, domain.StopEndTurn, "")
	case "session.execution.interrupted":
		a.onExecutionEnded(event.Data, domain.StopCancelled, "")
	case "session.execution.failed":
		a.onExecutionEnded(event.Data, domain.StopError, "failed")
	case "permission.asked":
		a.onPermissionAsked(event.Data)
	case "form.created":
		a.onFormCreated(event.Data)
	}
}

func (a *V2Adapter) bySessionID(sessionID string) *v2Thread {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.bySession[sessionID]
}

func (a *V2Adapter) decode(raw json.RawMessage) (*v2StreamData, *v2Thread) {
	var data v2StreamData
	if json.Unmarshal(raw, &data) != nil || data.SessionID == "" {
		return nil, nil
	}
	t := a.bySessionID(data.SessionID)
	if t == nil {
		return nil, nil
	}
	return &data, t
}

func (a *V2Adapter) base(t *v2Thread, kind domain.EventKind) domain.RuntimeEvent {
	t.mu.Lock()
	turnID := t.turnID
	t.mu.Unlock()
	return domain.RuntimeEvent{Kind: kind, ThreadID: t.threadID, TurnID: turnID, Driver: domain.DriverOpenCode}
}

func streamItem(data *v2StreamData, family string) string {
	return data.AssistantMessageID + ":" + family + ":" + strconv.Itoa(data.Ordinal)
}

func (a *V2Adapter) onStreamDelta(raw json.RawMessage, kind domain.EventKind, family string) {
	data, t := a.decode(raw)
	if t == nil || data.Delta == "" {
		return
	}
	item := streamItem(data, family)
	t.mu.Lock()
	t.itemText[item] += data.Delta
	t.mu.Unlock()

	event := a.base(t, kind)
	event.ItemID = item
	event.Text = data.Delta
	event.Delta = true
	a.emit.Emit(event)
}

func (a *V2Adapter) onStreamEnded(raw json.RawMessage, kind domain.EventKind, family string) {
	data, t := a.decode(raw)
	if t == nil || data.Text == "" {
		return
	}
	item := streamItem(data, family)
	t.mu.Lock()
	sent := t.itemText[item]
	delete(t.itemText, item)
	t.mu.Unlock()
	if data.Text == sent {
		return
	}

	event := a.base(t, kind)
	event.ItemID = item
	if strings.HasPrefix(data.Text, sent) {
		event.Text = data.Text[len(sent):]
		event.Delta = true
	} else {
		event.Text = data.Text
	}
	a.emit.Emit(event)
}

func (a *V2Adapter) onStepStarted(raw json.RawMessage) {
	data, t := a.decode(raw)
	if t == nil || data.Model == nil {
		return
	}
	t.mu.Lock()
	t.stepRef = data.Model
	t.mu.Unlock()
}

func (a *V2Adapter) onStepEnded(raw json.RawMessage) {
	data, t := a.decode(raw)
	if t == nil || data.Tokens == nil {
		return
	}
	t.mu.Lock()
	ref := t.stepRef
	t.mu.Unlock()

	tokens := data.Tokens
	event := a.base(t, domain.EventUsage)
	event.Usage = &domain.Usage{
		InputTokens:      tokens.Input,
		OutputTokens:     tokens.Output,
		CacheReadTokens:  tokens.Cache.Read,
		CacheWriteTokens: tokens.Cache.Write,
		CostUSD:          data.Cost,
		ContextTokens:    tokens.Input + tokens.Output + tokens.Reasoning + tokens.Cache.Read + tokens.Cache.Write,
		ContextWindow:    a.contextLimit(ref),
	}
	a.emit.Emit(event)
}

func isQuestionTool(name string) bool {
	lower := strings.ToLower(name)
	return lower == "question" || lower == "ask_question"
}

func (a *V2Adapter) onToolInputStarted(raw json.RawMessage) {
	data, t := a.decode(raw)
	if t == nil || data.ID == "" {
		return
	}
	t.mu.Lock()
	t.toolName[data.ID] = data.Name
	t.mu.Unlock()
}

func (a *V2Adapter) onToolCalled(raw json.RawMessage) {
	data, t := a.decode(raw)
	if t == nil || data.ID == "" {
		return
	}
	t.mu.Lock()
	name := t.toolName[data.ID]
	seen := t.toolSeen[data.ID]
	t.toolSeen[data.ID] = true
	t.mu.Unlock()
	if isQuestionTool(name) {
		return
	}

	input := map[string]any{}
	if len(data.Input) > 0 {
		_ = json.Unmarshal(data.Input, &input)
	}
	kind := domain.EventToolCall
	if seen {
		kind = domain.EventToolResult
	}
	event := a.base(t, kind)
	event.ItemID = data.ID
	event.Tool = &domain.ToolCall{ID: data.ID, Name: NormalizeToolName(name), Status: domain.ToolInProgress, Input: input}
	a.emit.Emit(event)
}

func toolOutput(content []v2ToolContent) string {
	var out strings.Builder
	for _, item := range content {
		if item.Type != "text" || item.Text == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(item.Text)
	}
	return out.String()
}

func (a *V2Adapter) onToolSettled(raw json.RawMessage, failed bool) {
	data, t := a.decode(raw)
	if t == nil || data.ID == "" {
		return
	}
	t.mu.Lock()
	name := t.toolName[data.ID]
	seen := t.toolSeen[data.ID]
	delete(t.toolName, data.ID)
	t.toolSeen[data.ID] = true
	t.mu.Unlock()
	if isQuestionTool(name) {
		return
	}

	tool := &domain.ToolCall{ID: data.ID, Name: NormalizeToolName(name), Status: domain.ToolCompleted, Output: toolOutput(data.Content)}
	if failed {
		tool.Status = domain.ToolFailed
		if message := data.Error.String(); message != "" {
			tool.Output = strings.TrimSpace(message + "\n" + tool.Output)
		}
	}
	kind := domain.EventToolResult
	if !seen {
		kind = domain.EventToolCall
	}
	event := a.base(t, kind)
	event.ItemID = data.ID
	event.Tool = tool
	a.emit.Emit(event)
}

func (a *V2Adapter) onExecutionEnded(raw json.RawMessage, reason domain.StopReason, failure string) {
	data, t := a.decode(raw)
	if t == nil {
		return
	}
	t.mu.Lock()
	turnID := t.turnID
	t.turnID = ""
	t.toolName = make(map[string]string)
	t.toolSeen = make(map[string]bool)
	t.itemText = make(map[string]string)
	t.mu.Unlock()
	if turnID == "" {
		return
	}

	if failure != "" {
		message := data.Error.String()
		if message == "" {
			message = "OpenCode turn failed"
		}
		logger.Errorf("OpenCode", "v2 turn failed: %s (thread=%s)", message, t.threadID)
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventTurnFailed, ThreadID: t.threadID, TurnID: turnID,
			Driver: domain.DriverOpenCode, Error: message,
		})
		return
	}
	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventTurnCompleted, ThreadID: t.threadID, TurnID: turnID,
		Driver: domain.DriverOpenCode, StopReason: reason,
	})
}

func (a *V2Adapter) onPermissionAsked(raw json.RawMessage) {
	var data v2PermissionAsked
	if json.Unmarshal(raw, &data) != nil || data.ID == "" {
		return
	}
	t := a.bySessionID(data.SessionID)
	if t == nil {
		return
	}
	a.mu.Lock()
	a.pending[data.ID] = data.SessionID
	a.mu.Unlock()

	if t.permission == domain.PermissionBypass {
		go func() {
			if err := a.RespondToApproval(context.Background(), t.threadID, data.ID, domain.ApprovalAllowOnce); err != nil {
				logger.Errorf("OpenCode", "auto-approve of %s failed: %v", data.ID, err)
			}
		}()
		return
	}

	title := data.Message
	if title == "" {
		title = NormalizeToolName(data.Action)
	}
	event := a.base(t, domain.EventApprovalRequest)
	event.Approval = &domain.ApprovalRequest{
		RequestID: data.ID,
		Title:     title,
		Detail:    strings.Join(data.Resources, "\n"),
		Options: []domain.ApprovalOption{
			{ID: "once", Name: "Allow once", Kind: domain.ApprovalAllowOnce},
			{ID: "always", Name: "Always allow", Kind: domain.ApprovalAllowAlways},
			{ID: "reject", Name: "Deny", Kind: domain.ApprovalDeny},
		},
	}
	a.emit.Emit(event)
}

func optionLabel(option v2FormOption) string {
	label := option.Label
	if label == "" {
		label = fmt.Sprint(option.Value)
	}
	if option.Description != "" && option.Description != label {
		label += " — " + option.Description
	}
	return label
}

func (a *V2Adapter) onFormCreated(raw json.RawMessage) {
	var data v2FormCreated
	if json.Unmarshal(raw, &data) != nil || data.Form.ID == "" {
		return
	}
	form := data.Form
	t := a.bySessionID(form.SessionID)
	if t == nil {
		return
	}

	pending := &pendingForm{sessionID: form.SessionID}
	var questions []domain.QuestionItem
	for _, field := range form.Fields {
		if field.Hidden {
			continue
		}
		text := field.Description
		if text == "" {
			text = field.Title
		} else if field.Title != "" && field.Title != text {
			text = field.Title + ": " + text
		}
		if text == "" {
			text = form.Title
		}
		values := make(map[string]any)
		var options []string
		for _, option := range field.Options {
			label := optionLabel(option)
			values[label] = option.Value
			options = append(options, label)
		}
		if field.Type == "boolean" && len(options) == 0 {
			options = []string{"Yes", "No"}
			values["Yes"], values["No"] = true, false
		}
		pending.fields = append(pending.fields, field)
		pending.values = append(pending.values, values)
		questions = append(questions, domain.QuestionItem{Question: text, Options: options})
	}
	if len(questions) == 0 {
		return
	}

	a.mu.Lock()
	a.forms[form.ID] = pending
	a.mu.Unlock()

	event := a.base(t, domain.EventQuestionAsked)
	event.Question = &domain.QuestionRequest{RequestID: form.ID, Questions: questions}
	a.emit.Emit(event)
}

func formValue(field v2FormField, values map[string]any, answer string) (any, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, false
	}
	value, known := values[answer]
	switch field.Type {
	case "boolean":
		if known {
			return value, true
		}
		lower := strings.ToLower(answer)
		return lower == "yes" || lower == "true", true
	case "number", "integer":
		parsed, err := strconv.ParseFloat(answer, 64)
		if err != nil {
			return nil, false
		}
		if field.Type == "integer" {
			return int64(parsed), true
		}
		return parsed, true
	case "multiselect":
		if known {
			return []any{value}, true
		}
		return []string{answer}, true
	default:
		if known {
			return value, true
		}
		return answer, true
	}
}
