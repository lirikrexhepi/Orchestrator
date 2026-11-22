package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestResolveFileRepos_WindowsDriveNormalizationAndMultiRepo(t *testing.T) {
	if got := normalizeFilePath(""); got != "" {
		t.Fatalf("expected empty normalized path, got %q", got)
	}
	if got := normalizeFilePath("c:/users/demo/repo/"); runtime.GOOS == "windows" && got != `C:\users\demo\repo` {
		t.Fatalf("expected C:\\users\\demo\\repo, got %q", got)
	}
	if got := normalizeFilePath("/d:/workspace/proj/"); runtime.GOOS == "windows" && got != `D:\workspace\proj` {
		t.Fatalf("expected D:\\workspace\\proj, got %q", got)
	}

	initRepo := func(dir, branch string) {
		t.Helper()
		cmdInit := exec.Command("git", "init")
		cmdInit.Dir = dir
		if out, err := cmdInit.CombinedOutput(); err != nil {
			t.Fatalf("git init failed: %v (%s)", err, string(out))
		}
		for _, args := range [][]string{
			{"config", "user.name", "test"},
			{"config", "user.email", "test@example.com"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			_ = cmd.Run()
		}
		f := filepath.Join(dir, "README.md")
		if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		for _, args := range [][]string{
			{"add", "."},
			{"commit", "-m", "init"},
			{"checkout", "-b", branch},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v failed: %v (%s)", args, err, string(out))
			}
		}
	}

	repoA := t.TempDir()
	repoB := t.TempDir()
	nonGit := t.TempDir()

	initRepo(repoA, "feat/repo-a")
	initRepo(repoB, "feat/repo-b")

	fileA := filepath.Join(repoA, "src", "main.go")
	_ = os.MkdirAll(filepath.Dir(fileA), 0o755)
	_ = os.WriteFile(fileA, []byte("package main"), 0o644)

	fileB := filepath.Join(repoB, "index.ts")
	_ = os.WriteFile(fileB, []byte("export {}"), 0o644)

	fileNonGit := filepath.Join(nonGit, "notes.txt")
	_ = os.WriteFile(fileNonGit, []byte("notes"), 0o644)

	var lowerDriveA, slashTrailingB, lowerNonGit string
	if runtime.GOOS == "windows" && len(repoA) >= 2 && repoA[1] == ':' {
		lowerDriveA = strings.ToLower(fileA[:1]) + fileA[1:]
		slashTrailingB = filepath.ToSlash(repoB) + "/"
		lowerNonGit = strings.ToLower(nonGit[:1]) + nonGit[1:] + `\`
	} else {
		lowerDriveA = fileA
		slashTrailingB = repoB + "/"
		lowerNonGit = nonGit + "/"
	}

	app := &App{}
	res := app.ResolveFileRepos([]string{
		fileA,
		lowerDriveA,
		fileB,
		slashTrailingB,
		fileNonGit,
		lowerNonGit,
	})

	expectedRootA := normalizeFilePath(repoA)
	expectedRootB := normalizeFilePath(repoB)
	expectedNonGit := normalizeFilePath(nonGit)

	if res[fileA].Root != expectedRootA || res[lowerDriveA].Root != expectedRootA {
		t.Fatalf("repoA roots did not canonicalize identically: fileA=%q lower=%q want=%q", res[fileA].Root, res[lowerDriveA].Root, expectedRootA)
	}
	if res[fileA].Branch != "feat/repo-a" || !res[fileA].IsGit {
		t.Fatalf("unexpected repoA metadata: %+v", res[fileA])
	}

	if res[fileB].Root != expectedRootB || res[slashTrailingB].Root != expectedRootB {
		t.Fatalf("repoB roots did not canonicalize identically: fileB=%q trailing=%q want=%q", res[fileB].Root, res[slashTrailingB].Root, expectedRootB)
	}
	if res[fileB].Branch != "feat/repo-b" || !res[fileB].IsGit {
		t.Fatalf("unexpected repoB metadata: %+v", res[fileB])
	}

	if res[fileNonGit].Root != expectedNonGit || res[lowerNonGit].Root != expectedNonGit {
		t.Fatalf("nonGit roots did not canonicalize identically: file=%q lower=%q want=%q", res[fileNonGit].Root, res[lowerNonGit].Root, expectedNonGit)
	}
	if res[fileNonGit].IsGit {
		t.Fatalf("expected nonGit IsGit=false, got true")
	}

	if runtime.GOOS == "windows" {
		for k, scope := range res {
			if len(scope.Root) >= 2 && scope.Root[1] == ':' && (scope.Root[0] < 'A' || scope.Root[0] > 'Z') {
				t.Errorf("expected uppercase Windows drive letter for %q, got %q", k, scope.Root)
			}
			if strings.HasSuffix(scope.Root, `\`) && len(scope.Root) > 3 {
				t.Errorf("expected no trailing slash for %q, got %q", k, scope.Root)
			}
		}
	}
}
