package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/fswatch"
	"composer/internal/git"
)

const gitRescanAfter = 60 * time.Second

type laneScan struct {
	upstream string
	unpushed int
	unpulled int
	files    []domain.FileChange
	commits  []domain.Commit
	ahead    int
}

func (s laneScan) apply(lane *domain.WorktreeChanges) {
	lane.Upstream, lane.Unpushed, lane.Unpulled = s.upstream, s.unpushed, s.unpulled
	lane.Files, lane.Commits, lane.Ahead = s.files, s.commits, s.ahead
}

type trackedLane struct {
	root      string
	gitDir    string
	commonDir string
	branch    string
	base      string
	watched   bool
	dirty     bool
	valid     bool
	scannedAt time.Time
	scan      laneScan
}

type trackedLayout struct {
	commonDir  string
	checkouts  []git.Checkout
	mainBranch string
	watched    bool
	dirty      bool
	valid      bool
	builtAt    time.Time
}

type gitTracker struct {
	mu       sync.Mutex
	watchers map[string]*fswatch.Watcher
	lanes    map[string]*trackedLane
	layouts  map[string]*trackedLayout
}

var gitWatch = newGitTracker()

func newGitTracker() *gitTracker {
	return &gitTracker{
		watchers: map[string]*fswatch.Watcher{},
		lanes:    map[string]*trackedLane{},
		layouts:  map[string]*trackedLayout{},
	}
}

func pathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

func gitDirs(root string) (gitDir, commonDir string, ok bool) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", "", false
	}
	if info.IsDir() {
		return dotGit, dotGit, true
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", "", false
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", "", false
	}
	gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	commonDir = gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		commonDir = strings.TrimSpace(string(data))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitDir, commonDir)
		}
	}
	return filepath.Clean(gitDir), filepath.Clean(commonDir), true
}

func (t *gitTracker) layout(ctx context.Context, root string) ([]git.Checkout, string, error) {
	key := pathKey(root)

	t.mu.Lock()
	entry := t.layouts[key]
	if entry != nil && entry.valid && entry.watched && !entry.dirty && time.Since(entry.builtAt) < gitRescanAfter {
		checkouts, mainBranch := entry.checkouts, entry.mainBranch
		t.mu.Unlock()
		return checkouts, mainBranch, nil
	}
	t.mu.Unlock()

	repo, ok := git.Open(ctx, root)
	if !ok {
		return nil, "", fmt.Errorf("%s is not a git repository", root)
	}
	_, commonDir, found := gitDirs(repo.Root)

	t.mu.Lock()
	entry = t.layouts[key]
	if entry == nil {
		entry = &trackedLayout{}
		t.layouts[key] = entry
	}
	entry.valid, entry.dirty = false, false
	entry.builtAt = time.Now()
	entry.watched = found && t.watch(commonDir)
	if found {
		entry.commonDir = pathKey(commonDir)
	}
	t.mu.Unlock()

	checkouts, err := repo.Worktrees(ctx)
	if err != nil {
		return nil, "", err
	}
	mainBranch, _ := repo.CurrentBranch(ctx)

	t.mu.Lock()
	entry.checkouts, entry.mainBranch, entry.valid = checkouts, mainBranch, true
	t.mu.Unlock()
	return checkouts, mainBranch, nil
}

func (t *gitTracker) cached(checkout git.Checkout, base string) (laneScan, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	lane := t.lanes[pathKey(checkout.Path)]
	if lane == nil || !lane.valid || !lane.watched || lane.dirty ||
		lane.branch != checkout.Branch || lane.base != base ||
		time.Since(lane.scannedAt) >= gitRescanAfter {
		return laneScan{}, false
	}
	return lane.scan, true
}

func (t *gitTracker) begin(checkout git.Checkout, base string) *trackedLane {
	gitDir, commonDir, found := gitDirs(checkout.Path)

	t.mu.Lock()
	defer t.mu.Unlock()
	key := pathKey(checkout.Path)
	lane := t.lanes[key]
	if lane == nil {
		lane = &trackedLane{root: key}
		t.lanes[key] = lane
	}
	lane.branch, lane.base = checkout.Branch, base
	lane.valid, lane.dirty = false, false
	lane.scannedAt = time.Now()
	lane.gitDir, lane.commonDir = "", ""
	if found {
		lane.gitDir, lane.commonDir = pathKey(gitDir), pathKey(commonDir)
	}
	lane.watched = found && t.watch(checkout.Path) && t.watch(commonDir)
	return lane
}

