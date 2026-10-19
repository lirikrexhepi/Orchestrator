package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(source Source, label string) *File {
	for i := range source.Files {
		if source.Files[i].Label == label {
			return &source.Files[i]
		}
	}
	return nil
}

func TestCollect(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "code", "app")
	work := filepath.Join(home, "accounts", "work")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENCODE_DISABLE_CLAUDE_CODE", "")

	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "Use tabs.\nSee @extra.md for more.\n```\n@ignored.md\n```\n")
	write(t, filepath.Join(home, ".claude", "extra.md"), "Extra rules")
	write(t, filepath.Join(home, ".claude", "ignored.md"), "never")
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"outputStyle": "terse"}`)
	write(t, filepath.Join(home, ".claude", "output-styles", "terse.md"), "---\nname: terse\n---\nBe brief.")
	write(t, filepath.Join(work, "CLAUDE.md"), "Use spaces.")
	write(t, filepath.Join(project, ".git", "HEAD"), "ref")
	write(t, filepath.Join(project, "CLAUDE.md"), "Project rules")
	write(t, filepath.Join(project, "AGENTS.md"), "Agent rules")
	write(t, filepath.Join(project, ".agents", "rules", "style.md"), "Style")
	write(t, filepath.Join(home, ".claude", "projects", Slug(project), "memory", "MEMORY.md"), "- [x](x.md)")
	write(t, filepath.Join(home, ".claude", "projects", Slug(project), "memory", "x.md"), "fact")
	write(t, filepath.Join(home, ".gemini", "GEMINI.md"), "Gemini global")
	write(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"), "{\n // comment\n \"instructions\": [\"rules/*.md\",],\n}")
	write(t, filepath.Join(home, ".config", "opencode", "rules", "a.md"), "OC rule")

	sources := Collect(Input{
		Home: home,
		Cwd:  project,
		ClaudeAccounts: []ClaudeAccount{
			{ID: "default", Name: "Default", Dir: filepath.Join(home, ".claude"), Default: true, InUse: true},
			{ID: "work", Name: "Work", Dir: work},
		},
		OpenCodeMajor: 1,
		AppNote:       "note",
	})
	if len(sources) != 4 {
		t.Fatalf("got %d sources", len(sources))
	}
	claude, agy, oc, app := sources[0], sources[1], sources[2], sources[3]

	var workFile *File
	for i := range claude.Files {
		if claude.Files[i].Account == "Work" && claude.Files[i].Label == "Global CLAUDE.md" {
			workFile = &claude.Files[i]
		}
	}
	if workFile == nil || !workFile.Warning || !strings.Contains(workFile.Note, "Different from") {
		t.Fatalf("drift not flagged: %+v", workFile)
	}
	if find(claude, "Imported: extra.md") == nil {
		t.Fatal("import not followed")
	}
	if find(claude, "Imported: ignored.md") != nil {
		t.Fatal("import inside a code fence must be ignored")
	}
	if style := find(claude, "Output style: terse"); style == nil || style.Content == "" {
		t.Fatal("output style missing")
	}
	if find(claude, "CLAUDE.md") == nil || find(claude, "MEMORY.md") == nil || find(claude, "x.md") == nil {
		t.Fatalf("project or memory files missing: %+v", claude.Files)
	}
	if claude.Files[len(claude.Files)-2].Label != "MEMORY.md" {
		t.Fatal("MEMORY.md must come first in auto memory")
	}

	if find(agy, "Global GEMINI.md") == nil || find(agy, "AGENTS.md") == nil || find(agy, "Rule: .agents/rules/style.md") == nil {
		t.Fatalf("antigravity files missing: %+v", agy.Files)
	}

	if global := find(oc, "Global AGENTS.md"); global == nil || global.Exists {
		t.Fatal("OpenCode global AGENTS.md placeholder missing")
	}
	if find(oc, "Claude's global CLAUDE.md") == nil {
		t.Fatal("OpenCode 1 CLAUDE.md fallback missing")
	}
	if find(oc, "rules/a.md") == nil || find(oc, "AGENTS.md") == nil {
		t.Fatalf("OpenCode instructions missing: %+v", oc.Files)
	}
	if len(app.Files) != 1 || app.Files[0].Content != "note" {
		t.Fatal("app note missing")
	}

	v2 := Collect(Input{Home: home, Cwd: project, OpenCodeMajor: 2})[2]
	if find(v2, "Claude's global CLAUDE.md") != nil {
		t.Fatal("OpenCode 2 must not fall back to CLAUDE.md")
	}
}

func TestSlug(t *testing.T) {
	if got := Slug(`C:\Users\PC\Projects\hobby\orchestrator`); got != "C--Users-PC-Projects-hobby-orchestrator" {
		t.Fatalf("got %s", got)
	}
}
