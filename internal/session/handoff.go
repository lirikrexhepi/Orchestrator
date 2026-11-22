package session

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"composer/internal/domain"
)

const maxHandoffChars = 24000
const perMessageCap = 4000

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}

func ExtractConversationText(events []domain.RuntimeEvent, maxChars int) string {
	text, omitted := extractConversationWithMeta(events, maxChars)
	if omitted {
		return "[earlier context omitted: showing most recent context]\n" + text
	}
	return text
}

func ExtractConversationWithMeta(events []domain.RuntimeEvent, maxChars int) (string, bool) {
	return extractConversationWithMeta(events, maxChars)
}

func extractConversationWithMeta(events []domain.RuntimeEvent, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = maxHandoffChars
	}
	lines := make([]string, 0, len(events))
	format := func(prefix, text string, capRunes int) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if utf8.RuneCountInString(text) > capRunes {
			text = truncateRunes(text, capRunes) + "\n[truncated]"
		}
		formatted := prefix + text
		if len(lines) > 0 && lines[len(lines)-1] == formatted {
			return
		}
		lines = append(lines, formatted)
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
		format("Assistant: ", entry.text.String(), perMessageCap)
		for i, p := range pending {
			if p == entry {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
	}
	flushAll := func() {
		for _, entry := range pending {
			if current, ok := pendingByTurn[entry.turnID]; ok && current == entry {
				delete(pendingByTurn, entry.turnID)
				format("Assistant: ", entry.text.String(), perMessageCap)
			}
		}
		pending = pending[:0]
		clear(pendingByTurn)
	}

	var modifiedFiles []string
	seenModified := make(map[string]bool)

	recordModifiedFile := func(target string) {
		target = strings.TrimSpace(target)
		if target == "" || seenModified[target] {
			return
		}
		seenModified[target] = true
		modifiedFiles = append(modifiedFiles, target)
	}

	flushModifiedFiles := func() {
		if len(modifiedFiles) == 0 {
			return
		}
		joined := strings.Join(modifiedFiles, ", ")
		if utf8.RuneCountInString(joined) > 500 {
			joined = truncateRunes(joined, 500) + "..."
		}
		line := "[Modified files: " + joined + "]"
		if len(lines) == 0 || lines[len(lines)-1] != line {
			lines = append(lines, line)
		}
		modifiedFiles = modifiedFiles[:0]
		clear(seenModified)
	}

	for _, event := range events {
		switch event.Kind {
		case domain.EventUserMessage:
			flushAll()
			flushModifiedFiles()
			format("User: ", event.Text, perMessageCap)
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
					for i, p := range pending {
						if p == entry {
							pending = append(pending[:i], pending[i+1:]...)
							break
						}
					}
				} else {
					flushTurn(event.TurnID)
				}
			}
			format("Assistant: ", event.Text, perMessageCap)
		case domain.EventToolCall, domain.EventToolResult:
			flushAll()
			if event.Tool != nil {
				if event.Tool.Category == domain.ToolCategoryEdit ||
					event.Tool.Category == domain.ToolCategoryWrite ||
					len(event.Tool.Diffs) > 0 {
					target := event.Tool.Target
					if target == "" && len(event.Tool.Diffs) > 0 {
						target = event.Tool.Diffs[0].Path
					}
					if target != "" {
						recordModifiedFile(target)
					}
				}
			}
		case domain.EventTurnFailed:
			flushAll()
			flushModifiedFiles()
			if event.Error != "" {
				format("System: previous turn failed: ", event.Error, 500)
			}
		case domain.EventTurnCompleted:
			flushAll()
			flushModifiedFiles()
		}
	}
	flushAll()
	flushModifiedFiles()

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
	for _, line := range keptLines {
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String()), omitted
}

func BuildHandoffPrompt(prevDriver domain.DriverKind, newDriver domain.DriverKind, events []domain.RuntimeEvent, cwd string) string {
	return BuildHandoffPromptForThread(prevDriver, newDriver, "", events, cwd)
}

func BuildHandoffPromptForThread(prevDriver domain.DriverKind, newDriver domain.DriverKind, threadID string, events []domain.RuntimeEvent, cwd string) string {
	conversation, omitted := extractConversationWithMeta(events, maxHandoffChars)
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
	b.WriteString("This is a direct continuation of the same thread. Pick up exactly where the previous agent left off. Do not restart the task or re-spawn another thread unless asked.\n\n")
	if conversation == "" {
		b.WriteString("[No prior transcript was available. Treat the next user message as a fresh task in the same directory.]\n")
	} else {
		b.WriteString("--- Prior conversation ---\n")
		if omitted {
			if threadID != "" {
				b.WriteString(fmt.Sprintf("[Earlier turns omitted. Call read_thread(threadId: %q) via the orchestrator MCP server if you need earlier details.]\n", threadID))
			} else {
				b.WriteString("[earlier context omitted: showing most recent context]\n")
			}
		}
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
