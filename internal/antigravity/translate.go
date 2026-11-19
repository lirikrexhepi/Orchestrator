package antigravity

import (
	"context"
	"encoding/json"
	"strings"

	"composer/internal/domain"
	"composer/internal/logger"
)

// handleEnvelope converts one agy frame into canonical events.
func (a *Adapter) handleEnvelope(s *session, turnID string, envelope *Envelope) {
	if envelope.ConversationID != "" {
		s.mu.Lock()
		isNew := s.conversationID != envelope.ConversationID
		s.conversationID = envelope.ConversationID
		s.mu.Unlock()
		if isNew && envelope.Event == "init" {
			a.emit.Emit(domain.RuntimeEvent{
				Kind: domain.EventSessionStarted, ThreadID: s.threadID, TurnID: turnID,
				Driver: domain.DriverAntigravity, Text: envelope.ConversationID,
			})
		}
	}

	switch envelope.Event {
	case "step_update":
		a.handleStep(s, turnID, envelope.StepUpdate)
	case "result":
		a.handleResult(s, turnID, envelope.Result)
	case "init", "":
		// Handled above (conversation tracking) or empty heartbeat.
	default:
		logger.Debugf("Antigravity", "ignoring envelope event %q", envelope.Event)
	}
}

func (a *Adapter) event(s *session, turnID string, kind domain.EventKind) domain.RuntimeEvent {
	return domain.RuntimeEvent{
		Kind: kind, ThreadID: s.threadID, TurnID: turnID, Driver: domain.DriverAntigravity,
	}
}

func (a *Adapter) handleStep(s *session, turnID string, step *StepUpdate) {
	if step == nil {
		return
	}

	switch step.StepType {
	case "agent_response":
		if step.TextDelta != "" {
			s.markText(step.TextDelta)
			event := a.event(s, turnID, domain.EventAgentMessage)
			event.Text = step.TextDelta
			event.Delta = true
			event.ItemID = stepItem(turnID, step)
			a.emit.Emit(event)
		}
	case "thinking", "reasoning":
		if step.TextDelta != "" {
			event := a.event(s, turnID, domain.EventAgentThought)
			event.Text = step.TextDelta
			event.Delta = true
			event.ItemID = stepItem(turnID, step)
			a.emit.Emit(event)
		}
	case "tool", "tool_call", "tool_result", "function_call":
		a.handleTool(s, turnID, step)
	default:
		// Newer agy versions may introduce step types this build does not know.
		// A step carrying tool payload is still a tool; any other text the CLI
		// streams is user-visible narration and must not be silently dropped.
		if step.ToolName != "" || step.ToolInfo != nil {
			a.handleTool(s, turnID, step)
		} else if step.TextDelta != "" {
			logger.Debugf("Antigravity", "unknown step_type %q, keeping text delta (%d chars)", step.StepType, len(step.TextDelta))
			s.markText(step.TextDelta)
			event := a.event(s, turnID, domain.EventAgentMessage)
			event.Text = step.TextDelta
			event.Delta = true
			event.ItemID = stepItem(turnID, step)
			a.emit.Emit(event)
		} else {
			logger.Debugf("Antigravity", "ignoring step_type %q with no text or tool payload", step.StepType)
		}
	}

	if step.Usage != nil {
		event := a.event(s, turnID, domain.EventUsage)
		event.Usage = convertUsage(step.Usage)
		event.Usage.ContextTokens = step.Usage.TotalTokens
		if event.Usage.ContextTokens == 0 {
			event.Usage.ContextTokens = step.Usage.InputTokens + step.Usage.OutputTokens
		}
		a.emit.Emit(event)
	}
}

// handleTool emits a call on the first sighting of a step index and a result on
// its terminal update, so each invocation produces one lifecycle.
func (a *Adapter) handleTool(s *session, turnID string, step *StepUpdate) {
	s.mu.Lock()
	seen := s.tools[step.StepIndex]
	s.tools[step.StepIndex] = step.State
	s.mu.Unlock()

	tool := &domain.ToolCall{
		ID:     toolID(step),
		Name:   step.ToolName,
		Status: mapState(step.State),
	}
	if step.ToolInfo != nil {
		if len(step.ToolInfo.Parameters) > 0 {
			_ = json.Unmarshal(step.ToolInfo.Parameters, &tool.Input)
		}
		tool.Output = step.ToolInfo.Output
		if message := step.ToolInfo.Error.Text(); message != "" {
			tool.Output = message
			tool.Status = domain.ToolFailed
		}
	}

	if hiddenTools[step.ToolName] {
		return
	}
	path := toolPath(tool.Input)
	if isEditTool(step.ToolName) {
		if seen == "" {
			s.captureBaseline(path)
		}
		if tool.Status == domain.ToolCompleted {
			tool.Diffs = s.editDiff(path)
		}
	} else if isReadTool(step.ToolName) && tool.Status == domain.ToolCompleted {
		s.rememberFile(path)
	}

	if step.ToolName == "ask_permission" || step.ToolName == "ask_custom_permission" {
		title := "Permission Request"
		detail := ""
		if tool.Input != nil {
			if action, ok := tool.Input["action"].(string); ok && action != "" {
				title = action
			}
			if target, ok := tool.Input["target"].(string); ok && target != "" {
				detail = target
			}
			if reason, ok := tool.Input["reason"].(string); ok && reason != "" {
				if detail != "" {
					detail += " — " + reason
				} else {
					detail = reason
				}
			}
		}

		s.mu.Lock()
		perm := s.permission
		s.mu.Unlock()

		if perm == domain.PermissionBypass {
			go func() {
				_ = a.RespondToApproval(context.Background(), s.threadID, tool.ID, domain.ApprovalAllowAlways)
			}()
			return
		}

		if seen == "" {
			event := a.event(s, turnID, domain.EventApprovalRequest)
			event.Approval = &domain.ApprovalRequest{
				RequestID: tool.ID,
				Title:     title,
				Detail:    detail,
				Options: []domain.ApprovalOption{
					{ID: "once", Name: "Allow once", Kind: domain.ApprovalAllowOnce},
					{ID: "always", Name: "Always allow", Kind: domain.ApprovalAllowAlways},
					{ID: "reject", Name: "Deny", Kind: domain.ApprovalDeny},
				},
			}
			a.emit.Emit(event)
		}
		return
	}

	if step.ToolName == "ask_question" {
		if seen == "" {
			event := a.event(s, turnID, domain.EventQuestionAsked)
			event.Question = parseQuestionInput(tool.ID, tool.Input)
			event.Tool = tool
			a.emit.Emit(event)
		}
		// Always return early: the question.asked event is the canonical source.
		// Falling through would emit a duplicate tool.call/tool.result block.
		return
	}

	kind := domain.EventToolResult
	if seen == "" {
		kind = domain.EventToolCall
	}
	event := a.event(s, turnID, kind)
	event.Tool = tool
	event.ItemID = tool.ID
	a.emit.Emit(event)
}

