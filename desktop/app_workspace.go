package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var editorCommands = map[string]string{
	"vscode": "code",
	"cursor": "cursor",
}

func (a *App) AvailableEditors() []string {
	out := []string{}
	for _, id := range []string{"vscode", "cursor"} {
		if _, err := exec.LookPath(editorCommands[id]); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) OpenInEditor(path, editor string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("no folder to open")
	}
	if editor == "explorer" {
		return a.RevealPath(path)
	}
	command, ok := editorCommands[editor]
	if !ok {
		return fmt.Errorf("unknown editor %q", editor)
	}
	binary, err := exec.LookPath(command)
	if err != nil {
		return fmt.Errorf("%s is not installed", command)
	}
	return startDetached(binary, filepath.Clean(path))
}

func (a *App) GitPullRequestURL(worktreePath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	remote, err := gitOutput(ctx, worktreePath, "remote", "get-url", "origin")
	if err != nil {
		return "", errors.New("this repository has no origin remote")
	}
	branch, err := gitOutput(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		return "", errors.New("not on a branch")
	}
	base := "main"
	if head, err := gitOutput(ctx, worktreePath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		base = strings.TrimPrefix(head, "origin/")
	}
	return pullRequestURL(remote, base, branch)
}

func pullRequestURL(remote, base, branch string) (string, error) {
	host, repo, ok := parseRemote(remote)
	if !ok {
		return "", fmt.Errorf("unrecognised remote %q", remote)
	}
	if strings.Contains(host, "gitlab") {
		return fmt.Sprintf("https://%s/%s/-/merge_requests/new?merge_request%%5Bsource_branch%%5D=%s&merge_request%%5Btarget_branch%%5D=%s",
			host, repo, url.QueryEscape(branch), url.QueryEscape(base)), nil
	}
	return fmt.Sprintf("https://%s/%s/compare/%s...%s?expand=1", host, repo, base, branch), nil
}

func parseRemote(remote string) (string, string, bool) {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), "/")
	remote = strings.TrimSuffix(remote, ".git")
	if rest, ok := strings.CutPrefix(remote, "git@"); ok {
		host, repo, found := strings.Cut(rest, ":")
		return host, repo, found && host != "" && repo != ""
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	repo := strings.Trim(parsed.Path, "/")
	return parsed.Hostname(), repo, repo != ""
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := workspaceCommand(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
