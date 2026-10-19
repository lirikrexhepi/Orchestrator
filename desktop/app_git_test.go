package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"composer/internal/domain"
	"composer/internal/git"
	"composer/internal/session"
)

// scratchProject builds a repository with a main checkout and one worktree, so
// lane assembly is exercised against real git output rather than a fixture.
func scratchProject(t *testing.T) (*App, *git.Repo, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	root := t.TempDir()
	gitRun(t, root, "init", "-b", "main")
	gitRun(t, root, "config", "user.email", "test@example.com")
	gitRun(t, root, "config", "user.name", "Test")
	gitRun(t, root, "config", "commit.gpgsign", "false")

	writeFile(t, root, "main.go", "package main\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-m", "initial")

	repo, ok := git.Open(context.Background(), root)
	if !ok {
		t.Fatal("Open did not find the repository")
	}

	worktree := filepath.Join(t.TempDir(), "agent-a")
	if _, err := repo.AddWorktree(context.Background(), worktree, "agent-a", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	app := &App{ctx: context.Background(), workspaces: session.NewWorkspaces()}
	return app, repo, worktree
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func lanesFor(t *testing.T, app *App, repo *git.Repo) []domain.WorktreeChanges {
	t.Helper()
	checkouts, err := repo.Worktrees(app.ctx)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}

	owners := app.worktreeOwners()
	branch, _ := repo.CurrentBranch(app.ctx)

	lanes := make([]domain.WorktreeChanges, 0, len(checkouts))
	for _, checkout := range checkouts {
		lanes = append(lanes, app.laneFor(context.Background(), checkout, owners, branch))
	}
	return lanes
}

func find(lanes []domain.WorktreeChanges, path string) *domain.WorktreeChanges {
	for i := range lanes {
		if normalisePath(lanes[i].Path) == normalisePath(path) {
			return &lanes[i]
		}
	}
	return nil
}

func TestLaneFlagsAWorktreeNoAgentClaims(t *testing.T) {
	app, repo, worktree := scratchProject(t)

	lanes := lanesFor(t, app, repo)
	if len(lanes) != 2 {
		t.Fatalf("lanes = %d, want 2", len(lanes))
	}

	// The whole point of reading checkouts from git: one left behind by a run
	// Composer no longer remembers is exactly what needs surfacing.
	orphan := find(lanes, worktree)
	if orphan == nil || !orphan.Orphaned {
		t.Fatalf("worktree lane = %+v, want it flagged as orphaned", orphan)
	}
	if orphan.Title != "agent-a" {
		t.Errorf("title = %q, want the branch name as a fallback", orphan.Title)
	}

	if main := find(lanes, repo.Root); main == nil || !main.IsMain || main.Orphaned {
		t.Errorf("main lane = %+v, want IsMain and not orphaned", main)
	}
}

func TestLaneAdoptsTheTaskThatOwnsTheWorktree(t *testing.T) {
	app, repo, worktree := scratchProject(t)

	workspace := app.workspaces.Create("session", "prompt", repo.Root)
	if _, ok := app.workspaces.AddTask(workspace.ID, domain.Task{
		ThreadID: "thread-7",
		Title:    "Add login form",
		Worktree: &domain.Worktree{Path: worktree, Branch: "agent-a", BaseBranch: "main"},
	}); !ok {
		t.Fatal("AddTask did not record the task")
	}

	lane := find(lanesFor(t, app, repo), worktree)
	if lane == nil {
		t.Fatal("worktree lane missing")
	}
	if lane.Orphaned {
		t.Error("a worktree with a live task was flagged as orphaned")
	}
	// The tab is labelled with the work, not the directory, so a user reads what
	// the agent was asked to do rather than a generated branch name.
	if lane.Title != "Add login form" {
		t.Errorf("title = %q, want the task title", lane.Title)
	}
	if lane.ThreadID != "thread-7" {
		t.Errorf("threadId = %q", lane.ThreadID)
	}
	if lane.Base != "main" {
		t.Errorf("base = %q, want main", lane.Base)
	}
}

func TestLaneReportsEachCheckoutsOwnChanges(t *testing.T) {
	app, repo, worktree := scratchProject(t)

	writeFile(t, repo.Root, "only-in-main.txt", "main side\n")
	writeFile(t, worktree, "only-in-agent.txt", "agent side\n")

	lanes := lanesFor(t, app, repo)

	main := find(lanes, repo.Root)
	agent := find(lanes, worktree)
	if main == nil || agent == nil {
		t.Fatal("expected both lanes")
	}

	// Worktrees share a repository, so running the diff from the wrong root
	// would show one checkout the other's edits.
	if !hasFile(main.Files, "only-in-main.txt") || hasFile(main.Files, "only-in-agent.txt") {
		t.Errorf("main lane files = %+v", main.Files)
	}
	if !hasFile(agent.Files, "only-in-agent.txt") || hasFile(agent.Files, "only-in-main.txt") {
		t.Errorf("agent lane files = %+v", agent.Files)
	}
}

func hasFile(files []domain.FileChange, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func TestLaneCountsOnlyTheAgentsOwnCommits(t *testing.T) {
	app, repo, worktree := scratchProject(t)

	writeFile(t, worktree, "work.txt", "done\n")
	gitRun(t, worktree, "add", "-A")
	gitRun(t, worktree, "commit", "-m", "agent commit")

	lane := find(lanesFor(t, app, repo), worktree)
	if lane == nil {
		t.Fatal("worktree lane missing")
	}
	if lane.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", lane.Ahead)
	}
	if len(lane.Commits) != 2 {
		t.Fatalf("commits = %d, want the agent's plus the inherited one", len(lane.Commits))
	}
	if lane.Commits[0].OnBase {
		t.Error("the agent's own commit was marked inherited")
	}
}

func TestNormalisePathMatchesAcrossSeparatorsAndCasing(t *testing.T) {
	base := t.TempDir()
	mixed := filepath.Join(base, "Some", "Dir")
	if err := os.MkdirAll(mixed, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// git reports its own separators and Composer records what it created; the
	// two must compare equal or every worktree would look orphaned.
	viaSlash := normalisePath(filepath.ToSlash(mixed))
	if normalisePath(mixed) != viaSlash {
		t.Errorf("%q != %q", normalisePath(mixed), viaSlash)
	}
}

func TestLaneHistoryFollowsNewCommits(t *testing.T) {
	app, repo, worktree := scratchProject(t)

	before := find(lanesFor(t, app, repo), worktree)
	again := find(lanesFor(t, app, repo), worktree)
	if before == nil || again == nil || len(before.Commits) != len(again.Commits) || before.Ahead != again.Ahead {
		t.Fatalf("repeat lanes differ: %+v vs %+v", before, again)
	}

	writeFile(t, worktree, "feature.go", "package main\n")
	gitRun(t, worktree, "add", "-A")
	gitRun(t, worktree, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "feature")

	after := find(lanesFor(t, app, repo), worktree)
	if after == nil || len(after.Commits) != len(before.Commits)+1 || after.Ahead != before.Ahead+1 {
		t.Fatalf("after commit: commits %d ahead %d, want %d and %d",
			len(after.Commits), after.Ahead, len(before.Commits)+1, before.Ahead+1)
	}
	if after.Commits[0].Subject != "feature" {
		t.Fatalf("newest commit = %q, want feature", after.Commits[0].Subject)
	}
}