func (t *gitTracker) finish(lane *trackedLane, result domain.WorktreeChanges) {
	if result.Error != "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	lane.scan = laneScan{
		upstream: result.Upstream,
		unpushed: result.Unpushed,
		unpulled: result.Unpulled,
		files:    result.Files,
		commits:  result.Commits,
		ahead:    result.Ahead,
	}
	lane.valid = true
}

func (t *gitTracker) watch(dir string) bool {
	key := pathKey(dir)
	for watched := range t.watchers {
		if within(key, watched) {
			return true
		}
	}
	watcher, err := fswatch.Watch(dir, func(path string, overflow bool) {
		t.changed(path, overflow)
	})
	if err != nil {
		return false
	}
	t.watchers[key] = watcher
	go func() {
		<-watcher.Done()
		t.mu.Lock()
		if t.watchers[key] == watcher {
			delete(t.watchers, key)
		}
		t.dirtyAll()
		t.mu.Unlock()
	}()
	return true
}

func (t *gitTracker) dirtyAll() {
	for _, lane := range t.lanes {
		lane.dirty = true
	}
	for _, layout := range t.layouts {
		layout.dirty = true
	}
}

func (t *gitTracker) changed(path string, overflow bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if overflow {
		t.dirtyAll()
		return
	}

	key := pathKey(path)
	for _, lane := range t.lanes {
		if within(key, lane.commonDir) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return
			}
			t.gitDirChanged(lane.commonDir, key)
			return
		}
	}

	var owner *trackedLane
	for _, lane := range t.lanes {
		if within(key, lane.root) && (owner == nil || len(lane.root) > len(owner.root)) {
			owner = lane
		}
	}
	if owner != nil {
		owner.dirty = true
	}
}

func (t *gitTracker) gitDirChanged(commonDir, key string) {
	rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(key, commonDir), string(filepath.Separator)))
	if rel == "" || rel == "objects" || rel == "logs" ||
		strings.HasPrefix(rel, "objects/") || strings.HasPrefix(rel, "logs/") ||
		strings.Contains(rel, "/logs/") || strings.HasSuffix(rel, "/logs") || strings.HasSuffix(rel, ".lock") {
		return
	}

	dirtyGitDir := func(gitDir string) {
		for _, lane := range t.lanes {
			if lane.gitDir == gitDir {
				lane.dirty = true
			}
		}
	}
	dirtyLayouts := func() {
		for _, layout := range t.layouts {
			if layout.commonDir == commonDir {
				layout.dirty = true
			}
		}
	}
	dirtyRepo := func() {
		for _, lane := range t.lanes {
			if lane.commonDir == commonDir {
				lane.dirty = true
			}
		}
		dirtyLayouts()
	}

	switch {
	case rel == "index":
		dirtyGitDir(commonDir)
	case rel == "head":
		dirtyGitDir(commonDir)
		dirtyLayouts()
	case rel == "worktrees" || strings.HasPrefix(rel, "worktrees/"):
		parts := strings.SplitN(rel, "/", 3)
		if len(parts) < 3 {
			if len(parts) == 2 {
				dirtyGitDir(filepath.Join(commonDir, "worktrees", parts[1]))
			}
			dirtyLayouts()
			return
		}
		gitDir := filepath.Join(commonDir, "worktrees", parts[1])
		switch parts[2] {
		case "index":
			dirtyGitDir(gitDir)
		case "head", "gitdir", "locked":
			dirtyGitDir(gitDir)
			dirtyLayouts()
		}
	case strings.HasPrefix(rel, "refs/heads/"):
		branch := strings.ToLower(strings.TrimPrefix(rel, "refs/heads/"))
		touches := func(name string) bool {
			name = strings.ToLower(name)
			return name == branch || strings.HasPrefix(name, branch+"/")
		}
		for _, lane := range t.lanes {
			if lane.commonDir == commonDir && (touches(lane.branch) || touches(lane.base)) {
				lane.dirty = true
			}
		}
		dirtyLayouts()
	case rel == "refs" || strings.HasPrefix(rel, "refs/") || rel == "packed-refs" || rel == "config" ||
		rel == "info" || strings.HasPrefix(rel, "info/"):
		dirtyRepo()
	}
}
