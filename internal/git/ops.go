package git

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"composer/internal/domain"
	"composer/internal/shell"
)

func runNetwork(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, binary(), args...)
	cmd.Dir = dir
	env := shell.BaseEnvironment()
	merged := make(map[string]string, len(env)+2)
	for key, value := range env {
		merged[key] = value
	}
	merged["GIT_TERMINAL_PROMPT"] = "0"
	merged["GCM_INTERACTIVE"] = "never"
	cmd.Env = shell.Slice(merged)
	hideWindow(cmd)

	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New(text)
	}
	return text, nil
}

func (r *Repo) Stage(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := runNetwork(ctx, r.Root, args...)
	return err
}

func (r *Repo) Unstage(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"restore", "--staged", "--"}, paths...)
	if _, err := runNetwork(ctx, r.Root, args...); err != nil {
		rm := append([]string{"rm", "--cached", "-r", "--"}, paths...)
		_, rmErr := runNetwork(ctx, r.Root, rm...)
		return rmErr
	}
	return nil
}

func (r *Repo) Commit(ctx context.Context, summary, description string) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("a commit needs a summary")
	}
	staged, err := run(ctx, r.Root, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if staged == "" {
		return errors.New("nothing is staged to commit")
	}
	args := []string{"commit", "-m", summary}
	if description = strings.TrimSpace(description); description != "" {
		args = append(args, "-m", description)
	}
	_, err = runNetwork(ctx, r.Root, args...)
	return err
}

func (r *Repo) Branches(ctx context.Context) ([]domain.BranchInfo, error) {
	const separator = "\x1f"
	raw, err := runRaw(ctx, r.Root, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)"+separator+"%(refname:short)"+separator+"%(HEAD)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := make([]domain.BranchInfo, 0, 16)
	local := make(map[string]bool)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimSpace(line), separator)
		if len(fields) < 3 {
			continue
		}
		full, short, head := fields[0], fields[1], fields[2]
		if strings.HasSuffix(full, "/HEAD") {
			continue
		}
		remote := strings.HasPrefix(full, "refs/remotes/")
		if !remote {
			local[short] = true
		}
		branches = append(branches, domain.BranchInfo{Name: short, Current: head == "*", Remote: remote})
	}
	out := branches[:0]
	for _, branch := range branches {
		if branch.Remote {
			if cut := strings.Index(branch.Name, "/"); cut >= 0 && local[branch.Name[cut+1:]] {
				continue
			}
		}
		out = append(out, branch)
	}
	return out, nil
}

func (r *Repo) Checkout(ctx context.Context, branch, mode string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("no branch given")
	}
	if mode != "leave" && mode != "bring" {
		_, err := runNetwork(ctx, r.Root, "checkout", branch)
		return err
	}

	current, _ := r.CurrentBranch(ctx)
	dirty, err := run(ctx, r.Root, "status", "--porcelain")
	if err != nil {
		return err
	}
	stashed := false
	if dirty != "" {
		if _, err := runNetwork(ctx, r.Root, "stash", "push", "--include-untracked", "-m", "orchestrator: changes on "+current); err != nil {
			return err
		}
		stashed = true
	}
	if _, err := runNetwork(ctx, r.Root, "checkout", branch); err != nil {
		if stashed {
			_, _ = runNetwork(ctx, r.Root, "stash", "pop")
		}
		return err
	}
	if stashed && mode == "bring" {
		if _, err := runNetwork(ctx, r.Root, "stash", "pop"); err != nil {
			return errors.New("switched to " + branch + ", but the changes could not be applied cleanly and remain in the stash: " + err.Error())
		}
	}
	return nil
}

func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a branch needs a name")
	}
	_, err := runNetwork(ctx, r.Root, "checkout", "-b", name)
	return err
}

func (r *Repo) Fetch(ctx context.Context) error {
	_, err := runNetwork(ctx, r.Root, "fetch", "--prune")
	return err
}

func (r *Repo) Pull(ctx context.Context) error {
	_, err := runNetwork(ctx, r.Root, "pull", "--ff-only")
	return err
}

func (r *Repo) Push(ctx context.Context) error {
	if upstream, _, _ := r.Sync(ctx); upstream == "" {
		_, err := runNetwork(ctx, r.Root, "push", "-u", "origin", "HEAD")
		return err
	}
	_, err := runNetwork(ctx, r.Root, "push")
	return err
}

func (r *Repo) Sync(ctx context.Context) (upstream string, unpushed, unpulled int) {
	name, err := run(ctx, r.Root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil || name == "" {
		return "", 0, 0
	}
	counts, err := run(ctx, r.Root, "rev-list", "--left-right", "--count", "@{u}...HEAD")
	if err != nil {
		return name, 0, 0
	}
	fields := strings.Fields(counts)
	if len(fields) == 2 {
		unpulled, _ = strconv.Atoi(fields[0])
		unpushed, _ = strconv.Atoi(fields[1])
	}
	return name, unpushed, unpulled
}
