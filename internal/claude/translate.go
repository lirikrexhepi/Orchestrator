package claude

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"composer/internal/domain"
)

func (a *Adapter) handleEnvelope(s *session, envelope *Envelope) {
	if envelope.SessionID != "" {
		s.mu.Lock()
		s.sessionID = envelope.SessionID
		s.mu.Unlock()
	}

	if envelope.ParentToolUseID != "" {
		a.handleSubagentFrame(s, envelope)
		return
	}

	switch envelope.Type {
	case "system":
		switch envelope.Subtype {
		case "init":
			s.noteModel(envelope.Model)
			event := a.event(s, domain.EventSessionStarted)
			event.Text = s.providerSessionID()
			a.emit.Emit(event)
		case "task_notification":
			a.handleTaskNotification(s, envelope)
		case "task_progress":
			a.handleTaskProgress(s, envelope)
		}
	case "stream_event":
		a.handleStreamEvent(s, envelope)
	case "assistant":
		a.handleAssistant(s, envelope)
	case "user":
		a.handleToolResults(s, envelope)
	case "result":
		a.handleResult(s, envelope)
	case "rate_limit_event":
		a.handleRateLimit(s, envelope)
	case "control_request":
		a.handleControlRequest(s, envelope)
	}
}

// handleStreamEvent forwards token deltas. The item id is the message id plus
// the content block index, so parallel blocks never merge into one another.
func (a *Adapter) handleStreamEvent(s *session, envelope *Envelope) {
	if len(envelope.Event) == 0 {
		return
	}
	var ev streamEvent
	if json.Unmarshal(envelope.Event, &ev) != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			s.mu.Lock()
			s.currentMsgID = ev.Message.ID
			s.mu.Unlock()
		}
	case "content_block_delta":
		if ev.Delta == nil {
			return
		}
		s.mu.Lock()
		msgID := s.currentMsgID
		s.mu.Unlock()
		var event domain.RuntimeEvent
		switch ev.Delta.Type {
		case "text_delta":
			if ev.Delta.Text == "" {
				return
			}
			event = a.event(s, domain.EventAgentMessage)
			event.Text = ev.Delta.Text
		case "thinking_delta":
			if ev.Delta.Thinking == "" {
				return
			}
			event = a.event(s, domain.EventAgentThought)
			event.Text = ev.Delta.Thinking
		default:
			return
		}
		if msgID != "" {
			s.mu.Lock()
			s.streamedMsgs[msgID] = true
			s.mu.Unlock()
		}
		event.Delta = true
		event.ItemID = msgID + ":" + strconv.Itoa(ev.Index)
		a.emit.Emit(event)
	}
}

// handleRateLimit forwards subscription quota. The CLI reports one window per
// frame, so windows are emitted individually and merged downstream rather than
// assumed to arrive together.
func (a *Adapter) handleRateLimit(s *session, envelope *Envelope) {
	info := envelope.RateLimitInfo
	if info == nil || info.RateLimitType == "" {
		return
	}

	limit := domain.RateLimit{
		Window:      info.RateLimitType,
		Status:      info.Status,
		ResetsAt:    info.ResetsAt,
		UsedPercent: info.UsedPercent,
	}
	// Some builds report headroom instead of consumption; normalise to used.
	if limit.UsedPercent == nil && info.RemainingPct != nil {
		used := 100 - *info.RemainingPct
		limit.UsedPercent = &used
	}

	event := a.event(s, domain.EventRateLimit)
	event.RateLimits = []domain.RateLimit{limit}
	a.emit.Emit(event)
}

func (a *Adapter) event(s *session, kind domain.EventKind) domain.RuntimeEvent {
	s.mu.Lock()
	turnID := s.turnID
	s.mu.Unlock()
	return domain.RuntimeEvent{Kind: kind, ThreadID: s.threadID, TurnID: turnID, Driver: domain.DriverClaude}
}

