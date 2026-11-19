package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveFileRepos(t *testing.T) {
	app := &App{}

	// 1. Non-git temp directory with a file that doesn't exist yet
	nonGitDir := t.TempDir()
	nonGitFile := filepath.Join(nonGitDir, "sub", "nonexistent.txt")

	// 2. A git repository created in a temp dir
	gitDir1 := t.TempDir()
	cmdInit := exec.Command("git", "init", "-b", "main")
	cmdInit.Dir = gitDir1
	if _, err := cmdInit.CombinedOutput(); err != nil {
		// Fallback for older git that does not support -b in git init
		exec.Command("git", "init", gitDir1).Run()
		cmdBranch := exec.Command("git", "checkout", "-b", "main")
		cmdBranch.Dir = gitDir1
		_ = cmdBranch.Run()
	}
	// Configure dummy git user so commit works
	cmdCfg1 := exec.Command("git", "config", "user.name", "test")
	cmdCfg1.Dir = gitDir1
	_ = cmdCfg1.Run()
	cmdCfg2 := exec.Command("git", "config", "user.email", "test@example.com")
	cmdCfg2.Dir = gitDir1
	_ = cmdCfg2.Run()

	gitFile1 := filepath.Join(gitDir1, "src", "index.ts")
	_ = os.MkdirAll(filepath.Dir(gitFile1), 0755)
	_ = os.WriteFile(gitFile1, []byte("console.log(1)"), 0644)
	cmdAdd := exec.Command("git", "add", ".")
	cmdAdd.Dir = gitDir1
	_ = cmdAdd.Run()
	cmdCommit := exec.Command("git", "commit", "-m", "initial")
	cmdCommit.Dir = gitDir1
	_ = cmdCommit.Run()

	// 3. A second git repository with a different branch name
	gitDir2 := t.TempDir()
	cmdInit2 := exec.Command("git", "init")
	cmdInit2.Dir = gitDir2
	_ = cmdInit2.Run()
	cmdCfg3 := exec.Command("git", "config", "user.name", "test")
	cmdCfg3.Dir = gitDir2
	_ = cmdCfg3.Run()
	cmdCfg4 := exec.Command("git", "config", "user.email", "test@example.com")
	cmdCfg4.Dir = gitDir2
	_ = cmdCfg4.Run()
	gitFile2 := filepath.Join(gitDir2, "README.md")
	_ = os.WriteFile(gitFile2, []byte("# Repo 2"), 0644)
	cmdAdd2 := exec.Command("git", "add", ".")
	cmdAdd2.Dir = gitDir2
	_ = cmdAdd2.Run()
	cmdCommit2 := exec.Command("git", "commit", "-m", "initial")
	cmdCommit2.Dir = gitDir2
	_ = cmdCommit2.Run()
	cmdBranch2 := exec.Command("git", "checkout", "-b", "feat/phase-7")
	cmdBranch2.Dir = gitDir2
	_ = cmdBranch2.Run()

	// Also test Windows slash prefix normalization (e.g. /C:/...)
	slashGitFile1 := "/" + filepath.ToSlash(gitFile1)

	paths := []string{
		nonGitFile,
		gitFile1,
		gitFile2,
		slashGitFile1,
	}

	res := app.ResolveFileRepos(paths)
	if len(res) != len(paths) {
		t.Fatalf("expected %d resolved paths, got %d", len(paths), len(res))
	}

	// Verify non-git path
	nonGitScope, ok := res[nonGitFile]
	if !ok {
		t.Fatalf("missing scope for %q", nonGitFile)
	}
	if nonGitScope.IsGit {
		t.Errorf("expected non-git path to have IsGit=false, got true")
	}
	// nonGitFile doesn't exist, neither does 'sub', nearest existing dir is nonGitDir
	if filepath.Clean(nonGitScope.Root) != filepath.Clean(nonGitDir) {
		t.Errorf("expected root %q, got %q", nonGitDir, nonGitScope.Root)
	}

	// Verify gitDir1
	git1Scope, ok := res[gitFile1]
	if !ok {
		t.Fatalf("missing scope for %q", gitFile1)
	}
	if !git1Scope.IsGit {
		t.Errorf("expected git path to have IsGit=true")
	}
	if filepath.Clean(git1Scope.Root) != filepath.Clean(gitDir1) {
		t.Errorf("expected root %q, got %q", gitDir1, git1Scope.Root)
	}
	if git1Scope.Name != filepath.Base(gitDir1) {
		t.Errorf("expected name %q, got %q", filepath.Base(gitDir1), git1Scope.Name)
	}

	// Verify gitDir2 has distinct scope and branch
	git2Scope, ok := res[gitFile2]
	if !ok {
		t.Fatalf("missing scope for %q", gitFile2)
	}
	if !git2Scope.IsGit {
		t.Errorf("expected git path to have IsGit=true")
	}
	if filepath.Clean(git2Scope.Root) != filepath.Clean(gitDir2) {
		t.Errorf("expected root %q, got %q", gitDir2, git2Scope.Root)
	}
	if git2Scope.Branch != "feat/phase-7" {
		t.Errorf("expected branch feat/phase-7, got %q", git2Scope.Branch)
	}

	// Verify slash normalized path resolves to same repo as gitFile1
	slashScope, ok := res[slashGitFile1]
	if !ok {
		t.Fatalf("missing scope for %q", slashGitFile1)
	}
	if filepath.Clean(slashScope.Root) != filepath.Clean(gitDir1) {
		t.Errorf("expected root %q for slash path, got %q", gitDir1, slashScope.Root)
	}
}
