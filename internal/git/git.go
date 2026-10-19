package git

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"composer/internal/domain"
	"composer/internal/shell"
)

type Repo struct {
	Root string
}

var resolvedBinary atomic.Pointer[string]

func binary() string {
	if path := resolvedBinary.Load(); path != nil {
		return *path
	}
	path, err := exec.LookPath("git")
	if err != nil {
		return "git"
	}
	resolvedBinary.Store(&path)
	return path
}

func command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary(), args...)
	cmd.Dir = dir
	cmd.Env = shell.Slice(shell.BaseEnvironment())
	hideWindow(cmd)
	return cmd
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := runRaw(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// runRaw is run without the trim, for output whose leading or trailing
// whitespace carries meaning — NUL-separated records and diff bodies both do.
func runRaw(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary(), args...)
	cmd.Dir = dir
	cmd.Env = shell.Slice(shell.BaseEnvironment())
	hideWindow(cmd)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", subcommand(args), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", subcommand(args), err)
	}
	return string(out), nil
}

func subcommand(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return args[0]
}

// Open resolves the repository containing dir, if there is one.
func Open(ctx context.Context, dir string) (*Repo, bool) {
	root, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil, false
	}
	return &Repo{Root: filepath.Clean(root)}, true
}

func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	return run(ctx, r.Root, "rev-parse", "--abbrev-ref", "HEAD")
}

// AddWorktree creates an isolated checkout so parallel agents never edit the
// same files. The branch is created from base at the time of the call.
func (r *Repo) AddWorktree(ctx context.Context, path, branch, base string) (*domain.Worktree, error) {
	if base == "" {
		current, err := r.CurrentBranch(ctx)
		if err != nil {
			return nil, err
		}
		base = current
	}

	if _, err := run(ctx, r.Root, "worktree", "add", "-b", branch, path, base); err != nil {
		return nil, err
	}
	return &domain.Worktree{Path: filepath.Clean(path), Branch: branch, BaseBranch: base}, nil
}

// RemoveWorktree detaches a checkout. Committed work survives on its branch;
// uncommitted work is only discarded when force is set.
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool) error {
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	_, err := run(ctx, r.Root, args...)
	return err
}

// Handoff summarises a task branch against its base: what changed, and whether
// it would merge cleanly. Composer reports this and leaves the merge to a human.
func (r *Repo) Handoff(ctx context.Context, worktree *domain.Worktree) (domain.TaskHandoff, error) {
	handoff := domain.TaskHandoff{Branch: worktree.Branch}
	base := worktree.BaseBranch
	if base == "" {
		base = "HEAD"
	}

	rangeSpec := base + "..." + worktree.Branch
	if stat, err := run(ctx, r.Root, "diff", "--numstat", rangeSpec); err == nil {
		handoff.FilesChanged, handoff.Insertions, handoff.Deletions = parseNumstat(stat)
	}
	if count, err := run(ctx, r.Root, "rev-list", "--count", base+".."+worktree.Branch); err == nil {
		handoff.Commits, _ = strconv.Atoi(count)
	}

	// CleanMerge stays false unless the preview actually ran and found nothing,
	// so an unsupported git or a broken invocation never reads as "safe".
	conflicts, determined, err := r.previewConflicts(ctx, base, worktree.Branch)
	if err != nil {
		return handoff, err
	}
	handoff.Conflicts = conflicts
	handoff.CleanMerge = determined && len(conflicts) == 0
	return handoff, nil
}

// previewConflicts merges in memory without touching any working tree.
//
// `merge-tree --write-tree` exits 1 when the merge conflicts, so a non-zero
// status is the signal itself rather than a failure. Its stdout is the result
// tree oid, then the conflicted paths, then a blank line and human-readable
// messages — only the paths section is of interest.
func (r *Repo) previewConflicts(ctx context.Context, base, branch string) ([]string, bool, error) {
	cmd := exec.CommandContext(ctx, binary(), "merge-tree", "--write-tree", "--name-only", base, branch)
	cmd.Dir = r.Root
	cmd.Env = shell.Slice(shell.BaseEnvironment())
	hideWindow(cmd)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		// Exit 1 means "conflicts found"; anything else is a real failure, and
		// must not be reported as a clean merge.
		if !asExitError(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, false, fmt.Errorf("git merge-tree: %w", err)
		}
	}

	var conflicts []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for index := 0; scanner.Scan(); index++ {
		line := strings.TrimSpace(scanner.Text())
		if index == 0 {
			continue
		}
		// The blank line terminates the path list.
		if line == "" {
			break
		}
		conflicts = append(conflicts, line)
	}
	return conflicts, true, nil
}

func parseNumstat(stat string) (files, insertions, deletions int) {
	for _, line := range strings.Split(stat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		files++
		if added, err := strconv.Atoi(fields[0]); err == nil {
			insertions += added
		}
		if removed, err := strconv.Atoi(fields[1]); err == nil {
			deletions += removed
		}
	}
	return files, insertions, deletions
}
