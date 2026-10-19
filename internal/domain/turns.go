package domain

import "strings"

func isSubagentEvent(kind EventKind) bool {
	return strings.HasPrefix(string(kind), "subagent.")
}

func keptAcrossTurns(kind EventKind) bool {
	switch kind {
	case EventNotice, EventSessionStarted, EventSessionStopped, EventProviderStatus:
		return true
	}
	return isSubagentEvent(kind)
}

func ExchangeMask(events []RuntimeEvent, turnID string) []bool {
	drop := make([]bool, len(events))
	if turnID == "" {
		return drop
	}
	start := -1
	for i, event := range events {
		if event.Kind == EventUserMessage && event.TurnID == turnID {
			start = i
			break
		}
	}
	end := len(events)
	if start >= 0 {
		for i := start + 1; i < len(events); i++ {
			if events[i].Kind == EventUserMessage && events[i].TurnID != turnID {
				end = i
				break
			}
		}
	}

	tools := make(map[string]bool)
	for i, event := range events {
		inExchange := start >= 0 && i >= start && i < end
		switch {
		case event.TurnID == turnID:
			drop[i] = !isSubagentEvent(event.Kind)
		case inExchange && event.TurnID == "" && !keptAcrossTurns(event.Kind):
			drop[i] = true
		}
		if drop[i] && event.Kind == EventToolCall && event.Tool != nil && event.Tool.ID != "" {
			tools[event.Tool.ID] = true
		}
	}
	for i, event := range events {
		if isSubagentEvent(event.Kind) && event.ParentToolID != "" && tools[event.ParentToolID] {
			drop[i] = true
		}
	}
	return drop
}
