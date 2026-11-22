package codex

import (
	"context"
	"encoding/json"
	"strings"

	"composer/internal/domain"
)

func (a *Adapter) handleInbound(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "item/agentMessage/delta":
		a.onAgentDelta(params)
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		a.onReasoningDelta(params)
	case "item/started":
		a.onItem(params, false)
	case "item/completed":
		a.onItem(params, true)
	case "item/commandExecution/outputDelta":
		a.onCommandOutput(params)
	case "turn/completed":
		a.onTurnCompleted(params)
	case "turn/plan/updated":
		a.onPlan(params)
	case "thread/tokenUsage/updated":
		a.onUsage(params)
	case "item/commandExecution/requestApproval",
		"item/fileChange/requestApproval",
		"item/permissions/requestApproval":
		return a.onApprovalRequest(ctx, method, params)
	}
	return nil, nil
}

func (a *Adapter) emitFor(codexThreadID, turnID string, kind domain.EventKind) (domain.RuntimeEvent, *thread) {
	t := a.lookupByCodexID(codexThreadID)
	if t == nil {
		return domain.RuntimeEvent{}, nil
	}
	t.mu.Lock()
	localTurn := t.turnID
	t.mu.Unlock()
	if localTurn == "" {
		localTurn = turnID
	}
	return domain.RuntimeEvent{
		Kind: kind, ThreadID: t.threadID, TurnID: localTurn, Driver: domain.DriverCodex,
	}, t
}

func (t *thread) markStreamed(itemID string) {
	if itemID == "" {
		return
	}
	t.mu.Lock()
	if t.streamedItems == nil {
		t.streamedItems = make(map[string]bool)
	}
	t.streamedItems[itemID] = true
	t.mu.Unlock()
}

func (t *thread) isStreamed(itemID string) bool {
	if itemID == "" {
		return false
	}
	t.mu.Lock()
	seen := t.streamedItems[itemID]
	t.mu.Unlock()
	return seen
}

func (a *Adapter) onAgentDelta(params json.RawMessage) {
	notification, ok := decode[AgentMessageDelta](params)
	if !ok || notification.Delta == "" {
		return
	}
	event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventAgentMessage)
	if t == nil {
		return
	}
	t.markStreamed(notification.ItemID)
	event.ItemID = notification.ItemID
	event.Text = notification.Delta
	event.Delta = true
	a.emit.Emit(event)
}

func (a *Adapter) onReasoningDelta(params json.RawMessage) {
	notification, ok := decode[AgentMessageDelta](params)
	if !ok || notification.Delta == "" {
		return
	}
	event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventAgentThought)
	if t == nil {
		return
	}
	t.markStreamed(notification.ItemID)
	event.ItemID = notification.ItemID
	event.Text = notification.Delta
	event.Delta = true
	a.emit.Emit(event)
}

func (a *Adapter) onCommandOutput(params json.RawMessage) {
	notification, ok := decode[CommandOutputDelta](params)
	if !ok {
		return
	}
	chunk := notification.Chunk
	if chunk == "" {
		chunk = notification.Delta
	}
	if chunk == "" {
		return
	}
	event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventToolResult)
	if t == nil {
		return
	}
	event.Tool = &domain.ToolCall{ID: notification.ItemID, Status: domain.ToolInProgress, Output: chunk}
	event.Delta = true
	a.emit.Emit(event)
}

// onItem converts a thread item into either a message, a thought, or a tool
// call. `completed` distinguishes the terminal notification from the opening
// one so tool status reflects reality.
func (a *Adapter) onItem(params json.RawMessage, completed bool) {
	notification, ok := decode[ItemNotification](params)
	if !ok {
		return
	}
	item := notification.Item

	switch item.Type {
	case "agentMessage":
		if !completed || item.Text == "" {
			return
		}
		event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventAgentMessage)
		if t == nil || t.isStreamed(item.ID) {
			return
		}
		event.ItemID = item.ID
		event.Text = item.Text
		a.emit.Emit(event)

	case "reasoning":
		if !completed {
			return
		}
		text := strings.Join(append(append([]string{}, item.Summary...), item.Content...), "\n")
		if text == "" {
			return
		}
		event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventAgentThought)
		if t == nil || t.isStreamed(item.ID) {
			return
		}
		event.ItemID = item.ID
		event.Text = text
		a.emit.Emit(event)

	case "commandExecution", "fileChange", "mcpToolCall", "webSearch", "dynamicToolCall":
		kind := domain.EventToolCall
		if completed {
			kind = domain.EventToolResult
		}
		event, t := a.emitFor(notification.ThreadID, notification.TurnID, kind)
		if t == nil {
			return
		}
		event.Tool = convertItem(item, completed)
		a.emit.Emit(event)
	}
}

