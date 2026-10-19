//go:build windows

package fswatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchReportsNestedWrites(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	seen := make(chan string, 64)
	w, err := Watch(dir, func(path string, overflow bool) {
		if !overflow {
			seen <- path
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(nested, "file.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case path := <-seen:
			if strings.EqualFold(path, target) {
				w.Close()
				select {
				case <-w.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("Close did not stop the watcher")
				}
				return
			}
		case <-deadline:
			t.Fatal("no notification for the nested write")
		}
	}
}

func TestWatchedDirectoryCanBeRemoved(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "worktree")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := Watch(dir, func(string, bool) {})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removing a watched directory failed: %v", err)
	}
}