func (a *Adapter) handleAssistant(s *session, envelope *Envelope) {
	if envelope.Message == nil {
		return
	}
	msgID := envelope.Message.ID
	s.mu.Lock()
	streamed := msgID != "" && s.streamedMsgs[msgID]
	s.mu.Unlock()

	for i, block := range envelope.Message.Content {
		switch block.Type {
		case "text", "thinking":
			// Already delivered token by token through stream_event frames.
			if streamed {
				continue
			}
			text := block.Text
			kind := domain.EventAgentMessage
			if block.Type == "thinking" {
				text, kind = block.Thinking, domain.EventAgentThought
			}
			if text == "" {
				continue
			}
			event := a.event(s, kind)
			event.Text = text
			event.ItemID = firstNonEmpty(envelope.UUID, msgID+":full:"+strconv.Itoa(i))
			a.emit.Emit(event)
		case "tool_use":
			// AskUserQuestion is rendered from its can_use_tool request as an
			// interactive card; the raw tool row would duplicate it.
			if block.Name == "AskUserQuestion" {
				s.mu.Lock()
				s.hiddenTools[block.ID] = true
				s.mu.Unlock()
				continue
			}
			tool := &domain.ToolCall{ID: block.ID, Name: block.Name, Status: domain.ToolInProgress}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &tool.Input)
			}
			event := a.event(s, domain.EventToolCall)
			event.Tool = tool
			event.ItemID = block.ID
			a.emit.Emit(event)
		}
	}

	if envelope.Message.Usage != nil {
		event := a.event(s, domain.EventUsage)
		event.Usage = convertUsage(envelope.Message.Usage, 0)
		u := envelope.Message.Usage
		event.Usage.ContextTokens = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens
		event.Usage.ContextWindow = s.contextLimit()
		a.emit.Emit(event)
	}
}

func (a *Adapter) handleToolResults(s *session, envelope *Envelope) {
	if envelope.Message == nil {
		return
	}
	for _, block := range envelope.Message.Content {
		if block.Type != "tool_result" {
			continue
		}
		s.mu.Lock()
		hidden := s.hiddenTools[block.ToolUseID]
		delete(s.hiddenTools, block.ToolUseID)
		s.mu.Unlock()
		if hidden {
			continue
		}
		status := domain.ToolCompleted
		if block.IsError {
			status = domain.ToolFailed
		}
		event := a.event(s, domain.EventToolResult)
		event.ItemID = block.ToolUseID
		event.Tool = &domain.ToolCall{
			ID:     block.ToolUseID,
			Status: status,
			Output: flattenContent(block.Content),
		}
		applyToolUseResult(event.Tool, envelope.ToolUseResult)
		a.emit.Emit(event)
	}
}

type toolUseResultInfo struct {
	Status            string `json:"status"`
	TotalDurationMs   int64  `json:"totalDurationMs"`
	TotalToolUseCount int    `json:"totalToolUseCount"`
	TotalTokens       int64  `json:"totalTokens"`
}

func applyToolUseResult(tool *domain.ToolCall, raw json.RawMessage) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var info toolUseResultInfo
	if json.Unmarshal(raw, &info) != nil {
		return
	}
	if info.Status == "async_launched" {
		tool.Async = true
		tool.Output = ""
		return
	}
	tool.DurationMs = info.TotalDurationMs
	tool.ToolCount = info.TotalToolUseCount
	tool.Tokens = info.TotalTokens
}

func (a *Adapter) handleTaskProgress(s *session, envelope *Envelope) {
	if envelope.ToolUseID == "" || envelope.Usage == nil {
		return
	}
	event := a.event(s, domain.EventSubagentProgress)
	event.ParentToolID = envelope.ToolUseID
	event.ItemID = envelope.ToolUseID
	event.Tool = &domain.ToolCall{
		ID:         envelope.ToolUseID,
		Status:     domain.ToolInProgress,
		DurationMs: envelope.Usage.DurationMs,
		ToolCount:  envelope.Usage.ToolUses,
		Tokens:     envelope.Usage.TotalTokens,
	}
	a.emit.Emit(event)
}

