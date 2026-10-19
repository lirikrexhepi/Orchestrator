package claudeimport

import (
	"database/sql"
	"path/filepath"
	"testing"

	"composer/internal/domain"
)

func TestOpenCodeV2Import(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE session_v2 (id text PRIMARY KEY, parent_id text, title text, directory text NOT NULL, time_created integer, time_updated integer, time_archived integer)`,
		`CREATE TABLE session_message (id text PRIMARY KEY, session_id text, type text, seq integer, time_created integer, time_updated integer, data text)`,
		`INSERT INTO session_v2 VALUES ('ses_1', NULL, 'Fix the build', 'C:/repo', 1, 5, NULL)`,
		`INSERT INTO session_v2 VALUES ('ses_child', 'ses_1', 'child', 'C:/repo', 2, 6, NULL)`,
		`INSERT INTO session_message VALUES ('m1', 'ses_1', 'user', 1, 1, 1, '{"text":"run the tests"}')`,
		`INSERT INTO session_message VALUES ('m2', 'ses_1', 'assistant', 2, 2, 2, '{"model":{"id":"big-pickle"},"content":[{"type":"reasoning","text":"thinking"},{"type":"tool","id":"call_1","name":"shell","state":{"status":"completed","input":{"command":"go test"},"content":[{"type":"text","text":"ok"}]}},{"type":"text","text":"All green."}]}')`,
		`INSERT INTO session_message VALUES ('m3', 'ses_1', 'idle', 3, 3, 3, '{"outcome":"succeeded"}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	if !isOpenCodeV2(db) {
		t.Fatal("v2 schema not detected")
	}
	page := listOpenCodeV2Page(db, 0, 10)
	if len(page.Sessions) != 1 || page.Sessions[0].ID != "ses_1" || page.Sessions[0].Cwd != "C:/repo" {
		t.Fatalf("unexpected page %+v", page.Sessions)
	}

	parsed, err := parseOpenCodeV2Session(db, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	kinds := []domain.EventKind{domain.EventUserMessage, domain.EventAgentThought, domain.EventToolCall, domain.EventToolResult, domain.EventAgentMessage}
	if len(parsed.Events) != len(kinds) {
		t.Fatalf("got %d events", len(parsed.Events))
	}
	for i, kind := range kinds {
		if parsed.Events[i].Kind != kind {
			t.Fatalf("event %d is %s, want %s", i, parsed.Events[i].Kind, kind)
		}
	}
	if tool := parsed.Events[3].Tool; tool.Name != "bash" || tool.Output != "ok" || tool.Status != domain.ToolCompleted {
		t.Fatalf("unexpected tool result %+v", tool)
	}
	if parsed.Model != "big-pickle" || parsed.Prompt != "run the tests" {
		t.Fatalf("unexpected session meta %+v", parsed)
	}
}
