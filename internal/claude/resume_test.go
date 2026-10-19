package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSession(t *testing.T, config, project, id, cwd string) {
	t.Helper()
	dir := filepath.Join(config, "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"type":"queue-operation","sessionId":"` + id + `"}` + "\n" +
		`{"type":"user","cwd":` + quote(cwd) + `,"sessionId":"` + id + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quote(s string) string {
	out := []byte{'"'}
	for _, r := range []byte(s) {
		if r == '\\' || r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(append(out, '"'))
}

func TestResumeCwdUsesRecordedFolder(t *testing.T) {
	config := t.TempDir()
	real := t.TempDir()
	other := t.TempDir()
	writeSession(t, config, "real-project", "sess-1", real)

	got, err := resumeCwd(config, "sess-1", other)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(got, real) {
		t.Fatalf("cwd = %q, want %q", got, real)
	}
}

func TestResumeCwdKeepsMatchingFolder(t *testing.T) {
	config := t.TempDir()
	real := t.TempDir()
	writeSession(t, config, "real-project", "sess-2", real)

	got, err := resumeCwd(config, "sess-2", real)
	if err != nil || got != real {
		t.Fatalf("cwd = %q, err = %v", got, err)
	}
}

func TestResumeCwdKeepsCwdWhenRecordedFolderIsGone(t *testing.T) {
	config := t.TempDir()
	current := t.TempDir()
	writeSession(t, config, "gone", "sess-3", filepath.Join(current, "missing"))

	got, err := resumeCwd(config, "sess-3", current)
	if err != nil || got != current {
		t.Fatalf("cwd = %q, err = %v", got, err)
	}
}

func TestResumeCwdRejectsUnknownSession(t *testing.T) {
	if _, err := resumeCwd(t.TempDir(), "nope", t.TempDir()); err == nil {
		t.Fatal("expected an error for a session with no transcript")
	}
}
