package main

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/git"
)

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func overviewLane(t *testing.T, app *App, root, path string) *domain.WorktreeChanges {
	t.Helper()
	lanes, err := app.gitOverviewAt(app.ctx, root)
	if err != nil {
		t.Fatalf("gitOverviewAt: %v", err)
	}
	return find(lanes, path)
}

func TestWatchedOverviewFollowsEveryKindOfChange(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("directory watching is Windows-only")
	}
	app, repo, worktree := scratchProject(t)

	if lane := overviewLane(t, app, repo.Root, worktree); lane == nil || len(lane.Files) != 0 {
		t.Fatalf("fresh worktree lane = %+v", lane)
	}
	checkout := git.Checkout{Path: worktree, Branch: "agent-a"}
	eventually(t, "the clean lane to be served from cache", func() bool {
		overviewLane(t, app, repo.Root, worktree)
		_, ok := gitWatch.cached(checkout, "main")
		return ok
	})

	writeFile(t, worktree, "new.go", "package main\n")
	eventually(t, "an untracked file", func() bool {
		lane := overviewLane(t, app, repo.Root, worktree)
		return lane != nil && len(lane.Files) == 1 && lane.Files[0].Status == domain.ChangeUntracked
	})

	gitRun(t, worktree, "add", "new.go")
	eventually(t, "the file to show as staged", func() bool {
		lane := overviewLane(t, app, repo.Root, worktree)
		return lane != nil && len(lane.Files) == 1 && lane.Files[0].Staged
	})

	gitRun(t, worktree, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "watched")
	eventually(t, "the commit", func() bool {
		lane := overviewLane(t, app, repo.Root, worktree)
		return lane != nil && len(lane.Files) == 0 && lane.Ahead == 1 && len(lane.Commits) > 0 && lane.Commits[0].Subject == "watched"
	})

	writeFile(t, repo.Root, "main.go", "package main\n\nfunc main() {}\n")
	eventually(t, "the main checkout edit", func() bool {
		lane := overviewLane(t, app, repo.Root, repo.Root)
		return lane != nil && len(lane.Files) == 1 && lane.Files[0].Status == domain.ChangeModified
	})

	second := filepath.Join(t.TempDir(), "agent-b")
	gitRun(t, repo.Root, "worktree", "add", "-b", "agent-b", second, "main")
	eventually(t, "the new worktree lane", func() bool {
		return overviewLane(t, app, repo.Root, second) != nil
	})
}
