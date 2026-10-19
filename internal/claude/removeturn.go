package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"composer/internal/provider"
)

var ErrTurnCompacted = errors.New("this message was already compacted into a summary, so it can no longer be removed from the agent's memory")

type sessionLine struct {
	raw    []byte
	fields map[string]json.RawMessage
	kind   string
	uuid   string
	parent string
	prompt string
}

func (l *sessionLine) str(key string) string {
	var value string
	if raw, ok := l.fields[key]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func (l *sessionLine) flag(key string) bool {
	var value bool
	if raw, ok := l.fields[key]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func (l *sessionLine) content() (string, bool) {
	raw, ok := l.fields["message"]
	if !ok {
		return "", false
	}
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return "", false
	}
	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return text, false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(message.Content, &blocks) != nil {
		return "", false
	}
	var parts []string
	toolResult := false
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "tool_result":
			toolResult = true
		}
	}
	return strings.Join(parts, "\n"), toolResult
}

func (l *sessionLine) isTurnStart() bool {
	if l.kind != "user" || l.flag("isSidechain") {
		return false
	}
	if _, ok := l.fields["toolUseResult"]; ok {
		return false
	}
	_, toolResult := l.content()
	return !toolResult
}

func (l *sessionLine) set(key string, value string) {
	encoded, _ := json.Marshal(value)
	l.fields[key] = encoded
	l.raw = nil
}

func (l *sessionLine) bytes() []byte {
	if l.raw != nil {
		return l.raw
	}
	encoded, err := json.Marshal(l.fields)
	if err != nil {
		return nil
	}
	return encoded
}

const currentRequestMarker = "--- Current request ---"

func currentRequest(text string) string {
	if index := strings.LastIndex(text, currentRequestMarker); index >= 0 {
		return text[index+len(currentRequestMarker):]
	}
	return text
}

func normalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func (a *Adapter) RemoveTurn(sessionID, text string, occurrence int) error {
	config := sessionConfigDir(provider.LaunchEnv(a.settings))
	if config == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return fmt.Errorf("no Claude session to edit")
	}
	matches, _ := filepath.Glob(filepath.Join(config, "projects", "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return fmt.Errorf("claude session %s not found under %s", sessionID, filepath.Join(config, "projects"))
	}
	return removeTurnFromFile(matches[0], text, occurrence)
}

func readSession(path string) ([]*sessionLine, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []*sessionLine
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 0, 1024*1024), 256*1024*1024)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		line := &sessionLine{raw: append([]byte(nil), raw...)}
		if json.Unmarshal(raw, &line.fields) != nil {
			line.fields = nil
			lines = append(lines, line)
			continue
		}
		line.kind = line.str("type")
		line.uuid = line.str("uuid")
		line.parent = line.str("parentUuid")
		line.prompt = line.str("promptId")
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

func findTurn(lines []*sessionLine, text string, occurrence int) (int, int, error) {
	needle := normalizeText(text)
	if len(needle) > 200 {
		needle = needle[:200]
	}
	if needle == "" {
		return 0, 0, fmt.Errorf("message has no text to match")
	}

	type turn struct {
		start  int
		prompt string
		text   string
	}
	var turns []turn
	for i, line := range lines {
		if line.fields == nil || !line.isTurnStart() || line.flag("isMeta") {
			continue
		}
		body, _ := line.content()
		if n := len(turns); n > 0 && line.prompt != "" && turns[n-1].prompt == line.prompt {
			turns[n-1].text += "\n" + body
			continue
		}
		turns = append(turns, turn{start: i, prompt: line.prompt, text: body})
	}

	var found []turn
	for _, candidate := range turns {
		if strings.Contains(normalizeText(currentRequest(candidate.text)), needle) {
			found = append(found, candidate)
		}
	}
	if occurrence < 0 || occurrence >= len(found) {
		return 0, 0, fmt.Errorf("couldn't find this message in the agent's session")
	}
	target := found[len(found)-1-occurrence]

	end := len(lines)
	for i := target.start + 1; i < len(lines); i++ {
		line := lines[i]
		if line.fields == nil || !line.isTurnStart() {
			continue
		}
		if target.prompt != "" && line.prompt == target.prompt {
			continue
		}
		if target.prompt == "" && line.flag("isMeta") {
			continue
		}
		end = i
		break
	}
	start := target.start
	for start > 0 && lines[start-1].kind == "queue-operation" {
		start--
	}
	if end < len(lines) {
		for end-1 > target.start && lines[end-1].kind == "queue-operation" {
			end--
		}
	}
	return start, end, nil
}

func removeTurnFromFile(path, text string, occurrence int) error {
	lines, err := readSession(path)
	if err != nil {
		return err
	}
	start, end, err := findTurn(lines, text, occurrence)
	if err != nil {
		return err
	}
	for _, line := range lines[start:] {
		if line.fields == nil {
			continue
		}
		if (line.kind == "system" && line.str("subtype") == "compact_boundary") || line.flag("isCompactSummary") {
			return ErrTurnCompacted
		}
	}

	parentOf := make(map[string]string)
	for _, line := range lines {
		if line.uuid != "" {
			parentOf[line.uuid] = line.parent
		}
	}
	removed := make(map[string]bool)
	for _, line := range lines[start:end] {
		if line.uuid != "" {
			removed[line.uuid] = true
		}
	}
	resolve := func(id string) string {
		for seen := 0; id != "" && removed[id] && seen < len(lines); seen++ {
			id = parentOf[id]
		}
		return id
	}

	kept := make([]*sessionLine, 0, len(lines)-(end-start))
	kept = append(kept, lines[:start]...)
	kept = append(kept, lines[end:]...)
	keptIDs := make(map[string]bool)
	for _, line := range kept {
		if line.uuid != "" {
			keptIDs[line.uuid] = true
		}
	}
	for _, line := range kept {
		if line.fields == nil {
			continue
		}
		if line.parent != "" && removed[line.parent] {
			line.parent = resolve(line.parent)
			if line.parent == "" {
				line.fields["parentUuid"] = json.RawMessage("null")
				line.raw = nil
			} else {
				line.set("parentUuid", line.parent)
			}
		}
		if leaf := line.str("leafUuid"); leaf != "" && removed[leaf] {
			line.set("leafUuid", resolve(leaf))
		}
	}

	if end == len(lines) {
		leaf := ""
		for i := len(kept) - 1; i >= 0; i-- {
			if kept[i].uuid != "" && !kept[i].flag("isSidechain") {
				leaf = kept[i].uuid
				break
			}
		}
		for i := len(kept) - 1; i >= 0; i-- {
			if kept[i].kind == "last-prompt" {
				if leaf != "" {
					kept[i].set("leafUuid", leaf)
				}
				break
			}
		}
	}

	for _, line := range kept {
		if line.parent != "" && !keptIDs[line.parent] {
			return fmt.Errorf("refusing to rewrite session: record %s would point at a missing parent", line.uuid)
		}
	}

	var out bytes.Buffer
	for _, line := range kept {
		encoded := line.bytes()
		if encoded == nil {
			return fmt.Errorf("refusing to rewrite session: a record could not be encoded")
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	backup := path + ".bak-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := os.WriteFile(backup, original, 0o644); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
