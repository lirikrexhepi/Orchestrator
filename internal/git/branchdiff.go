package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"composer/internal/domain"
)

func (r *Repo) DefaultBranch(ctx context.Context) string {
	if ref, err := run(ctx, r.Root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	raw, err := run(ctx, r.Root, "for-each-ref", "--format=%(refname:short)",
		"refs/heads/main", "refs/heads/master", "refs/remotes/origin/main", "refs/remotes/origin/master")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(raw, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			return name
		}
	}
	return ""
}

func SameBranch(ref, branch string) bool {
	ref = strings.TrimPrefix(ref, "origin/")
	return strings.EqualFold(ref, strings.TrimPrefix(branch, "origin/"))
}

func (r *Repo) BranchDiff(ctx context.Context, base string, untracked []string) (string, []domain.FileStat) {
	rev := "HEAD"
	resolved := ""
	if base != "" {
		if point, err := run(ctx, r.Root, "merge-base", base, "HEAD"); err == nil && point != "" {
			rev, resolved = point, base
		}
	}

	stats := r.numstatsAgainst(ctx, rev)
	files := make([]domain.FileStat, 0, len(stats)+len(untracked))
	for path, stat := range stats {
		files = append(files, domain.FileStat{Path: path, Insertions: stat.insertions, Deletions: stat.deletions, Binary: stat.binary})
	}
	for _, path := range untracked {
		if _, seen := stats[path]; seen {
			continue
		}
		lines, binary := r.countLines(path)
		files = append(files, domain.FileStat{Path: path, Insertions: lines, Binary: binary})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return resolved, files
}

func (r *Repo) countLines(path string) (int, bool) {
	full := filepath.Join(r.Root, filepath.FromSlash(path))
	info, err := os.Stat(full)
	if err != nil || info.IsDir() || info.Size() > maxUntrackedBytes {
		return 0, false
	}
	body, err := os.ReadFile(full)
	if err != nil || len(body) == 0 {
		return 0, false
	}
	if bytes.IndexByte(body[:min(len(body), 8000)], 0) >= 0 {
		return 0, true
	}
	lines := bytes.Count(body, []byte{'\n'})
	if body[len(body)-1] != '\n' {
		lines++
	}
	return lines, false
}