func (a *Adapter) handleTaskNotification(s *session, envelope *Envelope) {
	if envelope.ToolUseID == "" {
		return
	}
	status := domain.ToolCompleted
	switch envelope.TaskStatus {
	case "failed", "killed", "error", "stopped":
		status = domain.ToolFailed
	}
	tool := &domain.ToolCall{ID: envelope.ToolUseID, Status: status, Output: envelope.TaskSummary}
	if envelope.Usage != nil {
		tool.DurationMs = envelope.Usage.DurationMs
		tool.ToolCount = envelope.Usage.ToolUses
		tool.Tokens = envelope.Usage.TotalTokens
	}
	event := a.event(s, domain.EventToolResult)
	event.ItemID = envelope.ToolUseID
	event.Tool = tool
	a.emit.Emit(event)
}

// flattenContent renders a tool_result payload, which is either a bare string
// or an array of content blocks depending on the tool.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}

	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw)
	}
	var builder strings.Builder
	for _, block := range blocks {
		builder.WriteString(block.Text)
	}
	return builder.String()
}

func (a *Adapter) handleResult(s *session, envelope *Envelope) {
	s.mu.Lock()
	turnID := s.turnID
	interrupted := s.interrupting
	s.turnID = ""
	s.interrupting = false
	s.streamedMsgs = make(map[string]bool)
	s.currentMsgID = ""
	s.mu.Unlock()

	for _, model := range envelope.ModelUsage {
		s.noteContextWindow(model.ContextWindow)
	}
	if envelope.Usage != nil {
		usage := convertUsage(envelope.Usage, envelope.TotalCost)
		usage.ContextWindow = s.contextLimit()
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventUsage, ThreadID: s.threadID, TurnID: turnID, Driver: domain.DriverClaude,
			Usage: usage,
		})
	}

	event := domain.RuntimeEvent{ThreadID: s.threadID, TurnID: turnID, Driver: domain.DriverClaude}
	if interrupted {
		event.Kind = domain.EventTurnCompleted
		event.StopReason = domain.StopCancelled
	} else if envelope.IsError {
		event.Kind = domain.EventTurnFailed
		event.Error = envelope.Result
		if event.Error == "" {
			event.Error = envelope.Subtype
		}
	} else {
		event.Kind = domain.EventTurnCompleted
		event.StopReason = mapStopReason(envelope.StopReason, envelope.Subtype)
	}
	a.emit.Emit(event)
}

func mapStopReason(stopReason, subtype string) domain.StopReason {
	switch stopReason {
	case "max_tokens":
		return domain.StopMaxTokens
	case "refusal":
		return domain.StopRefusal
	}
	switch subtype {
	case "error_max_turns", "error_during_execution":
		return domain.StopError
	}
	return domain.StopEndTurn
}

func convertUsage(usage *Usage, cost float64) *domain.Usage {
	return &domain.Usage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadInputTokens,
		CacheWriteTokens: usage.CacheCreationInputTokens,
		CostUSD:          cost,
	}
}

func (a *Adapter) handleControlRequest(s *session, envelope *Envelope) {
	reqID := envelope.RequestID
	payload := envelope.Request
	if reqID == "" || payload == nil {
		return
	}
	if payload.Subtype != "can_use_tool" {
		// No hooks, MCP bridges or dialogs are registered, so these should not
		// arrive; answering keeps the CLI from waiting forever if they do.
		_ = s.write(ControlResponse{Type: "control_response", Response: ControlResponseBody{
			Subtype: "error", RequestID: reqID, Error: "unsupported control request: " + payload.Subtype,
		}})
		return
	}

	var input map[string]any
	if len(payload.Input) > 0 {
		_ = json.Unmarshal(payload.Input, &input)
	}
	pending := &pendingControl{
		toolName: payload.ToolName, toolUseID: payload.ToolUseID,
		input: input, suggestions: payload.PermissionSuggestions,
	}
	s.mu.Lock()
	s.pending[reqID] = pending
	perm := s.permission
	s.mu.Unlock()

	if payload.ToolName == "AskUserQuestion" {
		event := a.event(s, domain.EventQuestionAsked)
		event.Question = parseClaudeQuestion(reqID, input)
		event.ItemID = reqID
		a.emit.Emit(event)
		return
	}

	// Bypass sessions approve everything except interactive tools above.
	if perm == domain.PermissionBypass {
		go func() {
			_ = a.RespondToApproval(context.Background(), s.threadID, reqID, domain.ApprovalAllowOnce)
		}()
		return
	}

	title := firstNonEmpty(payload.ToolName, "Permission Request")
	detail := approvalDetail(payload.ToolName, input)
	if payload.DecisionReason != "" {
		detail = strings.TrimSpace(detail + "\n" + payload.DecisionReason)
	}

	event := a.event(s, domain.EventApprovalRequest)
	event.ItemID = reqID
	event.Approval = &domain.ApprovalRequest{
		RequestID: reqID,
		Title:     title,
		Detail:    detail,
		Tool:      &domain.ToolCall{ID: payload.ToolUseID, Name: payload.ToolName, Status: domain.ToolPending, Input: input},
		Options: []domain.ApprovalOption{
			{ID: "once", Name: "Allow once", Kind: domain.ApprovalAllowOnce},
			{ID: "always", Name: "Always allow", Kind: domain.ApprovalAllowAlways},
			{ID: "reject", Name: "Deny", Kind: domain.ApprovalDeny},
		},
	}
	a.emit.Emit(event)
}