func convertItem(item ThreadItem, completed bool) *domain.ToolCall {
	tool := &domain.ToolCall{ID: item.ID, Kind: item.Type, Status: domain.ToolInProgress}
	if completed {
		tool.Status = domain.ToolCompleted
	}

	switch item.Type {
	case "commandExecution":
		tool.Name = item.Command
		tool.Output = item.AggregatedOutput
		if item.ExitCode != nil && *item.ExitCode != 0 {
			tool.Status = domain.ToolFailed
		}
	case "fileChange":
		tool.Name = "edit"
		for _, change := range item.Changes {
			tool.Diffs = append(tool.Diffs, domain.FileDiff{
				Path: change.Path, OldText: change.OldText, NewText: firstNonEmpty(change.NewText, change.Diff),
			})
		}
		if item.Status == "failed" {
			tool.Status = domain.ToolFailed
		}
	case "mcpToolCall":
		tool.Name = strings.TrimPrefix(item.Server+"."+item.Name, ".")
		if len(item.Arguments) > 0 {
			_ = json.Unmarshal(item.Arguments, &tool.Input)
		}
		if len(item.Result) > 0 {
			tool.Output = string(item.Result)
		}
		if len(item.Error) > 0 {
			tool.Status = domain.ToolFailed
			tool.Output = string(item.Error)
		}
	default:
		tool.Name = firstNonEmpty(item.Name, item.Type)
	}
	return tool
}

func (a *Adapter) onPlan(params json.RawMessage) {
	notification, ok := decode[PlanUpdatedNotification](params)
	if !ok {
		return
	}
	steps := notification.Plan
	if len(steps) == 0 {
		steps = notification.Steps
	}
	if len(steps) == 0 {
		return
	}

	event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventPlan)
	if t == nil {
		return
	}
	entries := make([]domain.PlanEntry, 0, len(steps))
	for _, step := range steps {
		entries = append(entries, domain.PlanEntry{
			Content: firstNonEmpty(step.Step, step.Text), Status: step.Status,
		})
	}
	event.Plan = entries
	a.emit.Emit(event)
}

func (a *Adapter) onUsage(params json.RawMessage) {
	notification, ok := decode[TokenUsageNotification](params)
	if !ok {
		return
	}
	event, t := a.emitFor(notification.ThreadID, notification.TurnID, domain.EventUsage)
	if t == nil {
		return
	}

	var breakdown *TokenBreakdown
	var ctxWindow int64
	if notification.TokenUsage != nil {
		breakdown = notification.TokenUsage.Last
		if breakdown == nil {
			breakdown = notification.TokenUsage.Total
		}
		ctxWindow = notification.TokenUsage.ModelContextWindow
		if ctxWindow == 0 && breakdown != nil {
			ctxWindow = breakdown.ContextWindow
		}
	}
	if breakdown == nil {
		breakdown = &notification.Usage
		ctxWindow = notification.Usage.ContextWindow
	}

	contextTokens := breakdown.TotalTokens
	if contextTokens == 0 {
		contextTokens = breakdown.InputTokens + breakdown.OutputTokens
	}

	event.Usage = &domain.Usage{
		InputTokens:     breakdown.InputTokens,
		OutputTokens:    breakdown.OutputTokens,
		CacheReadTokens: breakdown.CachedInputTokens,
		ContextTokens:   contextTokens,
		ContextWindow:   ctxWindow,
	}
	a.emit.Emit(event)
}

