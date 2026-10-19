package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectDirNameMatchesClaudeCode(t *testing.T) {
	got := projectDirName(`C:\Users\PC\Projects\project-home\configurator`)
	if got != "C--Users-PC-Projects-project-home-configurator" {
		t.Fatalf("got %s", got)
	}
}

func TestMoveSessionRelocatesFileAndResumesInTheNewFolder(t *testing.T) {
	config := t.TempDir()
	oldDir := t.TempDir()
	newDir := t.TempDir()
	writeSession(t, config, projectDirName(oldDir), "sess-move", oldDir)
	if err := os.MkdirAll(filepath.Join(config, "projects", projectDirName(oldDir), "sess-move", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := moveSessionFile(config, "sess-move", newDir); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(config, "projects", projectDirName(newDir), "sess-move.jsonl")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("session not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config, "projects", projectDirName(newDir), "sess-move", "subagents")); err != nil {
		t.Fatalf("sidecar folder not moved: %v", err)
	}
	got, err := resumeCwd(config, "sess-move", newDir)
	if err != nil || !samePath(got, newDir) {
		t.Fatalf("resume cwd = %q, err = %v; want the new folder", got, err)
	}
	if err := moveSessionFile(config, "sess-move", newDir); err != nil {
		t.Fatalf("moving into the folder it is already in should be a no-op: %v", err)
	}
}
