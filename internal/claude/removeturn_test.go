package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func record(fields map[string]any) string {
	encoded, _ := json.Marshal(fields)
	return string(encoded)
}

func prompt(uuid, parent, promptID, text string) string {
	return record(map[string]any{"type": "user", "uuid": uuid, "parentUuid": nullable(parent), "promptId": promptID, "message": map[string]any{"role": "user", "content": text}})
}

func reply(uuid, parent, text string) string {
	return record(map[string]any{"type": "assistant", "uuid": uuid, "parentUuid": parent, "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
}

func toolResult(uuid, parent, promptID string) string {
	return record(map[string]any{"type": "user", "uuid": uuid, "parentUuid": parent, "promptId": promptID, "toolUseResult": map[string]any{}, "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t1"}}}})
}

func nullable(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func chain(t *testing.T, path string) []string {
	t.Helper()
	lines, err := readSession(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := map[string]string{}
	last := ""
	for _, line := range lines {
		if line.uuid != "" {
			parent[line.uuid] = line.parent
			last = line.uuid
		}
	}
	var out []string
	for id := last; id != ""; id = parent[id] {
		out = append([]string{id}, out...)
	}
	return out
}

func TestRemoveTurnRelinksTheChain(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "first question"),
		reply("a1", "u1", "first answer"),
		`{"type":"queue-operation","operation":"enqueue"}`,
		prompt("u2", "a1", "p2", "wrong   chat\nsent here"),
		reply("a2", "u2", "calling a tool"),
		toolResult("r2", "a2", "p2"),
		reply("a3", "r2", "done with wrong chat"),
		prompt("u3", "a3", "p3", "real follow up"),
		reply("a4", "u3", "follow up answer"),
	)

	if err := removeTurnFromFile(path, "wrong chat sent here", 0); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(chain(t, path), ",")
	if got != "u1,a1,u3,a4" {
		t.Fatalf("chain = %s", got)
	}
	backups, _ := filepath.Glob(path + ".bak-*")
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %v", backups)
	}
}

func TestRemoveTurnTakesItsQueueRecordsAlong(t *testing.T) {
	path := writeLines(t,
		`{"type":"queue-operation","operation":"enqueue","content":"keep one"}`,
		prompt("u1", "", "p1", "keep one"),
		reply("a1", "u1", "fine"),
		`{"type":"queue-operation","operation":"enqueue","content":"drop me"}`,
		`{"type":"queue-operation","operation":"dequeue"}`,
		prompt("u2", "a1", "p2", "drop me"),
		reply("a2", "u2", "dropped"),
		`{"type":"queue-operation","operation":"enqueue","content":"keep two"}`,
		`{"type":"queue-operation","operation":"dequeue"}`,
		prompt("u3", "a2", "p3", "keep two"),
		reply("a3", "u3", "kept"),
	)
	if err := removeTurnFromFile(path, "drop me", 0); err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(path)
	text := string(payload)
	if strings.Contains(text, "drop me") || strings.Count(text, "keep two") != 2 || strings.Count(text, "dequeue") != 1 {
		t.Fatalf("unexpected result:\n%s", text)
	}
}

func TestRemoveTurnAtTheEndMovesTheLeaf(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "first question"),
		reply("a1", "u1", "first answer"),
		record(map[string]any{"type": "last-prompt", "leafUuid": "a1"}),
		prompt("u2", "a1", "p2", "oops"),
		reply("a2", "u2", "answer to oops"),
		record(map[string]any{"type": "last-prompt", "leafUuid": "a2"}),
	)
	if err := removeTurnFromFile(path, "oops", 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chain(t, path), ","); got != "u1,a1" {
		t.Fatalf("chain = %s", got)
	}
	payload, _ := os.ReadFile(path)
	if strings.Contains(string(payload), "oops") {
		t.Fatalf("message still present:\n%s", payload)
	}
}

func TestRemoveTurnPicksTheRightRepeat(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "continue"),
		reply("a1", "u1", "one"),
		prompt("u2", "a1", "p2", "continue"),
		reply("a2", "u2", "two"),
		prompt("u3", "a2", "p3", "continue"),
		reply("a3", "u3", "three"),
	)
	if err := removeTurnFromFile(path, "continue", 1); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chain(t, path), ","); got != "u1,a1,u3,a3" {
		t.Fatalf("chain = %s", got)
	}
}

func TestRemoveTurnRefusesCompactedMessages(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "old message"),
		reply("a1", "u1", "old answer"),
		record(map[string]any{"type": "system", "subtype": "compact_boundary", "uuid": "c1", "parentUuid": nil}),
		prompt("u2", "c1", "p2", "new message"),
	)
	if err := removeTurnFromFile(path, "old message", 0); !errors.Is(err, ErrTurnCompacted) {
		t.Fatalf("err = %v", err)
	}
	if err := removeTurnFromFile(path, "new message", 0); err != nil {
		t.Fatalf("message after the boundary should be removable: %v", err)
	}
}

func TestRemoveTurnFailsWhenMessageIsMissing(t *testing.T) {
	path := writeLines(t, prompt("u1", "", "p1", "hello"), reply("a1", "u1", "hi"))
	if err := removeTurnFromFile(path, "never sent", 0); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRemoveTurnCountsRepeatsFromTheEnd(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "continue"),
		reply("a1", "u1", "one"),
		prompt("u2", "a1", "p2", "continue"),
		reply("a2", "u2", "two"),
		prompt("u3", "a2", "p3", "continue"),
		reply("a3", "u3", "three"),
	)
	if err := removeTurnFromFile(path, "continue", 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chain(t, path), ","); got != "u1,a1,u2,a2" {
		t.Fatalf("chain = %s", got)
	}
}

func TestRemoveTurnIgnoresHandoffCopies(t *testing.T) {
	path := writeLines(t,
		prompt("u1", "", "p1", "This conversation was continued.\n--- Prior conversation ---\nUser: fix the login bug\n--- Current request ---\n\nnow add tests"),
		reply("a1", "u1", "added tests"),
		prompt("u2", "a1", "p2", "fix the login bug"),
		reply("a2", "u2", "fixed"),
	)
	if err := removeTurnFromFile(path, "fix the login bug", 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chain(t, path), ","); got != "u1,a1" {
		t.Fatalf("chain = %s", got)
	}
}
