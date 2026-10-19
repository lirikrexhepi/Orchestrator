package git

import (
	"context"
	"path/filepath"
	"testing"
)

func assertTipsMatchSync(t *testing.T, repo *Repo, branch, base string) Tips {
	t.Helper()
	ctx := context.Background()
	tips, ok := repo.BranchTips(ctx, branch, base)
	if !ok {
		t.Fatalf("BranchTips(%q, %q) did not resolve", branch, base)
	}
	upstream, unpushed, unpulled := repo.Sync(ctx)
	if tips.Upstream != upstream || tips.Unpushed != unpushed || tips.Unpulled != unpulled {
		t.Fatalf("BranchTips = (%q, %d, %d), Sync = (%q, %d, %d)",
			tips.Upstream, tips.Unpushed, tips.Unpulled, upstream, unpushed, unpulled)
	}
	head, err := run(ctx, repo.Root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if tips.Head != head {
		t.Fatalf("Head = %q, want %q", tips.Head, head)
	}
	return tips
}

func TestBranchTipsMatchesSync(t *testing.T) {
	origin := newRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	mustRun(t, origin, "clone", origin, clone)
	repo := &Repo{Root: clone}

	assertTipsMatchSync(t, repo, "main", "")

	writeFile(t, filepath.Join(clone, "local.txt"), "local\n")
	mustRun(t, clone, "add", ".")
	mustRun(t, clone, "commit", "-m", "local")
	writeFile(t, filepath.Join(origin, "remote.txt"), "remote\n")
	mustRun(t, origin, "add", ".")
	mustRun(t, origin, "commit", "-m", "remote")
	mustRun(t, clone, "fetch")

	tips := assertTipsMatchSync(t, repo, "main", "origin/main")
	if tips.Unpushed != 1 || tips.Unpulled != 1 {
		t.Fatalf("ahead/behind = %d/%d, want 1/1", tips.Unpushed, tips.Unpulled)
	}
	remote, _ := run(context.Background(), clone, "rev-parse", "origin/main")
	if tips.Base != remote {
		t.Fatalf("Base = %q, want %q", tips.Base, remote)
	}

	mustRun(t, clone, "checkout", "-b", "feature")
	assertTipsMatchSync(t, repo, "feature", "main")

	mustRun(t, clone, "checkout", "main")
	mustRun(t, origin, "branch", "-m", "main", "renamed")
	mustRun(t, clone, "fetch", "--prune")
	if tips := assertTipsMatchSync(t, repo, "main", ""); tips.Upstream != "" {
		t.Fatalf("Upstream = %q for a gone upstream, want empty", tips.Upstream)
	}
}

func TestBranchTipsUnknownBranch(t *testing.T) {
	repo := &Repo{Root: newRepo(t)}
	if _, ok := repo.BranchTips(context.Background(), "missing", "main"); ok {
		t.Fatal("BranchTips resolved a branch that does not exist")
	}
	if _, ok := repo.BranchTips(context.Background(), "", ""); ok {
		t.Fatal("BranchTips resolved an empty branch")
	}
}
