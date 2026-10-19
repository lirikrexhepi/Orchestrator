package claudeimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"composer/internal/domain"
	"composer/internal/opencode"
)

func isOpenCodeV2(db *sql.DB) bool {
	var name string
	if db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'session_v2'`).Scan(&name) != nil {
		return false
	}
	if db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'session'`).Scan(&name) != nil {
		return true
	}
	var found int
	return db.QueryRow(`SELECT 1 FROM session_v2 LIMIT 1`).Scan(&found) == nil
}

func listOpenCodeV2Page(db *sql.DB, offset, limit int) Page {
	empty := Page{Sessions: []ExternalSession{}, Next: -1}
	rows, err := db.Query(
		`SELECT id, COALESCE(title, ''), directory, time_created, time_updated FROM session_v2
		 WHERE parent_id IS NULL AND time_archived IS NULL
		   AND COALESCE(title, '') NOT LIKE 'task-%' AND COALESCE(title, '') NOT LIKE 'New session%' AND TRIM(COALESCE(title, '')) <> ''
		 ORDER BY time_updated DESC LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return empty
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
	return page
}

type openCodeV2Message struct {
	Text  string `json:"text"`
	Model struct {
		ID string `json:"id"`
	} `json:"model"`
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		State struct {
			Status  string         `json:"status"`
			Input   map[string]any `json:"input"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"state"`
	} `json:"content"`
}

func parseOpenCodeV2Session(db *sql.DB, sessionID string) (*ParsedSession, error) {
	parsed := &ParsedSession{SessionID: sessionID}
	var created, updated int64
	if err := db.QueryRow(`SELECT COALESCE(title, ''), directory, time_created, time_updated FROM session_v2 WHERE id = ?`, sessionID).
		Scan(&parsed.Title, &parsed.Cwd, &created, &updated); err != nil {
		return nil, fmt.Errorf("session not found")
	}
	parsed.StartedAt = created
	parsed.UpdatedAt = updated

	rows, err := db.Query(`SELECT id, type, time_created, data FROM session_message WHERE session_id = ? ORDER BY seq, time_created, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turn := 0
	turnID := ""
	seq := 0
	emit := func(event domain.RuntimeEvent) {
		seq++
		event.Driver = domain.DriverOpenCode
		event.Seq = uint64(seq)
		parsed.Events = append(parsed.Events, event)
	}

	for rows.Next() {
		var id, kind, data string
		var at int64
		if rows.Scan(&id, &kind, &at, &data) != nil {
			continue
		}
		var message openCodeV2Message
		if json.Unmarshal([]byte(data), &message) != nil {
			continue
		}
		switch kind {
		case "user":
			body := strings.TrimSpace(message.Text)
			if body == "" {
				continue
			}
			turn++
			turnID = "import-turn-" + itoa(turn)
			if parsed.Prompt == "" {
				parsed.Prompt = truncate(body, 2000)
			}
			emit(domain.RuntimeEvent{Kind: domain.EventUserMessage, TurnID: turnID, ItemID: id, At: at, Text: truncate(body, maxTextChars)})
		case "assistant":
			if turnID == "" {
				turn++
				turnID = "import-turn-" + itoa(turn)
			}
			if message.Model.ID != "" {
				parsed.Model = message.Model.ID
			}
			for index, item := range message.Content {
				itemID := id + ":" + itoa(index)
				switch item.Type {
				case "text", "reasoning":
					if strings.TrimSpace(item.Text) == "" {
						continue
					}
					eventKind := domain.EventAgentMessage
					if item.Type == "reasoning" {
						eventKind = domain.EventAgentThought
					}
					emit(domain.RuntimeEvent{Kind: eventKind, TurnID: turnID, ItemID: itemID, At: at, Text: truncate(item.Text, maxTextChars)})
				case "tool":
					callID := firstNonEmpty(item.ID, itemID)
					name := opencode.NormalizeToolName(item.Name)
					emit(domain.RuntimeEvent{
						Kind: domain.EventToolCall, TurnID: turnID, ItemID: callID, At: at,
						Tool: &domain.ToolCall{ID: callID, Name: name, Status: domain.ToolInProgress, Input: item.State.Input},
					})
					var output strings.Builder
					for _, part := range item.State.Content {
						if part.Type == "text" && part.Text != "" {
							if output.Len() > 0 {
								output.WriteString("\n")
							}
							output.WriteString(part.Text)
						}
					}
					status := domain.ToolCompleted
					if item.State.Status == "error" || item.State.Status == "failed" || item.State.Error != nil {
						status = domain.ToolFailed
						if item.State.Error != nil && output.Len() == 0 {
							output.WriteString(item.State.Error.Message)
						}
					}
					emit(domain.RuntimeEvent{
						Kind: domain.EventToolResult, TurnID: turnID, ItemID: callID, At: at,
						Tool: &domain.ToolCall{ID: callID, Name: name, Status: status, Output: truncate(output.String(), maxToolOutputChars)},
					})
				}
			}
		}
	}
	return parsed, nil
}

var hiddenAntigravity = struct {
	sync.RWMutex
	ids map[string]bool
}{ids: make(map[string]bool)}

func HideAntigravityConversations(ids ...string) {
	hiddenAntigravity.Lock()
	defer hiddenAntigravity.Unlock()
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			hiddenAntigravity.ids[id] = true
		}
	}
}

func antigravityHidden(id string) bool {
	hiddenAntigravity.RLock()
	defer hiddenAntigravity.RUnlock()
	return hiddenAntigravity.ids[id]
}
