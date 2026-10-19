package claudeimport

import (
	"os"
	"path/filepath"
	"testing"

	"composer/internal/domain"
)

func writeSample(t *testing.T) string {
	t.Helper()
	lines := []string{
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-09-20T10:00:00.000Z","sessionId":"sess-1"}`,
		`{"type":"ai-title","aiTitle":"Fix login bug","sessionId":"sess-1"}`,
		`{"parentUuid":"00000000-0000-0000-0000-000000000000","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"text","text":"Fix the login bug on the settings page"}]},"uuid":"u1","timestamp":"2026-09-20T10:00:01.000Z","cwd":"C:\\work\\app","sessionId":"sess-1","version":"2.1.0"}`,
		`{"parentUuid":"u1","isSidechain":false,"type":"assistant","message":{"model":"claude-opus-4-6","id":"m1","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"I should look at the settings page first."},{"type":"text","text":"I will look at the settings page."},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"settings.tsx"}}]},"uuid":"a1","timestamp":"2026-09-20T10:00:05.000Z","sessionId":"sess-1"}`,
		`{"parentUuid":"a1","isSidechain":false,"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"export function Settings() {}"}]},"uuid":"u2","timestamp":"2026-09-20T10:00:06.000Z","sessionId":"sess-1"}`,
		`{"parentUuid":"u2","isSidechain":true,"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"subagent internals"}]},"uuid":"a-side","timestamp":"2026-09-20T10:00:07.000Z","sessionId":"sess-1"}`,
		`{"parentUuid":"u2","isSidechain":false,"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Found it, the handler is missing."}],"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":10}},"uuid":"a2","timestamp":"2026-09-20T10:00:08.000Z","sessionId":"sess-1"}`,
		`{"type":"attachment","sessionId":"sess-1"}`,
	}
	path := filepath.Join(t.TempDir(), "sess-1.jsonl")
	payload := ""
	for _, line := range lines {
		payload += line + "\n"
	}
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFileConvertsConversation(t *testing.T) {
	parsed, err := ParseFile(writeSample(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if parsed.SessionID != "sess-1" {
		t.Fatalf("session id = %q", parsed.SessionID)
	}
	if parsed.Title != "Fix login bug" {
		t.Fatalf("title = %q", parsed.Title)
	}
	if parsed.Cwd != `C:\work\app` {
		t.Fatalf("cwd = %q", parsed.Cwd)
	}
	if parsed.Model != "claude-opus-4-6" {
		t.Fatalf("model = %q", parsed.Model)
	}
	kinds := map[domain.EventKind]int{}
	for _, event := range parsed.Events {
		kinds[event.Kind]++
		if event.Driver != domain.DriverClaude {
			t.Fatalf("event driver = %q", event.Driver)
		}
		if event.At == 0 {
			t.Fatalf("event missing timestamp: %+v", event)
		}
	}
	if kinds[domain.EventUserMessage] != 1 {
		t.Fatalf("user messages = %d: %+v", kinds[domain.EventUserMessage], kinds)
	}
	if kinds[domain.EventAgentMessage] != 2 {
		t.Fatalf("agent messages = %d: %+v", kinds[domain.EventAgentMessage], kinds)
	}
	if kinds[domain.EventAgentThought] != 1 {
		t.Fatalf("thoughts = %d: %+v", kinds[domain.EventAgentThought], kinds)
	}
	if kinds[domain.EventToolCall] != 1 {
		t.Fatalf("tool calls = %d: %+v", kinds[domain.EventToolCall], kinds)
	}
	if kinds[domain.EventToolResult] != 1 {
		t.Fatalf("tool results = %d: %+v", kinds[domain.EventToolResult], kinds)
	}
	if kinds[domain.EventUsage] != 1 {
		t.Fatalf("usage = %d: %+v", kinds[domain.EventUsage], kinds)
	}
	for _, event := range parsed.Events {
		if event.Kind == domain.EventAgentMessage && event.Text == "subagent internals" {
			t.Fatalf("sidechain record was not skipped")
		}
	}
	var last uint64
	for _, event := range parsed.Events {
		if event.Seq != last+1 {
			t.Fatalf("seq not contiguous: %+v", parsed.Events)
		}
		last = event.Seq
	}
}

func TestIsAgentRunFlagsOrchestratorAndWorktreeSessions(t *testing.T) {
	if !isAgentRun(&ParsedSession{Prompt: "You are the orchestrator in Composer, plan this."}) {
		t.Errorf("orchestrator prompt not flagged")
	}
	if !isAgentRun(&ParsedSession{Cwd: `C:\Users\PC\AppData\Local\composer\worktrees\001-abc\write-alpha`}) {
		t.Errorf("composer worktree run not flagged")
	}
	if isAgentRun(&ParsedSession{Prompt: "Fix the login bug", Cwd: `C:\Users\PC\Projects\hobby\orchestrator`}) {
		t.Errorf("hand-written project chat flagged")
	}
}

func TestListFindsSampleSession(t *testing.T) {

	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "proj")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := "{\"parentUuid\":\"x\",\"isSidechain\":false,\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"hello\"},\"uuid\":\"u1\",\"timestamp\":\"2026-09-20T10:00:01.000Z\",\"cwd\":\"C:\\\\w\",\"sessionId\":\"s-9\"}\n"
	if err := os.WriteFile(filepath.Join(projects, "s-9.jsonl"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %+v", sessions)
	}
	if sessions[0].ID != "s-9" || sessions[0].Title != "hello" {
		t.Fatalf("session = %+v", sessions[0])
	}
}

func TestListSkipsTranscriptsWithoutHumanText(t *testing.T) {
	dir := t.TempDir()
	projects := filepath.Join(dir, "projects", "proj")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	assistantOnly := "{\"isSidechain\":false,\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"done\"}]},\"uuid\":\"a1\",\"timestamp\":\"2026-09-20T10:00:01.000Z\",\"cwd\":\"C:\\\\w\",\"sessionId\":\"s-a\"}\n"
	toolResultOnly := "{\"isSidechain\":false,\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"t1\",\"content\":\"ok\"}]},\"uuid\":\"u1\",\"timestamp\":\"2026-09-20T10:00:01.000Z\",\"cwd\":\"C:\\\\w\",\"sessionId\":\"s-t\"}\n"
	if err := os.WriteFile(filepath.Join(projects, "s-a.jsonl"), []byte(assistantOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "s-t.jsonl"), []byte(toolResultOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %+v, want none", sessions)
	}
}

func writeSubagentSession(t *testing.T, withNotification bool) string {
	t.Helper()
	dir := t.TempDir()
	lines := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"run agents"}]},"uuid":"u1","timestamp":"2026-09-20T10:00:00.000Z","sessionId":"sess-2"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_fg","name":"Agent","input":{"description":"foreground"}},{"type":"tool_use","id":"toolu_bg","name":"Agent","input":{"description":"background","run_in_background":true}}]},"uuid":"a1","timestamp":"2026-09-20T10:00:01.000Z","sessionId":"sess-2"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bg","content":"Async agent launched"}]},"uuid":"u2","timestamp":"2026-09-20T10:00:02.000Z","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"bgagent"},"sessionId":"sess-2"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_fg","content":"report"}]},"uuid":"u3","timestamp":"2026-09-20T10:01:01.000Z","toolUseResult":{"status":"completed","totalDurationMs":60000,"totalToolUseCount":4},"sessionId":"sess-2"}`,
	}
	if withNotification {
		lines = append(lines, `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-09-20T10:20:00.000Z","sessionId":"sess-2","content":"<task-notification>\n<task-id>bgagent</task-id>\n<tool-use-id>toolu_bg</tool-use-id>\n<status>completed</status>\n<usage><subagent_tokens>9</subagent_tokens><tool_uses>2</tool_uses><duration_ms>1100000</duration_ms></usage>\n</task-notification>"}`)
	}
	payload := ""
	for _, line := range lines {
		payload += line + "\n"
	}
	path := filepath.Join(dir, "sess-2.jsonl")
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sess-2", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-bgagent.meta.json"), []byte(`{"toolUseId":"toolu_bg"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	transcript := `{"isSidechain":true,"type":"user","message":{"role":"user","content":"do the thing"},"uuid":"s0","timestamp":"2026-09-20T10:00:03.000Z"}
{"isSidechain":true,"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","id":"toolu_s1","name":"Read","input":{"file_path":"a.go"}}]},"uuid":"s1","timestamp":"2026-09-20T10:05:00.000Z"}
{"isSidechain":true,"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_s1","content":"package a"}]},"uuid":"s2","timestamp":"2026-09-20T10:05:01.000Z"}
{"isSidechain":true,"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_s2","name":"Bash","input":{"command":"ls"}}]},"uuid":"s3","timestamp":"2026-09-20T10:15:00.000Z"}
`
	if err := os.WriteFile(filepath.Join(sub, "agent-bgagent.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFileMergesSubagentTranscriptsAndNotificationTotals(t *testing.T) {
	parsed, err := ParseFile(writeSubagentSession(t, true))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	subCalls, subResults, subMessages := 0, 0, 0
	var launch, finish *domain.RuntimeEvent
	var fg *domain.RuntimeEvent
	var callAt int64
	for i := range parsed.Events {
		event := &parsed.Events[i]
		switch event.Kind {
		case domain.EventSubagentToolCall:
			subCalls++
			if event.ParentToolID != "toolu_bg" {
				t.Fatalf("parent = %q", event.ParentToolID)
			}
		case domain.EventSubagentToolDone:
			subResults++
		case domain.EventSubagentMessage:
			subMessages++
		case domain.EventToolCall:
			if event.Tool.ID == "toolu_bg" {
				callAt = event.At
			}
		case domain.EventToolResult:
			switch {
			case event.Tool.ID == "toolu_fg":
				fg = event
			case event.Tool.Async:
				launch = event
			case event.Tool.ID == "toolu_bg":
				finish = event
			}
		}
	}
	if subCalls != 2 || subResults != 1 || subMessages != 1 {
		t.Fatalf("subagent events calls=%d results=%d messages=%d", subCalls, subResults, subMessages)
	}
	if launch == nil || finish == nil || fg == nil {
		t.Fatalf("missing results launch=%v finish=%v fg=%v", launch, finish, fg)
	}
	if finish.Tool.ToolCount != 2 || finish.Tool.DurationMs != 1100000 || finish.At <= launch.At {
		t.Fatalf("finish = %+v at=%d launch at=%d", finish.Tool, finish.At, launch.At)
	}
	if fg.Tool.ToolCount != 4 || fg.Tool.DurationMs != 60000 || fg.At-callAt < 59000 {
		t.Fatalf("foreground = %+v", fg.Tool)
	}
	for i := 1; i < len(parsed.Events); i++ {
		if parsed.Events[i].At < parsed.Events[i-1].At {
			t.Fatalf("events out of order at %d", i)
		}
	}
}

func TestParseFileClosesLaunchedAgentWithoutNotificationAtLastActivity(t *testing.T) {
	parsed, err := ParseFile(writeSubagentSession(t, false))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var launchAt, endAt int64
	for _, event := range parsed.Events {
		if event.Kind != domain.EventToolResult || event.Tool.ID != "toolu_bg" {
			continue
		}
		if event.Tool.Async {
			launchAt = event.At
		} else {
			endAt = event.At
		}
	}
	if launchAt == 0 || endAt-launchAt < 10*60*1000 {
		t.Fatalf("launch=%d end=%d", launchAt, endAt)
	}
}
