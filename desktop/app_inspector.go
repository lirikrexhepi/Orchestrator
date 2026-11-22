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

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func normalizeFilePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if len(p) >= 3 && p[0] == '/' && isASCIILetter(p[1]) && p[2] == ':' {
		p = p[1:]
	}
	cleaned := filepath.Clean(p)
	if len(cleaned) >= 2 && cleaned[0] >= 'a' && cleaned[0] <= 'z' && cleaned[1] == ':' {
		cleaned = string(cleaned[0]-('a'-'A')) + cleaned[1:]
	}
	return cleaned
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
			repoRoot := normalizeFilePath(repo.Root)
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
			name := ""
			if cleaned != "" {
				name = filepath.Base(cleaned)
			}
			result[p] = FileRepoScope{
				Root:  cleaned,
				Name:  name,
				IsGit: false,
			}
		}
	}

	return result
}
