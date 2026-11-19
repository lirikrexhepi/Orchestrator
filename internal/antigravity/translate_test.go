package antigravity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"composer/internal/domain"
)

const deniedToolFrame = `{"event":"step_update","step_update":{"conversation_id":"75dbb7cb","step_index":3,"state":"ERROR","step_type":"tool","tool_name":"run_command","duration_seconds":0.0124,"tool_info":{"name":"run_command","parameters":{"CommandLine":"echo hi"},"error":{"type":"TOOL_ERROR","message":"permission check failed for command \"echo hi\": user denied permission to run command:\necho hi"}}}}`

const timeoutResultFrame = `{"event":"result","result":{"conversation_id":"fb5af3b8","status":"ERROR","response":"","error":"timeout waiting for response","duration_seconds":2.23,"num_turns":1,"usage":{"input_tokens":0,"output_tokens":0}}}`

const successResultFrame = `{"event":"result","result":{"conversation_id":"b68fef7f","status":"SUCCESS","response":"HELLO\n","num_turns":1,"usage":{"input_tokens":14954,"output_tokens":89}}}`

func newTestAdapter() (*Adapter, *session, *[]domain.RuntimeEvent) {
	var captured []domain.RuntimeEvent
	emit := providerEmitter(func(event domain.RuntimeEvent) {
		captured = append(captured, event)
	})
	a := NewAdapter(domain.ProviderSettings{}, emit)
	s := &session{threadID: "t1", cwd: `C:\repo`, permission: domain.PermissionBypass, tools: make(map[int]string), baselines: make(map[string]string)}
	return a, s, &captured
}

type providerEmitter func(domain.RuntimeEvent)

func (f providerEmitter) Emit(e domain.RuntimeEvent) { f(e) }

func decode(t *testing.T, line string) *Envelope {
	t.Helper()
	var envelope Envelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("frame must decode, got %v", err)
	}
	return &envelope
}

func TestDeniedToolFrameSurfacesAsFailure(t *testing.T) {
	a, s, captured := newTestAdapter()
	a.handleEnvelope(s, "turn-1", decode(t, deniedToolFrame))

	if len(*captured) != 1 {
		t.Fatalf("want 1 event, got %d", len(*captured))
	}
	event := (*captured)[0]
	if event.Tool == nil {
		t.Fatal("event carries no tool")
	}
	if event.Tool.Status != domain.ToolFailed {
		t.Errorf("status = %q, want %q", event.Tool.Status, domain.ToolFailed)
	}
	if !strings.Contains(event.Tool.Output, "user denied permission") {
		t.Errorf("output lost the reason: %q", event.Tool.Output)
	}
	if event.Tool.Input["CommandLine"] != "echo hi" {
		t.Errorf("input = %v, want CommandLine echo hi", event.Tool.Input)
	}
}

func TestTimeoutResultSurfacesAsTurnFailure(t *testing.T) {
	a, s, captured := newTestAdapter()
	a.handleEnvelope(s, "turn-1", decode(t, timeoutResultFrame))

	var failure *domain.RuntimeEvent
	for i := range *captured {
		if (*captured)[i].Kind == domain.EventTurnFailed {
			failure = &(*captured)[i]
		}
	}
	if failure == nil {
		t.Fatal("no turn.failed emitted")
	}
	if failure.Error != "timeout waiting for response" {
		t.Errorf("error = %q", failure.Error)
	}
}

func TestFinalResponseSurfacesWhenNoDeltasArrived(t *testing.T) {
	a, s, captured := newTestAdapter()
	a.handleEnvelope(s, "turn-1", decode(t, successResultFrame))

	var text string
	for _, event := range *captured {
		if event.Kind == domain.EventAgentMessage {
			text = event.Text
		}
	}
	if text != "HELLO\n" {
		t.Errorf("agent message = %q, want HELLO", text)
	}
}

func TestFinalResponseNotDuplicatedAfterDeltas(t *testing.T) {
	a, s, captured := newTestAdapter()
	s.markText("HELLO\n")
	a.handleEnvelope(s, "turn-1", decode(t, successResultFrame))

	for _, event := range *captured {
		if event.Kind == domain.EventAgentMessage {
			t.Fatalf("streamed turn re-emitted its response: %q", event.Text)
		}
	}
}

func TestFinalResponseEmittedWhenDeltasWereOnlyNarration(t *testing.T) {
	a, s, captured := newTestAdapter()
	s.markText("Working on it...")
	a.handleEnvelope(s, "turn-1", decode(t, successResultFrame))

	var text string
	for _, event := range *captured {
		if event.Kind == domain.EventAgentMessage {
			text = event.Text
		}
	}
	if text != "HELLO\n" {
		t.Errorf("distinct final response lost, got %q", text)
	}
}

