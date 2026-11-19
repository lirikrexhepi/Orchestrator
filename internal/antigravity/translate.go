package antigravity

import (
	"encoding/json"
	"fmt"
	"strings"

	"composer/internal/domain"
	"composer/internal/logger"
)

func (a *Adapter) event(s *session, turnID string, kind domain.EventKind) domain.RuntimeEvent {
	return domain.RuntimeEvent{
		Kind:     kind,
		ThreadID: s.threadID,
		TurnID:   turnID,
		Driver:   domain.DriverAntigravity,
	}
}

func (a *Adapter) handleSessionUpdate(s *session, turnID string, update *acpSessionUpdate) {
	if update == nil {
		return
	}

	switch update.SessionUpdate {
	case "agent_message_chunk":
		a.handleAgentMessageChunk(s, turnID, update)
	case "agent_thought_chunk":
		a.handleAgentThoughtChunk(s, turnID, update)
	case "tool_call", "tool_call_update":
		a.handleToolCall(s, turnID, update)
	case "usage_update":
		a.handleUsageUpdate(s, turnID, update)
	case "plan":
		a.handlePlan(s, turnID, update)
	case "session_info_update", "available_commands_update", "config_option_update", "current_mode_update":
		// Informational notifications from ACP; ignore quietly.
	default:
		logger.Debugf("Antigravity", "unhandled session update %q", update.SessionUpdate)
	}
}

func (a *Adapter) handleAgentMessageChunk(s *session, turnID string, update *acpSessionUpdate) {
	var chunk acpTextContent
	if err := json.Unmarshal(update.Content, &chunk); err != nil {
		return
	}
	text := chunk.Text
	if text == "" {
		return
	}

	s.mu.Lock()
	if s.activeKind != "message" {
		s.segmentSeq++
		s.activeKind = "message"
	}
	itemID := fmt.Sprintf("%s:msg:%d", turnID, s.segmentSeq)
	s.mu.Unlock()

	event := a.event(s, turnID, domain.EventAgentMessage)
	event.Text = text
	event.Delta = true
	event.ItemID = itemID
	a.emit.Emit(event)
}

func (a *Adapter) handleAgentThoughtChunk(s *session, turnID string, update *acpSessionUpdate) {
	var chunk acpTextContent
	if err := json.Unmarshal(update.Content, &chunk); err != nil {
		return
	}
	text := chunk.Text
	if text == "" {
		return
	}

	s.mu.Lock()
	if s.activeKind != "thought" {
		s.segmentSeq++
		s.activeKind = "thought"
	}
	itemID := fmt.Sprintf("%s:thought:%d", turnID, s.segmentSeq)
	s.mu.Unlock()

	event := a.event(s, turnID, domain.EventAgentThought)
	event.Text = text
	event.Delta = true
	event.ItemID = itemID
	a.emit.Emit(event)
}

func (a *Adapter) handleToolCall(s *session, turnID string, update *acpSessionUpdate) {
	s.mu.Lock()
	s.activeKind = ""
	s.mu.Unlock()

	if hiddenTools[update.Name] {
		return
	}

	toolID := update.ToolCallID
	if toolID == "" {
		toolID = update.Name
	}

	tool := &domain.ToolCall{
		ID:      toolID,
		Name:    update.Name,
		Kind:    update.Kind,
		Summary: strings.TrimSpace(update.Title),
		Status:  mapToolStatus(update.Status),
	}

	input := make(map[string]any)
	if len(update.RawInput) > 0 {
		for k, v := range update.RawInput {
			input[k] = v
		}
	} else if len(update.Args) > 0 {
		for k, v := range update.Args {
			input[k] = v
		}
	}

	for _, candidate := range toolPathCandidates {
		for k, v := range input {
			if strings.EqualFold(k, candidate) {
				if str, ok := v.(string); ok && str != "" {
					input[k] = normalizeACPPath(str)
				}
			}
		}
	}

	if toolPath(input) == "" && len(update.Locations) > 0 && update.Locations[0].Path != "" {
		input["path"] = normalizeACPPath(update.Locations[0].Path)
	}
	tool.Input = input
	path := toolPath(input)

	tool.Output = extractRawOutput(update.RawOutput)

	if len(update.Content) > 0 {
		var items []acpToolContentItem
		if err := json.Unmarshal(update.Content, &items); err == nil {
			for _, item := range items {
				if item.Type == "content" && item.Content != nil && item.Content.Text != "" {
					if tool.Output != "" {
						tool.Output += "\n" + item.Content.Text
					} else {
						tool.Output = item.Content.Text
					}
				} else if item.Type == "diff" {
					tool.Diffs = append(tool.Diffs, domain.FileDiff{
						Path:    normalizeACPPath(item.Path),
						OldText: item.OldText,
						NewText: item.NewText,
					})
				}
			}
		}
	}

	s.mu.Lock()
	seen := s.tools[toolID]
	s.tools[toolID] = true
	s.mu.Unlock()

	if len(tool.Diffs) == 0 && (isEditTool(tool.Name) || update.Kind == "edit") {
		if !seen {
			s.captureBaseline(path)
		}
		if tool.Status == domain.ToolCompleted {
			tool.Diffs = s.editDiff(path)
		}
	} else if isReadTool(tool.Name) && tool.Status == domain.ToolCompleted {
		s.rememberFile(path)
	}

	kind := domain.EventToolResult
	if !seen {
		kind = domain.EventToolCall
	}
	event := a.event(s, turnID, kind)
	event.Tool = tool
	event.ItemID = tool.ID
	a.emit.Emit(event)
}

func (a *Adapter) handleUsageUpdate(s *session, turnID string, update *acpSessionUpdate) {
	if update.Size > 0 {
		s.mu.Lock()
		s.contextWindow = update.Size
		s.mu.Unlock()
	}
	event := a.event(s, turnID, domain.EventUsage)
	event.Usage = &domain.Usage{
		ContextTokens: update.Used,
		ContextWindow: update.Size,
	}
	a.emit.Emit(event)
}

func (a *Adapter) handlePlan(s *session, turnID string, update *acpSessionUpdate) {
	event := a.event(s, turnID, domain.EventPlan)
	event.Plan = update.Entries
	a.emit.Emit(event)
}
