package claudeimport

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"composer/internal/domain"

	_ "modernc.org/sqlite"
)

const (
	ProviderClaude      = "claude"
	ProviderOpenCode    = "opencode"
	ProviderAntigravity = "antigravity"

	liteChunkBytes = 128 * 1024
)

func describeLite(path string, info os.FileInfo) *ExternalSession {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	head := make([]byte, liteChunkBytes)
	n, _ := file.Read(head)
	head = head[:n]

	var tail []byte
	if info.Size() > int64(2*liteChunkBytes) {
		tail = make([]byte, liteChunkBytes)
		read, _ := file.ReadAt(tail, info.Size()-int64(liteChunkBytes))
		tail = tail[:read]
	}

	fallbackAt := info.ModTime().UnixMilli()
	parsed := &ParsedSession{}
	conv := &converter{fallbackAt: fallbackAt}
	for _, chunk := range [][]byte{head, tail} {
		for _, line := range bytes.Split(chunk, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var record fileRecord
			if json.Unmarshal(line, &record) == nil {
				conv.visit(record, parsed)
			}
		}
	}
	if !hasHumanText(parsed.Events) {
		return nil
	}
	if parsed.SessionID == "" {
		parsed.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	title := firstNonEmpty(conv.customTitle, conv.aiTitle, conv.summary, previewText(conv.titlePrompt, 60))
	if title == "" {
		title = "Imported Claude Code chat"
	}
	if parsed.StartedAt == 0 {
		parsed.StartedAt = fallbackAt
	}
	return &ExternalSession{
		ID:         parsed.SessionID,
		Title:      title,
		Cwd:        parsed.Cwd,
		Model:      parsed.Model,
		Preview:    previewText(parsed.Prompt, 140),
		FilePath:   path,
		StartedAt:  parsed.StartedAt,
		UpdatedAt:  fallbackAt,
		AgentRun:   isAgentRun(parsed),
		Entrypoint: parsed.Entrypoint,
		Provider:   ProviderClaude,
	}
}

func homeSub(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{home}, parts...)...)
}

func openCodeDBPath() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		candidate := filepath.Join(dir, "opencode", "opencode.db")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return homeSub(".local", "share", "opencode", "opencode.db")
}

func antigravityDBPath() string {
	return homeSub(".gemini", "antigravity", "conversation_summaries.db")
}

func openReadOnly(path string) (*sql.DB, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?mode=ro&_pragma=busy_timeout(3000)"
	return sql.Open("sqlite", dsn)
}