func TestUnknownStepTypeTextDeltaIsKept(t *testing.T) {
	a, s, captured := newTestAdapter()
	line := `{"event":"step_update","step_update":{"conversation_id":"abc","step_index":1,"state":"ACTIVE","step_type":"narrate","text_delta":"hello there"}}`
	a.handleEnvelope(s, "turn-1", decode(t, line))

	var text string
	for _, event := range *captured {
		if event.Kind == domain.EventAgentMessage {
			text += event.Text
		}
	}
	if text != "hello there" {
		t.Errorf("unknown step text lost, got %q", text)
	}
	if _, _, sent := s.streamedText(); !sent {
		t.Error("unknown step text did not mark the turn as text-bearing")
	}
}

func TestBuildArgsPinsWorkspaceAndTimeout(t *testing.T) {
	a, s, _ := newTestAdapter()
	args := strings.Join(a.buildArgs(s, "hi"), " ")

	for _, want := range []string{`--add-dir C:\repo`, "--print-timeout 30m", "--dangerously-skip-permissions"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
}

func TestBuildArgsKeepsPlanModeReadOnly(t *testing.T) {
	a, s, _ := newTestAdapter()
	s.permission = domain.PermissionPlan
	args := strings.Join(a.buildArgs(s, "hi"), " ")

	if !strings.Contains(args, "--mode plan") {
		t.Errorf("plan mode not requested: %s", args)
	}
	if strings.Contains(args, "--dangerously-skip-permissions") {
		t.Errorf("plan mode must not auto-approve tools: %s", args)
	}
}

func TestBuildArgsOmitsDangerouslySkipPermissionsWhenNotBypass(t *testing.T) {
	a, s, _ := newTestAdapter()
	s.permission = domain.PermissionDefault
	args := strings.Join(a.buildArgs(s, "hi"), " ")

	if strings.Contains(args, "--dangerously-skip-permissions") {
		t.Errorf("default permission mode must not pass --dangerously-skip-permissions: %s", args)
	}
}

func TestReadFileBaselineAndSubsequentEdits(t *testing.T) {
	a, s, captured := newTestAdapter()
	dir := t.TempDir()
	path := filepath.Join(dir, "example.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. read_file with {"Path": "<temp file>"} captures the baseline.
	readParams := map[string]any{"Path": path}
	a.handleEnvelope(s, "turn-1", toolFrame(t, 1, StateActive, "read_file", readParams))
	a.handleEnvelope(s, "turn-1", toolFrame(t, 1, StateDone, "read_file", readParams))

	s.mu.Lock()
	baseline, hasBaseline := s.baselines[path]
	s.mu.Unlock()
	if !hasBaseline || baseline != "alpha\nbeta\ngamma\n" {
		t.Fatalf("read_file failed to capture baseline: got %q (hasBaseline=%v)", baseline, hasBaseline)
	}

	// 2. Subsequent edit_file with {"Path": "<temp file>"} produces populated domain.FileDiff entries on DONE.
	editParams := map[string]any{"Path": path}
	a.handleEnvelope(s, "turn-1", toolFrame(t, 2, StateActive, "edit_file", editParams))
	if err := os.WriteFile(path, []byte("alpha\nmodified\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleEnvelope(s, "turn-1", toolFrame(t, 2, StateDone, "edit_file", editParams))

	editTool := lastTool(t, *captured)
	if len(editTool.Diffs) == 0 {
		t.Fatal("expected populated domain.FileDiff for edit_file, got none")
	}
	if !strings.Contains(editTool.Diffs[0].NewText, "modified") {
		t.Errorf("expected edit diff to contain 'modified', got %+v", editTool.Diffs[0])
	}

	// 3. Subsequent write_file with {"Path": "<temp file>"} produces populated domain.FileDiff entries on DONE.
	writePath := filepath.Join(dir, "created.txt")
	writeParams := map[string]any{"Path": writePath}
	a.handleEnvelope(s, "turn-1", toolFrame(t, 3, StateActive, "write_file", writeParams))
	if err := os.WriteFile(writePath, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleEnvelope(s, "turn-1", toolFrame(t, 3, StateDone, "write_file", writeParams))

	writeTool := lastTool(t, *captured)
	if len(writeTool.Diffs) == 0 {
		t.Fatal("expected populated domain.FileDiff for write_file, got none")
	}
	if writeTool.Diffs[0].NewText != "hello world\n" {
		t.Errorf("expected write diff new text 'hello world\\n', got %+v", writeTool.Diffs[0])
	}
}
