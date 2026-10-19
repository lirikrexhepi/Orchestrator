package session

import (
	"strings"

	"composer/internal/domain"
)

const maxHandoffChars = 12000
const maxToolOutputChars = 1200

func ExtractConversationText(events []domain.RuntimeEvent, maxChars int) string {
	if maxChars <= 0 {
		maxChars = maxHandoffChars
	}
	lines := make([]string, 0, len(events))
	format := func(prefix, text string, cap int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if len(text) > cap {
			text = text[:cap] + "\n[truncated]"
		}
		lines = append(lines, prefix+text)
	}
	// Streamed text arrives as per-turn deltas that never get a terminal frame
	// on some providers, so skipping deltas would drop whole replies. Merge
	// them per turn; a non-delta frame for the same turn replaces its merge
	// when either side contains the other (echo), else both are kept.
	type pendingText struct {
		turnID string
		text   strings.Builder
	}
	pending := make([]*pendingText, 0, 8)
	pendingByTurn := make(map[string]*pendingText)
	accumulate := func(turnID, text string) {
		if text == "" {
			return
		}
		entry, ok := pendingByTurn[turnID]
		if !ok {
			entry = &pendingText{turnID: turnID}
			pendingByTurn[turnID] = entry
			pending = append(pending, entry)
		}
		if entry.text.Len() < maxHandoffChars {
			entry.text.WriteString(text)
		}
	}
	flushTurn := func(turnID string) {
		entry, ok := pendingByTurn[turnID]
		if !ok {
			return
		}
		delete(pendingByTurn, turnID)
		format("Assistant: ", entry.text.String(), 2000)
	}
	flushAll := func() {
		for _, entry := range pending {
			if _, ok := pendingByTurn[entry.turnID]; !ok {
				continue
			}
			delete(pendingByTurn, entry.turnID)
			format("Assistant: ", entry.text.String(), 2000)
		}
	}
	for _, event := range events {
		switch event.Kind {
		case domain.EventUserMessage:
			flushAll()
			format("User: ", event.Text, 2000)
		case domain.EventAgentMessage:
			if event.Delta {
				accumulate(event.TurnID, event.Text)
				continue
			}
			if entry, ok := pendingByTurn[event.TurnID]; ok {
				merged := entry.text.String()
				if strings.Contains(merged, strings.TrimSpace(event.Text)) ||
					strings.Contains(strings.TrimSpace(event.Text), strings.TrimSpace(merged)) {
					delete(pendingByTurn, event.TurnID)
				} else {
					flushTurn(event.TurnID)
				}
			}
			format("Assistant: ", event.Text, 2000)
		case domain.EventToolResult:
			if event.Tool == nil {
				continue
			}
			output := strings.TrimSpace(event.Tool.Output)
			if output == "" {
				continue
			}
			flushAll()
			format("Tool "+event.Tool.Name+": ", output, maxToolOutputChars)
		case domain.EventTurnFailed:
			flushAll()
			if event.Error != "" {
				format("System: previous turn failed: ", event.Error, 500)
			}
		case domain.EventTurnCompleted:
			flushAll()
		}
	}
	flushAll()
	total := 0
	kept := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if total+len(lines[i])+1 > maxChars {
			break
		}
		total += len(lines[i]) + 1
		kept++
	}
	keptLines := lines
	omitted := false
	if kept < len(lines) {
		keptLines = lines[len(lines)-kept:]
		omitted = len(keptLines) > 0
	}
	var b strings.Builder
	if omitted {
		b.WriteString("[earlier context omitted: showing most recent context]\n")
	}
	for _, line := range keptLines {
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

func BuildHandoffPrompt(prevDriver domain.DriverKind, newDriver domain.DriverKind, events []domain.RuntimeEvent, cwd string) string {
	conversation := ExtractConversationText(events, maxHandoffChars)
	var b strings.Builder
	if prevDriver != "" && prevDriver == newDriver {
		b.WriteString("This conversation was continued in a new session because the previous one could not be resumed. ")
	} else {
		b.WriteString("This conversation was continued from another provider. ")
	}
	if prevDriver != "" && newDriver != "" && prevDriver != newDriver {
		b.WriteString("Previous provider was " + string(prevDriver) + ", you are " + string(newDriver) + ". ")
	}
	if cwd != "" {
		b.WriteString("Working directory is " + cwd + ". ")
	}
	b.WriteString("Pick up exactly where the previous agent left off. Do not restart the task.\n\n")
	if conversation == "" {
		b.WriteString("[No prior transcript was available. Treat the next user message as a fresh task in the same directory.]\n")
	} else {
		b.WriteString("--- Prior conversation ---\n")
		b.WriteString(conversation)
		b.WriteString("\n--- End prior conversation ---\n")
	}
	return b.String()
}

func DedupeFullText(events []domain.RuntimeEvent) []domain.RuntimeEvent {
	seenDelta := make(map[string]*strings.Builder)
	out := make([]domain.RuntimeEvent, 0, len(events))
	for _, event := range events {
		if (event.Kind == domain.EventAgentMessage || event.Kind == domain.EventAgentThought) && event.Delta {
			key := event.ThreadID + "\x00" + event.TurnID + "\x00" + string(event.Kind)
			builder, ok := seenDelta[key]
			if !ok {
				builder = &strings.Builder{}
				seenDelta[key] = builder
			}
			builder.WriteString(event.Text)
			continue
		}
		out = append(out, event)
	}
	return out
}