func (a *Adapter) onTurnCompleted(params json.RawMessage) {
	notification, ok := decode[TurnCompletedNotification](params)
	if !ok {
		return
	}
	t := a.lookupByCodexID(notification.ThreadID)
	if t == nil {
		return
	}

	t.mu.Lock()
	turnID := t.turnID
	t.turnID = ""
	t.streamedItems = make(map[string]bool)
	t.mu.Unlock()

	a.mu.Lock()
	delete(a.codexTurns, t.threadID)
	a.mu.Unlock()

	event := domain.RuntimeEvent{ThreadID: t.threadID, TurnID: turnID, Driver: domain.DriverCodex}
	switch notification.Turn.Status {
	case "failed":
		event.Kind = domain.EventTurnFailed
		if notification.Turn.Error != nil {
			event.Error = notification.Turn.Error.Message
		}
	case "interrupted":
		event.Kind = domain.EventTurnCompleted
		event.StopReason = domain.StopCancelled
	default:
		event.Kind = domain.EventTurnCompleted
		event.StopReason = domain.StopEndTurn
	}
	a.emit.Emit(event)
}

func approvalReply(method string, request ApprovalRequest, decision string) any {
	if method == "item/permissions/requestApproval" {
		switch decision {
		case DecisionApproved:
			return PermissionsApprovalResponse{Permissions: nonNilMap(request.Permissions), Scope: "turn"}
		case DecisionApprovedForSession:
			return PermissionsApprovalResponse{Permissions: nonNilMap(request.Permissions), Scope: "session"}
		default:
			return PermissionsApprovalResponse{Permissions: map[string]any{}, Scope: "turn"}
		}
	}
	return ApprovalResponse{Decision: decision}
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// onApprovalRequest blocks the app-server until the UI answers, mirroring the
// ACP permission flow.
func (a *Adapter) onApprovalRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	request, ok := decode[ApprovalRequest](params)
	if !ok {
		return approvalReply(method, ApprovalRequest{}, DecisionDenied), nil
	}
	t := a.lookupByCodexID(request.ThreadID)
	if t == nil {
		return approvalReply(method, request, DecisionDenied), nil
	}

	requestID := firstNonEmpty(request.CallID, request.ItemID, request.TurnID)
	reply := make(chan string, 1)

	a.mu.Lock()
	a.pending[requestID] = reply
	a.mu.Unlock()

	t.mu.Lock()
	turnID := t.turnID
	t.mu.Unlock()

	tool := &domain.ToolCall{ID: requestID, Name: request.Command, Status: domain.ToolPending}
	for _, change := range request.Changes {
		tool.Diffs = append(tool.Diffs, domain.FileDiff{
			Path: change.Path, OldText: change.OldText, NewText: firstNonEmpty(change.NewText, change.Diff),
		})
	}
	if request.Command != "" {
		tool.Kind = "commandExecution"
	} else if len(request.Changes) > 0 {
		tool.Kind = "fileChange"
	} else if method == "item/permissions/requestApproval" {
		tool.Kind = "permissions"
		tool.Input = request.Permissions
	}

	fallbackTitle := "Approve file changes"
	if method == "item/permissions/requestApproval" {
		fallbackTitle = "Approve permissions"
	}

	a.emit.Emit(domain.RuntimeEvent{
		Kind: domain.EventApprovalRequest, ThreadID: t.threadID, TurnID: turnID, Driver: domain.DriverCodex,
		Approval: &domain.ApprovalRequest{
			RequestID: requestID,
			Title:     firstNonEmpty(request.Command, fallbackTitle),
			Detail:    request.Reason,
			Tool:      tool,
			Options: []domain.ApprovalOption{
				{ID: DecisionApproved, Name: "Allow once", Kind: domain.ApprovalAllowOnce},
				{ID: DecisionApprovedForSession, Name: "Allow for session", Kind: domain.ApprovalAllowAlways},
				{ID: DecisionDenied, Name: "Deny", Kind: domain.ApprovalDeny},
			},
		},
	})

	select {
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, requestID)
		a.mu.Unlock()
		return approvalReply(method, request, DecisionAbort), nil
	case decision := <-reply:
		a.emit.Emit(domain.RuntimeEvent{
			Kind: domain.EventApprovalResolved, ThreadID: t.threadID, TurnID: turnID,
			Driver: domain.DriverCodex, Text: decision,
			Approval: &domain.ApprovalRequest{RequestID: requestID},
		})
		return approvalReply(method, request, decision), nil
	}
}
