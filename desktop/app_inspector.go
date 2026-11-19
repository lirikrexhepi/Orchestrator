package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"composer/internal/git"
)

type FileRepoScope struct {
	Root   string `json:"root"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	IsGit  bool   `json:"isGit"`
}

func normalizeFilePath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 3 && p[0] == '/' && ((p[1] >= 'a' && p[1] <= 'z') || (p[1] >= 'A' && p[1] <= 'Z')) && p[2] == ':' {
		p = p[1:]
	}
	return filepath.Clean(p)
}

func resolveNearestDir(p string) string {
	p = normalizeFilePath(p)
	if p == "" || p == "." {
		return ""
	}
	fi, err := os.Stat(p)
	if err == nil && fi.IsDir() {
		return p
	}
	dir := filepath.Dir(p)
	for {
		fi, err := os.Stat(dir)
		if err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "" || parent == "." {
			return dir
		}
		dir = parent
	}
}

// ResolveFileRepos resolves repository scopes for a list of file or directory paths.
func (a *App) ResolveFileRepos(paths []string) map[string]FileRepoScope {
	result := make(map[string]FileRepoScope, len(paths))
	if len(paths) == 0 {
		return result
	}

	pathToDir := make(map[string]string, len(paths))
	uniqueDirs := make(map[string]struct{})

	for _, p := range paths {
		dir := resolveNearestDir(p)
		pathToDir[p] = dir
		if dir != "" {
			uniqueDirs[dir] = struct{}{}
		}
	}

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	dirScopes := make(map[string]FileRepoScope, len(uniqueDirs))
	for dir := range uniqueDirs {
		ctx, cancel := context.WithTimeout(baseCtx, 2*time.Second)
		repo, ok := git.Open(ctx, dir)
		if ok && repo != nil && repo.Root != "" {
			branch, _ := repo.CurrentBranch(ctx)
			cancel()
			repoRoot := repo.Root
			dirScopes[dir] = FileRepoScope{
				Root:   repoRoot,
				Name:   filepath.Base(repoRoot),
				Branch: strings.TrimSpace(branch),
				IsGit:  true,
			}
		} else {
			cancel()
			dirScopes[dir] = FileRepoScope{
				Root:  dir,
				Name:  filepath.Base(dir),
				IsGit: false,
			}
		}
	}

	for _, p := range paths {
		dir := pathToDir[p]
		if scope, ok := dirScopes[dir]; ok {
			result[p] = scope
		} else {
			cleaned := normalizeFilePath(p)
			result[p] = FileRepoScope{
				Root:  cleaned,
				Name:  filepath.Base(cleaned),
				IsGit: false,
			}
		}
	}

	return result
}
