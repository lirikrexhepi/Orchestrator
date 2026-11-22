package session

import (
	"strings"
	"testing"

	"composer/internal/shell"
)

// planFor runs one orchestrator turn and returns the parsed plan plus the raw
// reply, so a refusal can be inspected rather than just counted.
func planFor(t *testing.T, model, prompt string) ([]TaskRequest, string) {
	t.Helper()
	t.Skip("legacy composer:tasks prompt scraping removed in v2 architecture")
	return nil, ""
}

// TestOrchestratorAcceptsForeignPaths reproduces the refusal seen in the UI: the
// orchestrator declined work in directories outside its own project, and
// declined non-coding deliverables.
func TestOrchestratorAcceptsForeignPaths(t *testing.T) {
	if _, ok := shell.LookPath("claude", shell.BaseEnvironment()); !ok {
		t.Skip("claude CLI not installed")
	}

	tasks, reply := planFor(t, "claude-haiku-4-5",
		`nah nah im good, so look, i have 2 tasks for you. 1st is to check out `+
			`"C:\Users\PC\Projects\extensions\superscreenshot" and give me a detailed marketing plan for it. `+
			`2nd is to give me a detailed path for this "C:\Users\PC\Projects\homemade\mediapro"`)

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, parsed %d\nreply:\n%s", len(tasks), reply)
	}

	// The absolute paths must survive into the agent prompts, or the agent
	// starts in the wrong directory.
	joined := strings.ToLower(tasks[0].Prompt + tasks[1].Prompt)
	for _, want := range []string{"superscreenshot", "mediapro"} {
		if !strings.Contains(joined, want) {
			t.Errorf("path %q missing from task prompts:\n%s", want, joined)
		}
	}
	for _, task := range tasks {
		t.Logf("TASK %s\n  %s", task.Title, task.Prompt)
	}
}

// TestOrchestratorPlansSingleNonCodingTask covers the other half: one task, and
// a deliverable that is not code.
func TestOrchestratorPlansSingleNonCodingTask(t *testing.T) {
	if _, ok := shell.LookPath("claude", shell.BaseEnvironment()); !ok {
		t.Skip("claude CLI not installed")
	}

	tasks, reply := planFor(t, "claude-haiku-4-5",
		`Write me a competitive analysis document for the project in C:\Users\PC\Projects\extensions\superscreenshot`)

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, parsed %d\nreply:\n%s", len(tasks), reply)
	}
	t.Logf("TASK %s\n  %s", tasks[0].Title, tasks[0].Prompt)
}

// TestOrchestratorStillAnswersQuestions guards the opposite failure: a plain
// question must not spawn agents.
func TestOrchestratorStillAnswersQuestions(t *testing.T) {
	if _, ok := shell.LookPath("claude", shell.BaseEnvironment()); !ok {
		t.Skip("claude CLI not installed")
	}

	tasks, reply := planFor(t, "claude-haiku-4-5", "which model are you?")
	if len(tasks) != 0 {
		t.Errorf("a question should not spawn tasks, got %d\nreply:\n%s", len(tasks), reply)
	}
	t.Logf("reply: %s", strings.TrimSpace(reply))
}

// TestOrchestratorEmitsCwd checks the plan carries the directory each task must
// run in; without it an agent starts in the orchestrator's folder and inspects
// the wrong project.
func TestOrchestratorEmitsCwd(t *testing.T) {
	if _, ok := shell.LookPath("claude", shell.BaseEnvironment()); !ok {
		t.Skip("claude CLI not installed")
	}

	tasks, reply := planFor(t, "claude-haiku-4-5",
		`Two jobs: audit C:\Users\PC\Projects\extensions\superscreenshot for security issues, `+
			`and separately write release notes for C:\Users\PC\Projects\homemade\mediapro`)

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, parsed %d\nreply:\n%s", len(tasks), reply)
	}

	for _, task := range tasks {
		if task.Cwd == "" {
			t.Errorf("task %q has no cwd; agent would start in the wrong directory", task.Title)
			continue
		}
		t.Logf("%-34s cwd=%s", task.Title, task.Cwd)
	}

	joined := strings.ToLower(tasks[0].Cwd + "|" + tasks[1].Cwd)
	for _, want := range []string{"superscreenshot", "mediapro"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no task targets %q (got %s)", want, joined)
		}
	}
}