// approvalDetail picks the one field a user needs to judge the request.
func approvalDetail(tool string, input map[string]any) string {
	for _, key := range []string{"command", "file_path", "path", "url", "pattern", "plan"} {
		if value, ok := input[key].(string); ok && value != "" {
			return value
		}
	}
	if len(input) == 0 {
		return ""
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	if len(encoded) > 600 {
		return string(encoded[:600]) + "…"
	}
	return string(encoded)
}

func parseClaudeQuestion(requestID string, input map[string]any) *domain.QuestionRequest {
	req := &domain.QuestionRequest{RequestID: requestID}
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
			if opts, ok := m["options"].([]any); ok {
				item.Options = formatClaudeOptions(opts)
			}
			if item.Question != "" {
				req.Questions = append(req.Questions, item)
			}
		}
		if len(req.Questions) > 0 {
			return req
		}
	}
	item := domain.QuestionItem{}
	if q, ok := input["question"].(string); ok && q != "" {
		item.Question = q
	} else if h, ok := input["header"].(string); ok && h != "" {
		item.Question = h
	}
	if opts, ok := input["options"].([]any); ok {
		item.Options = formatClaudeOptions(opts)
	}
	if item.Question != "" {
		req.Questions = []domain.QuestionItem{item}
	}
	return req
}

func formatClaudeOptions(opts []any) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		switch v := o.(type) {
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

func (a *Adapter) handleSubagentFrame(s *session, envelope *Envelope) {
	if envelope.Message == nil {
		return
	}
	parent := envelope.ParentToolUseID
	switch envelope.Type {
	case "assistant":
		msgID := envelope.Message.ID
		for i, block := range envelope.Message.Content {
			switch block.Type {
			case "text":
				if block.Text == "" {
					continue
				}
				event := a.event(s, domain.EventSubagentMessage)
				event.ParentToolID = parent
				event.Text = block.Text
				event.ItemID = firstNonEmpty(envelope.UUID, msgID+":full:"+strconv.Itoa(i))
				a.emit.Emit(event)
			case "tool_use":
				tool := &domain.ToolCall{ID: block.ID, Name: block.Name, Status: domain.ToolInProgress}
				if len(block.Input) > 0 {
					_ = json.Unmarshal(block.Input, &tool.Input)
				}
				event := a.event(s, domain.EventSubagentToolCall)
				event.ParentToolID = parent
				event.Tool = tool
				event.ItemID = block.ID
				a.emit.Emit(event)
			}
		}
	case "user":
		for _, block := range envelope.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			status := domain.ToolCompleted
			if block.IsError {
				status = domain.ToolFailed
			}
			event := a.event(s, domain.EventSubagentToolDone)
			event.ParentToolID = parent
			event.ItemID = block.ToolUseID
			event.Tool = &domain.ToolCall{ID: block.ToolUseID, Status: status, Output: flattenContent(block.Content)}
			a.emit.Emit(event)
		}
	}
}
