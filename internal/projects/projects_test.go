package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	store := New(dir)
	added, err := store.Add(target, true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A second store over the same directory stands in for the next app run.
	reopened := New(dir)
	if got := reopened.ActivePath(); got != added.Path {
		t.Fatalf("active project lost across restart: got %q want %q", got, added.Path)
	}
	if list := reopened.List(); len(list) != 1 {
		t.Fatalf("expected one stored project, got %d", len(list))
	}
}

func TestAddingSameDirectoryTwiceDoesNotDuplicate(t *testing.T) {
	store := New(t.TempDir())
	target := t.TempDir()

	first, err := store.Add(target, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := store.Add(target, false)
	if err != nil {
		t.Fatalf("Add again: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("same directory produced two projects: %q and %q", first.ID, second.ID)
	}
	if list := store.List(); len(list) != 1 {
		t.Fatalf("expected one project, got %d", len(list))
	}
}

func TestAddRejectsAFile(t *testing.T) {
	store := New(t.TempDir())
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Caught here rather than when a CLI cannot chdir into it later.
	if _, err := store.Add(file, false); err == nil {
		t.Fatal("expected adding a file to fail")
	}
}

func TestRemovingActiveProjectClearsSelection(t *testing.T) {
	store := New(t.TempDir())
	added, err := store.Add(t.TempDir(), false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := store.Remove(added.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// Nothing may be silently promoted: the next spawn must not land in a
	// directory the user never chose.
	if path := store.ActivePath(); path != "" {
		t.Fatalf("expected no active project, got %q", path)
	}
}

func TestDeletedDirectoryIsReportedMissing(t *testing.T) {
	store := New(t.TempDir())
	target := t.TempDir()
	if _, err := store.Add(target, false); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	list := store.List()
	if len(list) != 1 || !list[0].Missing {
		t.Fatalf("expected the entry to be flagged missing, got %+v", list)
	}
	// A missing directory must not be handed out as a working directory.
	if path := store.ActivePath(); path != "" {
		t.Fatalf("expected no usable path for a missing project, got %q", path)
	}
}

func TestActivateKeepsManualOrder(t *testing.T) {
	store := New(t.TempDir())
	first, _ := store.Add(t.TempDir(), false)
	second, _ := store.Add(t.TempDir(), false)

	if _, err := store.Activate(first.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	list := store.List()
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("Activate must not reorder the list: %+v", list)
	}
	if store.ActivePath() != first.Path {
		t.Fatalf("Activate did not change the active project")
	}
}

func TestMoveReordersProjects(t *testing.T) {
	store := New(t.TempDir())
	first, _ := store.Add(t.TempDir(), false)
	second, _ := store.Add(t.TempDir(), false)
	third, _ := store.Add(t.TempDir(), false)

	if err := store.Move(first.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}
	list := store.List()
	if len(list) != 3 || list[0].ID != first.ID || list[1].ID != third.ID || list[2].ID != second.ID {
		t.Fatalf("unexpected order after move: %+v", list)
	}

	if err := store.Move("missing", 0); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := store.Move(second.ID, 99); err != nil {
		t.Fatalf("Move clamps out-of-range indexes: %v", err)
	}
	if list := store.List(); list[len(list)-1].ID != second.ID {
		t.Fatalf("expected second last after clamped move: %+v", list)
	}
}

func TestMoveSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	first, _ := store.Add(t.TempDir(), false)
	second, _ := store.Add(t.TempDir(), false)

	if err := store.Move(first.ID, 0); err != nil {
		t.Fatalf("Move: %v", err)
	}
	list := New(dir).List()
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("manual order was not persisted: %+v", list)
	}
}

func TestUnreadableStoreFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	// Corrupt file: losing the list is acceptable, refusing to start is not.
	if err := os.WriteFile(filepath.Join(dir, storeFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	store := New(dir)
	if len(store.List()) != 0 {
		t.Fatalf("expected an empty store, got %+v", store.List())
	}
	if _, err := store.Add(t.TempDir(), false); err != nil {
		t.Fatalf("a corrupt file must not block later writes: %v", err)
	}
}

func TestLegacyEntriesWithoutOrderKeepTheirTimestampOrder(t *testing.T) {
	dir := t.TempDir()
	older, newer := t.TempDir(), t.TempDir()

	// A file as an earlier version wrote it: timestamps, no order field.
	legacy := `{"projects":[
		{"id":"older-1","name":"older","path":` + quote(older) + `,"addedAt":100,"usedAt":100},
		{"id":"newer-1","name":"newer","path":` + quote(newer) + `,"addedAt":200,"usedAt":300}
	],"activeId":"newer-1"}`
	if err := os.WriteFile(filepath.Join(dir, storeFile), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	list := New(dir).List()
	if len(list) != 2 {
		t.Fatalf("expected two projects, got %d", len(list))
	}
	if list[0].ID != "newer-1" {
		t.Fatalf("most recently used entry should sort first, got %q", list[0].ID)
	}
}

// quote renders a path as a JSON string, so Windows separators stay escaped.
func quote(path string) string {
	encoded, err := json.Marshal(path)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestIDsAreUniqueForSameBaseName(t *testing.T) {
	store := New(t.TempDir())
	parentA, parentB := t.TempDir(), t.TempDir()

	pathA := filepath.Join(parentA, "api")
	pathB := filepath.Join(parentB, "api")
	for _, path := range []string{pathA, pathB} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	a, err := store.Add(pathA, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	b, err := store.Add(pathB, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if a.ID == b.ID {
		t.Fatalf("two folders named %q collided on id %q", "api", a.ID)
	}
	if !strings.HasPrefix(a.ID, "api-") || !strings.HasPrefix(b.ID, "api-") {
		t.Fatalf("ids should be derived from the folder name, got %q and %q", a.ID, b.ID)
	}
}

func TestRelocatePreservesIdentityOrderAndAccounts(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	oldDir := filepath.Join(t.TempDir(), "falacy")
	newDir := filepath.Join(t.TempDir(), "falacy-moved")
	otherDir := filepath.Join(t.TempDir(), "other")
	for _, p := range []string{oldDir, newDir, otherDir} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	first, err := store.Add(oldDir, false)
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}
	if _, err := store.SetAccount(first.ID, "claude", "work"); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}
	second, err := store.Add(otherDir, false)
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if _, err := store.Activate(first.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// Simulate moving the original folder away so it becomes missing.
	if err := os.RemoveAll(oldDir); err != nil {
		t.Fatalf("remove oldDir: %v", err)
	}
	if got := store.ActivePath(); got != "" {
		t.Fatalf("expected missing active project to yield empty ActivePath, got %q", got)
	}

	// Reject relocating onto a path already owned by another project.
	if _, _, err := store.Relocate(first.ID, otherDir, false); err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	prev, updated, err := store.Relocate(first.ID, newDir, true)
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if prev != first.Path {
		t.Fatalf("oldPath: got %q want %q", prev, first.Path)
	}
	if updated.ID != first.ID || updated.Path != newDir || updated.Name != "falacy-moved" || !updated.IsGit || updated.Missing {
		t.Fatalf("unexpected relocated project: %+v", updated)
	}
	if updated.Accounts["claude"] != "work" {
		t.Fatalf("expected accounts preserved, got %+v", updated.Accounts)
	}

	// Order and active selection must survive restart.
	reopened := New(dir)
	if got := reopened.ActivePath(); got != newDir {
		t.Fatalf("ActivePath after relocate: got %q want %q", got, newDir)
	}
	list := reopened.List()
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("Relocate must keep picker order: %+v", list)
	}
}