// stepItem keys streamed text by turn and step, so text from separate steps
// (narration before and after a tool) lands in separate blocks.
func stepItem(turnID string, step *StepUpdate) string {
	return turnID + ":step:" + itoa(step.StepIndex)
}

func toolID(step *StepUpdate) string {
	return step.ToolName + ":" + itoa(step.StepIndex)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func mapState(state string) domain.ToolStatus {
	switch state {
	case StateActive:
		return domain.ToolInProgress
	case StateDone:
		return domain.ToolCompleted
	case StateError:
		return domain.ToolFailed
	default:
		return domain.ToolPending
	}
}

func (a *Adapter) handleResult(s *session, turnID string, result *Result) {
	if result == nil {
		a.emit.Emit(a.event(s, turnID, domain.EventTurnCompleted))
		return
	}

	if result.Usage != nil {
		event := a.event(s, turnID, domain.EventUsage)
		event.Usage = convertUsage(result.Usage)
		a.emit.Emit(event)
	}

	// Some models stream only progress narration as deltas and deliver the
	// actual answer in the terminal response frame. Emit it unless it clearly
	// duplicates what already streamed (either side containing the other).
	if resp := strings.TrimSpace(result.Response); resp != "" {
		streamed, cut, sent := s.streamedText()
		duplicate := sent && (streamed == "" ||
			(!cut && (strings.Contains(streamed, resp) || strings.Contains(resp, streamed))))
		if !duplicate {
			message := a.event(s, turnID, domain.EventAgentMessage)
			message.ItemID = turnID + ":response"
			message.Text = result.Response
			a.emit.Emit(message)
		}
	}

	if result.Status != "SUCCESS" {
		event := a.event(s, turnID, domain.EventTurnFailed)
		event.Error = firstNonEmpty(result.Error, result.Status)
		a.emit.Emit(event)
		return
	}

	event := a.event(s, turnID, domain.EventTurnCompleted)
	event.StopReason = domain.StopEndTurn
	a.emit.Emit(event)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func convertUsage(usage *Usage) *domain.Usage {
	return &domain.Usage{
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.CacheReadTokens,
	}
}

// parseQuestionInput extracts structured question items from the ask_question
// tool input. The input format is: { "questions": [{ "question": "...",
// "options": ["...", ...], "is_multi_select": bool }] }
func parseQuestionInput(requestID string, input map[string]any) *domain.QuestionRequest {
	req := &domain.QuestionRequest{RequestID: requestID}
	raw, ok := input["questions"]
	if !ok {
		if q, ok := input["question"].(string); ok && q != "" {
			item := domain.QuestionItem{Question: q}
			if opts, ok := input["options"].([]any); ok {
				item.Options = formatAntigravityOptions(opts)
			}
			req.Questions = []domain.QuestionItem{item}
		} else if h, ok := input["header"].(string); ok && h != "" {
			item := domain.QuestionItem{Question: h}
			if opts, ok := input["options"].([]any); ok {
				item.Options = formatAntigravityOptions(opts)
			}
			req.Questions = []domain.QuestionItem{item}
		}
		return req
	}
	questions, ok := raw.([]any)
	if !ok {
		return req
	}
	for _, entry := range questions {
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
		if opts, ok := m["options"].([]any); ok {
			item.Options = formatAntigravityOptions(opts)
		}
		if ms, ok := m["is_multi_select"].(bool); ok {
			item.MultiSelect = ms
		}
		if item.Question != "" {
			req.Questions = append(req.Questions, item)
		}
	}
	return req
}

func formatAntigravityOptions(opts []any) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		if s, ok := o.(string); ok {
			if s != "" {
				out = append(out, s)
			}
			continue
		}
		if v, ok := o.(map[string]any); ok {
			label := ""
			if s, ok := v["label"].(string); ok && s != "" {
				label = s
			} else if s, ok := v["value"].(string); ok && s != "" {
				label = s
			}
			if d, ok := v["description"].(string); ok && d != "" {
				if label != "" {
					label = label + " - " + d
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
