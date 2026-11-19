package domain

import (
	"testing"
)

func TestNormalizeToolCall_Antigravity(t *testing.T) {
	t.Run("read_file", func(t *testing.T) {
		tool := &ToolCall{
			Name: "read_file",
			Input: map[string]any{
				"Path": "C:\\repo\\go.mod",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryRead {
			t.Fatalf("expected category %s, got %s", ToolCategoryRead, tool.Category)
		}
		if tool.Target != "C:\\repo\\go.mod" {
			t.Fatalf("expected target C:\\repo\\go.mod, got %s", tool.Target)
		}
	})

	t.Run("edit_file", func(t *testing.T) {
		tool := &ToolCall{
			Name: "edit_file",
			Input: map[string]any{
				"Path":               "a.go",
				"TargetContent":      "old",
				"ReplacementContent": "new",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryEdit {
			t.Fatalf("expected category %s, got %s", ToolCategoryEdit, tool.Category)
		}
		if tool.Target != "a.go" {
			t.Fatalf("expected target a.go, got %s", tool.Target)
		}
		if len(tool.Diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(tool.Diffs))
		}
		if tool.Diffs[0].Path != "a.go" || tool.Diffs[0].OldText != "old" || tool.Diffs[0].NewText != "new" {
			t.Fatalf("unexpected diff: %+v", tool.Diffs[0])
		}
	})

	t.Run("write_file", func(t *testing.T) {
		tool := &ToolCall{
			Name: "write_file",
			Input: map[string]any{
				"Path":               "a.go",
				"TargetContent":      "old",
				"ReplacementContent": "new",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryWrite {
			t.Fatalf("expected category %s, got %s", ToolCategoryWrite, tool.Category)
		}
		if tool.Target != "a.go" {
			t.Fatalf("expected target a.go, got %s", tool.Target)
		}
		if len(tool.Diffs) != 1 {
			t.Fatalf("expected 1 diff, got %d", len(tool.Diffs))
		}
		if tool.Diffs[0].Path != "a.go" || tool.Diffs[0].OldText != "old" || tool.Diffs[0].NewText != "new" {
			t.Fatalf("unexpected diff: %+v", tool.Diffs[0])
		}
	})
}

func TestNormalizeToolCall_Claude(t *testing.T) {
	t.Run("Edit", func(t *testing.T) {
		tool := &ToolCall{
			Name: "Edit",
			Input: map[string]any{
				"file_path":  "internal/main.go",
				"old_string": "hello",
				"new_string": "world",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryEdit {
			t.Fatalf("expected category %s, got %s", ToolCategoryEdit, tool.Category)
		}
		if tool.Target != "internal/main.go" {
			t.Fatalf("expected target internal/main.go, got %s", tool.Target)
		}
		if len(tool.Diffs) != 1 || tool.Diffs[0].OldText != "hello" || tool.Diffs[0].NewText != "world" {
			t.Fatalf("unexpected diffs: %+v", tool.Diffs)
		}
	})

	t.Run("MultiEdit", func(t *testing.T) {
		tool := &ToolCall{
			Name: "MultiEdit",
			Input: map[string]any{
				"file_path": "internal/main.go",
				"edits": []any{
					map[string]any{"old_string": "foo", "new_string": "bar"},
					map[string]any{"old_string": "baz", "new_string": "qux"},
				},
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryEdit {
			t.Fatalf("expected category %s, got %s", ToolCategoryEdit, tool.Category)
		}
		if tool.Target != "internal/main.go" {
			t.Fatalf("expected target internal/main.go, got %s", tool.Target)
		}
		if len(tool.Diffs) != 2 {
			t.Fatalf("expected 2 diffs, got %d", len(tool.Diffs))
		}
		if tool.Diffs[0].OldText != "foo" || tool.Diffs[0].NewText != "bar" {
			t.Fatalf("unexpected diff 0: %+v", tool.Diffs[0])
		}
		if tool.Diffs[1].OldText != "baz" || tool.Diffs[1].NewText != "qux" {
			t.Fatalf("unexpected diff 1: %+v", tool.Diffs[1])
		}
	})

	t.Run("Write", func(t *testing.T) {
		tool := &ToolCall{
			Name: "Write",
			Input: map[string]any{
				"file_path": "test.txt",
				"content":   "some content",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryWrite {
			t.Fatalf("expected category %s, got %s", ToolCategoryWrite, tool.Category)
		}
		if tool.Target != "test.txt" {
			t.Fatalf("expected target test.txt, got %s", tool.Target)
		}
		if len(tool.Diffs) != 1 || tool.Diffs[0].OldText != "" || tool.Diffs[0].NewText != "some content" {
			t.Fatalf("unexpected diffs: %+v", tool.Diffs)
		}
	})

	t.Run("Bash", func(t *testing.T) {
		tool := &ToolCall{
			Name: "Bash",
			Input: map[string]any{
				"command": "npm test",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryCommand {
			t.Fatalf("expected category %s, got %s", ToolCategoryCommand, tool.Category)
		}
		if tool.Target != "npm test" {
			t.Fatalf("expected target npm test, got %s", tool.Target)
		}
	})

	t.Run("Grep", func(t *testing.T) {
		tool := &ToolCall{
			Name: "Grep",
			Input: map[string]any{
				"pattern": "TODO.*refactor",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategorySearch {
			t.Fatalf("expected category %s, got %s", ToolCategorySearch, tool.Category)
		}
		if tool.Target != "TODO.*refactor" {
			t.Fatalf("expected target TODO.*refactor, got %s", tool.Target)
		}
	})

	t.Run("Task", func(t *testing.T) {
		tool := &ToolCall{
			Name: "Task",
			Input: map[string]any{
				"description": "explore codebase",
				"prompt":      "search for auth modules",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategorySubagent {
			t.Fatalf("expected category %s, got %s", ToolCategorySubagent, tool.Category)
		}
	})
}

func TestNormalizeToolCall_OpenCode_And_Codex(t *testing.T) {
	t.Run("OpenCode read", func(t *testing.T) {
		tool := &ToolCall{
			Name: "read",
			Input: map[string]any{
				"filePath":  "pkg/api.go",
				"startLine": 10,
				"endLine":   25,
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryRead {
			t.Fatalf("expected category %s, got %s", ToolCategoryRead, tool.Category)
		}
		if tool.Target != "pkg/api.go:10-25" {
			t.Fatalf("expected target pkg/api.go:10-25, got %s", tool.Target)
		}
	})

	t.Run("OpenCode bash", func(t *testing.T) {
		tool := &ToolCall{
			Name: "bash",
			Input: map[string]any{
				"command": "git status",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryCommand {
			t.Fatalf("expected category %s, got %s", ToolCategoryCommand, tool.Category)
		}
		if tool.Target != "git status" {
			t.Fatalf("expected target git status, got %s", tool.Target)
		}
	})

	t.Run("OpenCode glob", func(t *testing.T) {
		tool := &ToolCall{
			Name: "glob",
			Input: map[string]any{
				"pattern": "**/*.ts",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategorySearch {
			t.Fatalf("expected category %s, got %s", ToolCategorySearch, tool.Category)
		}
		if tool.Target != "**/*.ts" {
			t.Fatalf("expected target **/*.ts, got %s", tool.Target)
		}
	})

	t.Run("Codex commandExecution", func(t *testing.T) {
		tool := &ToolCall{
			Kind: "commandExecution",
			Input: map[string]any{
				"commandLine": "go build ./...",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryCommand {
			t.Fatalf("expected category %s, got %s", ToolCategoryCommand, tool.Category)
		}
		if tool.Target != "go build ./..." {
			t.Fatalf("expected target go build ./..., got %s", tool.Target)
		}
	})

	t.Run("Codex fileChange", func(t *testing.T) {
		tool := &ToolCall{
			Kind: "fileChange",
			Input: map[string]any{
				"filePath": "config.json",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryEdit {
			t.Fatalf("expected category %s, got %s", ToolCategoryEdit, tool.Category)
		}
		if tool.Target != "config.json" {
			t.Fatalf("expected target config.json, got %s", tool.Target)
		}
	})
}

func TestNormalizeToolCall_PreserveExistingDiffs(t *testing.T) {
	existingDiffs := []FileDiff{
		{
			Path:    "adapter/computed.go",
			OldText: "original adapter text",
			NewText: "modified adapter text",
		},
	}
	tool := &ToolCall{
		Name:  "edit_file",
		Diffs: existingDiffs,
		Input: map[string]any{
			"Path":               "input/computed.go",
			"TargetContent":      "different old",
			"ReplacementContent": "different new",
		},
	}
	NormalizeToolCall(tool)
	if tool.Category != ToolCategoryEdit {
		t.Fatalf("expected category %s, got %s", ToolCategoryEdit, tool.Category)
	}
	if len(tool.Diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(tool.Diffs))
	}
	if tool.Diffs[0].Path != "adapter/computed.go" || tool.Diffs[0].OldText != "original adapter text" {
		t.Fatalf("adapter diffs were overwritten: %+v", tool.Diffs)
	}
}

func TestNormalizeEvent(t *testing.T) {
	ev := &RuntimeEvent{
		Kind: EventToolCall,
		Tool: &ToolCall{
			Name: "run_command",
			Input: map[string]any{
				"CommandLine": "echo 123",
				"toolSummary": "Run echo",
			},
		},
	}
	NormalizeEvent(ev)
	if ev.Tool.Category != ToolCategoryCommand {
		t.Fatalf("expected category %s, got %s", ToolCategoryCommand, ev.Tool.Category)
	}
	if ev.Tool.Target != "echo 123" {
		t.Fatalf("expected target 'echo 123', got %s", ev.Tool.Target)
	}
	if ev.Tool.Summary != "Run echo" {
		t.Fatalf("expected summary 'Run echo', got %s", ev.Tool.Summary)
	}

	// Idempotent second run
	NormalizeEvent(ev)
	if ev.Tool.Category != ToolCategoryCommand || ev.Tool.Target != "echo 123" || ev.Tool.Summary != "Run echo" {
		t.Fatalf("second call mutated event: %+v", ev.Tool)
	}
}

func TestNormalizeToolCall_OrchestratorMCP(t *testing.T) {
	t.Run("bare delegate_task with driver", func(t *testing.T) {
		tool := &ToolCall{
			Name: "delegate_task",
			Input: map[string]any{
				"title":  "Explore tests",
				"driver": "claude",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategorySubagent {
			t.Fatalf("expected category %s, got %s", ToolCategorySubagent, tool.Category)
		}
		if tool.Target != "Explore tests · claude" {
			t.Fatalf("expected target 'Explore tests · claude', got %q", tool.Target)
		}
		if tool.Summary != "Delegated task" {
			t.Fatalf("expected summary 'Delegated task', got %q", tool.Summary)
		}
	})

	t.Run("prefixed mcp__orchestrator__delegate_task without driver", func(t *testing.T) {
		tool := &ToolCall{
			Name: "mcp__orchestrator__delegate_task",
			Input: map[string]any{
				"title": "Subagent refactor",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategorySubagent {
			t.Fatalf("expected category %s, got %s", ToolCategorySubagent, tool.Category)
		}
		if tool.Target != "Subagent refactor" {
			t.Fatalf("expected target 'Subagent refactor', got %q", tool.Target)
		}
		if tool.Summary != "Delegated task" {
			t.Fatalf("expected summary 'Delegated task', got %q", tool.Summary)
		}
	})

	t.Run("prefixed mcp__orchestrator__read_thread", func(t *testing.T) {
		tool := &ToolCall{
			Name: "mcp__orchestrator__read_thread",
			Input: map[string]any{
				"threadId": "th_12345",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryMCP {
			t.Fatalf("expected category %s, got %s", ToolCategoryMCP, tool.Category)
		}
		if tool.Target != "th_12345" {
			t.Fatalf("expected target 'th_12345', got %q", tool.Target)
		}
		if tool.Summary != "Read thread" {
			t.Fatalf("expected summary 'Read thread', got %q", tool.Summary)
		}
	})

	t.Run("prefixed mcp__orchestrator__send_to_thread", func(t *testing.T) {
		tool := &ToolCall{
			Name: "mcp__orchestrator__send_to_thread",
			Input: map[string]any{
				"threadId": "th_abcde",
				"message":  "Please run tests",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryMCP {
			t.Fatalf("expected category %s, got %s", ToolCategoryMCP, tool.Category)
		}
		if tool.Target != "th_abcde" {
			t.Fatalf("expected target 'th_abcde', got %q", tool.Target)
		}
		if tool.Summary != "Messaged thread" {
			t.Fatalf("expected summary 'Messaged thread', got %q", tool.Summary)
		}
	})

	t.Run("orchestrator:stop_thread", func(t *testing.T) {
		tool := &ToolCall{
			Name: "orchestrator:stop_thread",
			Input: map[string]any{
				"threadId": "th_999",
			},
		}
		NormalizeToolCall(tool)
		if tool.Category != ToolCategoryMCP {
			t.Fatalf("expected category %s, got %s", ToolCategoryMCP, tool.Category)
		}
		if tool.Target != "th_999" {
			t.Fatalf("expected target 'th_999', got %q", tool.Target)
		}
		if tool.Summary != "Stopped thread" {
			t.Fatalf("expected summary 'Stopped thread', got %q", tool.Summary)
		}
	})

	t.Run("orchestrator_capabilities and list_threads empty target", func(t *testing.T) {
		caps := &ToolCall{
			Name:  "orchestrator_capabilities",
			Input: map[string]any{},
		}
		NormalizeToolCall(caps)
		if caps.Category != ToolCategoryMCP {
			t.Fatalf("expected category %s, got %s", ToolCategoryMCP, caps.Category)
		}
		if caps.Target != "" {
			t.Fatalf("expected target '', got %q", caps.Target)
		}
		if caps.Summary != "Orchestrator capabilities" {
			t.Fatalf("expected summary 'Orchestrator capabilities', got %q", caps.Summary)
		}

		list := &ToolCall{
			Name: "list_threads",
			Input: map[string]any{
				"limit": 5,
			},
		}
		NormalizeToolCall(list)
		if list.Category != ToolCategoryMCP {
			t.Fatalf("expected category %s, got %s", ToolCategoryMCP, list.Category)
		}
		if list.Target != "" {
			t.Fatalf("expected target '', got %q", list.Target)
		}
		if list.Summary != "Listed threads" {
			t.Fatalf("expected summary 'Listed threads', got %q", list.Summary)
		}
	})
}
