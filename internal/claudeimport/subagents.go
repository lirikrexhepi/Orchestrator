package claudeimport

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"composer/internal/domain"
)

const maxSubagentOutputChars = 2000

type subagentMeta struct {
	ToolUseID string `json:"toolUseId"`
}

func (c *converter) attachSubagents(path string, parsed *ParsedSession) {
	dir := filepath.Join(strings.TrimSuffix(path, filepath.Ext(path)), "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		c.settleLaunched(parsed, nil)
		return
	}
	var extra []domain.RuntimeEvent
	lastAt := map[string]int64{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var meta subagentMeta
		if json.Unmarshal(raw, &meta) != nil || meta.ToolUseID == "" {
			continue
		}
		transcript := filepath.Join(dir, strings.TrimSuffix(name, ".meta.json")+".jsonl")
		events := readSubagentTranscript(transcript, meta.ToolUseID, c.fallbackAt)
		for _, event := range events {
			if event.At > lastAt[meta.ToolUseID] {
				lastAt[meta.ToolUseID] = event.At
			}
		}
		extra = append(extra, events...)
	}
	if len(extra) > 0 {
		parsed.Events = append(parsed.Events, extra...)
		sort.SliceStable(parsed.Events, func(i, j int) bool { return parsed.Events[i].At < parsed.Events[j].At })
	}
	c.settleLaunched(parsed, lastAt)
}

func (c *converter) settleLaunched(parsed *ParsedSession, lastAt map[string]int64) {
	ids := make([]string, 0, len(c.launched))
	for id := range c.launched {
		if !c.finished[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		launchedAt := c.launched[id]
		endedAt := launchedAt
		if last := lastAt[id]; last > endedAt {
			endedAt = last
		}
		parsed.Events = append(parsed.Events, domain.RuntimeEvent{
			Kind:   domain.EventToolResult,
			Driver: domain.DriverClaude,
			ItemID: id,
			At:     endedAt,
			Tool:   &domain.ToolCall{ID: id, Status: domain.ToolCompleted, DurationMs: endedAt - launchedAt},
		})
	}
	if len(ids) > 0 {
		sort.SliceStable(parsed.Events, func(i, j int) bool { return parsed.Events[i].At < parsed.Events[j].At })
	}
}

func readSubagentTranscript(path, parentID string, fallbackAt int64) []domain.RuntimeEvent {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var events []domain.RuntimeEvent
	var lastAt int64
	reader := bufio.NewReaderSize(file, 256*1024)
	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			var record fileRecord
			if json.Unmarshal(line, &record) == nil {
				at := parseAt(record.Timestamp, fallbackAt)
				if at < lastAt {
					at = lastAt
				}
				lastAt = at
				events = append(events, subagentEvents(record, parentID, at)...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
	return events
}

func subagentEvents(record fileRecord, parentID string, at int64) []domain.RuntimeEvent {
	if len(record.Message) == 0 {
		return nil
	}
	var msg fileMessage
	if json.Unmarshal(record.Message, &msg) != nil {
		return nil
	}
	base := domain.RuntimeEvent{Driver: domain.DriverClaude, ParentToolID: parentID, At: at}
	var out []domain.RuntimeEvent
	switch record.Type {
	case "user":
		_, results := splitUserContent(msg.Content)
		for _, result := range results {
			event := base
			event.Kind = domain.EventSubagentToolDone
			event.ItemID = result.id
			event.Tool = &domain.ToolCall{
				ID:     result.id,
				Status: result.status,
				Output: truncate(result.output, maxSubagentOutputChars),
			}
			out = append(out, event)
		}
	case "assistant":
		var blocks []contentBlock
		if json.Unmarshal(msg.Content, &blocks) != nil {
			return nil
		}
		for i, block := range blocks {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) == "" {
					continue
				}
				event := base
				event.Kind = domain.EventSubagentMessage
				event.ItemID = firstNonEmpty(record.UUID, parentID) + "-" + itoa(i)
				event.Text = truncate(block.Text, maxTextChars)
				out = append(out, event)
			case "tool_use":
				if block.ID == "" {
					continue
				}
				tool := &domain.ToolCall{ID: block.ID, Name: block.Name, Status: domain.ToolInProgress}
				if len(block.Input) > 0 {
					var input map[string]any
					if json.Unmarshal(block.Input, &input) == nil {
						tool.Input = input
					}
				}
				event := base
				event.Kind = domain.EventSubagentToolCall
				event.ItemID = block.ID
				event.Tool = tool
				out = append(out, event)
			}
		}
	}
	return out
}