func ListOpenCodePage(offset, limit int) (Page, error) {
	offset, limit = clampPage(offset, limit)
	empty := Page{Sessions: []ExternalSession{}, Next: -1}
	db, err := openReadOnly(openCodeDBPath())
	if err != nil {
		return empty, nil
	}
	defer db.Close()
	if isOpenCodeV2(db) {
		return listOpenCodeV2Page(db, offset, limit), nil
	}
	rows, err := db.Query(
		`SELECT id, title, directory, time_created, time_updated FROM session
		 WHERE parent_id IS NULL AND time_archived IS NULL
		   AND title NOT LIKE 'task-%' AND title NOT LIKE 'New session%' AND TRIM(title) <> ''
		 ORDER BY time_updated DESC LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return empty, nil
	}
	defer rows.Close()
	out := make([]ExternalSession, 0, limit)
	more := false
	for rows.Next() {
		var id, title, dir string
		var created, updated int64
		if rows.Scan(&id, &title, &dir, &created, &updated) != nil {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		out = append(out, ExternalSession{
			ID:        id,
			Title:     title,
			Cwd:       dir,
			FilePath:  "opencode:" + id,
			StartedAt: created,
			UpdatedAt: updated,
			Provider:  ProviderOpenCode,
		})
	}
	page := Page{Sessions: out, Next: -1}
	if more {
		page.Next = offset + limit
	}
	return page, nil
}

func ListAntigravityPage(offset, limit int) (Page, error) {
	offset, limit = clampPage(offset, limit)
	empty := Page{Sessions: []ExternalSession{}, Next: -1}
	db, err := openReadOnly(antigravityDBPath())
	if err != nil {
		return empty, nil
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT conversation_id, title, workspace_uris, last_modified_time FROM conversation_summaries
		 WHERE (parent_conversation_id IS NULL OR parent_conversation_id = '') AND TRIM(title) <> ''
		 ORDER BY last_modified_time DESC LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return empty, nil
	}
	defer rows.Close()
	out := make([]ExternalSession, 0, limit)
	more := false
	for rows.Next() {
		var id, title, uris string
		var modified any
		if rows.Scan(&id, &title, &uris, &modified) != nil || antigravityHidden(id) {
			continue
		}
		if len(out) == limit {
			more = true
			break
		}
		at := parseSQLTime(modified)
		out = append(out, ExternalSession{
			ID:        id,
			Title:     title,
			Cwd:       firstWorkspacePath(uris),
			FilePath:  "antigravity:" + id,
			StartedAt: at,
			UpdatedAt: at,
			Provider:  ProviderAntigravity,
		})
	}
	page := Page{Sessions: out, Next: -1}
	if more {
		page.Next = offset + limit
	}
	return page, nil
}

func clampPage(offset, limit int) (int, int) {
	if limit <= 0 {
		limit = pageDefaultSize
	}
	if limit > pageMaxLimit {
		limit = pageMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return offset, limit
}

func parseSQLTime(value any) int64 {
	switch v := value.(type) {
	case time.Time:
		return v.UnixMilli()
	case int64:
		return v
	case string:
		for _, layout := range []string{
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05-07:00",
			time.RFC3339Nano,
		} {
			if parsed, err := time.Parse(layout, v); err == nil {
				return parsed.UnixMilli()
			}
		}
	}
	return 0
}

func firstWorkspacePath(raw string) string {
	var uris []string
	if json.Unmarshal([]byte(raw), &uris) != nil || len(uris) == 0 {
		return ""
	}
	parsed, err := url.Parse(uris[0])
	if err != nil {
		return ""
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(path, "/")
}

func ListChatPage(provider string, offset, limit int) (Page, error) {
	switch provider {
	case ProviderOpenCode:
		return ListOpenCodePage(offset, limit)
	case ProviderAntigravity:
		return ListAntigravityPage(offset, limit)
	default:
		return ListPage(offset, limit)
	}
}

type openCodeMessage struct {
	Role  string `json:"role"`
	Model struct {
		ModelID string `json:"modelID"`
	} `json:"model"`
	ModelID string `json:"modelID"`
}

type openCodePart struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Tool   string `json:"tool"`
	CallID string `json:"callID"`
	State  struct {
		Status string         `json:"status"`
		Input  map[string]any `json:"input"`
		Output string         `json:"output"`
	} `json:"state"`
}

func ParseOpenCodeSession(sessionID string) (*ParsedSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("missing session id")
	}
	db, err := openReadOnly(openCodeDBPath())
	if err != nil {
		return nil, fmt.Errorf("OpenCode history is unavailable on this machine")
	}
	defer db.Close()
	if isOpenCodeV2(db) {
		return parseOpenCodeV2Session(db, sessionID)
	}

	parsed := &ParsedSession{SessionID: sessionID}
	var created, updated int64
	if err := db.QueryRow(`SELECT title, directory, time_created, time_updated FROM session WHERE id = ?`, sessionID).
		Scan(&parsed.Title, &parsed.Cwd, &created, &updated); err != nil {
		return nil, fmt.Errorf("session not found")
	}
	parsed.StartedAt = created
	parsed.UpdatedAt = updated

	rows, err := db.Query(
		`SELECT m.id, m.time_created, m.data FROM message m WHERE m.session_id = ? ORDER BY m.time_created, m.id`, sessionID)
	if err != nil {
		return nil, err
	}
	type messageRow struct {
		id      string
		at      int64
		message openCodeMessage
	}
	var messages []messageRow
	for rows.Next() {
		var id, data string
		var at int64
		if rows.Scan(&id, &at, &data) != nil {
			continue
		}
		var message openCodeMessage
		if json.Unmarshal([]byte(data), &message) != nil {
			continue
		}
		messages = append(messages, messageRow{id: id, at: at, message: message})
	}
	rows.Close()

	turn := 0
	turnID := ""
	seq := 0
	emit := func(event domain.RuntimeEvent) {
		seq++
		event.Driver = domain.DriverOpenCode
		event.Seq = uint64(seq)
		parsed.Events = append(parsed.Events, event)
	}

	for _, row := range messages {
		partRows, err := db.Query(`SELECT id, data FROM part WHERE message_id = ? ORDER BY time_created, id`, row.id)
		if err != nil {
			continue
		}
		type partRow struct {
			id   string
			part openCodePart
		}
		var parts []partRow
		for partRows.Next() {
			var id, data string
			if partRows.Scan(&id, &data) != nil {
				continue
			}
			var part openCodePart
			if json.Unmarshal([]byte(data), &part) == nil {
				parts = append(parts, partRow{id: id, part: part})
			}
		}
		partRows.Close()

		if row.message.Role == "user" {
			var text strings.Builder
			for _, p := range parts {
				if p.part.Type == "text" && strings.TrimSpace(p.part.Text) != "" {
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(p.part.Text)
				}
			}
			body := strings.TrimSpace(text.String())
			if body == "" {
				continue
			}
			turn++
			turnID = "import-turn-" + itoa(turn)
			if parsed.Prompt == "" {
				parsed.Prompt = truncate(body, 2000)
			}
			if parsed.Model == "" {
				parsed.Model = row.message.Model.ModelID
			}
			emit(domain.RuntimeEvent{Kind: domain.EventUserMessage, TurnID: turnID, ItemID: row.id, At: row.at, Text: truncate(body, maxTextChars)})
			continue
		}

		if turnID == "" {
			turn++
			turnID = "import-turn-" + itoa(turn)
		}
		if row.message.ModelID != "" {
			parsed.Model = row.message.ModelID
		}
		for _, p := range parts {
			switch p.part.Type {
			case "text":
				if strings.TrimSpace(p.part.Text) == "" {
					continue
				}
				emit(domain.RuntimeEvent{Kind: domain.EventAgentMessage, TurnID: turnID, ItemID: p.id, At: row.at, Text: truncate(p.part.Text, maxTextChars)})
			case "reasoning":
				if strings.TrimSpace(p.part.Text) == "" {
					continue
				}
				emit(domain.RuntimeEvent{Kind: domain.EventAgentThought, TurnID: turnID, ItemID: p.id, At: row.at, Text: truncate(p.part.Text, maxTextChars)})
			case "tool":
				callID := firstNonEmpty(p.part.CallID, p.id)
				emit(domain.RuntimeEvent{
					Kind: domain.EventToolCall, TurnID: turnID, ItemID: callID, At: row.at,
					Tool: &domain.ToolCall{ID: callID, Name: p.part.Tool, Status: domain.ToolInProgress, Input: p.part.State.Input},
				})
				status := domain.ToolCompleted
				if p.part.State.Status == "error" {
					status = domain.ToolFailed
				}
				emit(domain.RuntimeEvent{
					Kind: domain.EventToolResult, TurnID: turnID, ItemID: callID, At: row.at,
					Tool: &domain.ToolCall{ID: callID, Name: p.part.Tool, Status: status, Output: truncate(p.part.State.Output, maxToolOutputChars)},
				})
			}
		}
	}
	return parsed, nil
}
