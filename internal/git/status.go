package git

import (
	"context"
	"strconv"
	"strings"

	"composer/internal/domain"
)

// Status lists what has changed in a working tree.
//
// Porcelain v1 with -z is used rather than the newer v2: the fields needed here
// are the two status columns and the path, v1 gives exactly those, and NUL
// separation keeps paths with spaces or quotes intact without unquoting rules.
func (r *Repo) Status(ctx context.Context) ([]domain.FileChange, error) {
	raw, err := runRaw(ctx, r.Root, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}

	changes := make([]domain.FileChange, 0, 16)
	if strings.Trim(raw, "\x00") == "" {
		return changes, nil
	}
	stats := r.numstats(ctx)

	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}

		index, worktree := entry[0], entry[1]
		path := entry[3:]

		// A rename entry is followed by its original path as a separate field,
		// which must be consumed here or it would be read as its own entry.
		var oldPath string
		if index == 'R' || index == 'C' {
			if i+1 < len(fields) {
				oldPath = fields[i+1]
				i++
			}
		}

		for _, change := range splitSides(index, worktree, path, oldPath) {
			if stat, ok := stats[change.Path]; ok {
				change.Insertions, change.Deletions, change.Binary = stat.insertions, stat.deletions, stat.binary
			}
			changes = append(changes, change)
		}
	}
	return changes, nil
}

// splitSides turns one porcelain entry into the rows a user should see.
//
// A file staged and then edited again has a status in both columns and is two
// distinct diffs, so it is listed twice — matching what git and GitHub Desktop
// both show — rather than being collapsed into a single ambiguous row.
func splitSides(index, worktree byte, path, oldPath string) []domain.FileChange {
	if index == '?' || worktree == '?' {
		return []domain.FileChange{{Path: path, Status: domain.ChangeUntracked}}
	}
	if index == 'U' || worktree == 'U' || (index == 'A' && worktree == 'A') || (index == 'D' && worktree == 'D') {
		return []domain.FileChange{{Path: path, OldPath: oldPath, Status: domain.ChangeConflicted}}
	}

	changes := make([]domain.FileChange, 0, 2)
	if status, ok := statusOf(index); ok {
		changes = append(changes, domain.FileChange{Path: path, OldPath: oldPath, Status: status, Staged: true})
	}
	if status, ok := statusOf(worktree); ok {
		changes = append(changes, domain.FileChange{Path: path, OldPath: oldPath, Status: status})
	}
	return changes
}

func statusOf(code byte) (domain.ChangeStatus, bool) {
	switch code {
	case 'A':
		return domain.ChangeAdded, true
	case 'M':
		return domain.ChangeModified, true
	case 'D':
		return domain.ChangeDeleted, true
	case 'R':
		return domain.ChangeRenamed, true
	case 'C':
		return domain.ChangeCopied, true
	case 'T':
		return domain.ChangeModified, true
	}
	return "", false
}

type fileStat struct {
	insertions int
	deletions  int
	binary     bool
}

// numstats collects per-file line counts for tracked changes in one call.
//
// Diffing against HEAD covers staged and unstaged edits together, which is the
// figure worth showing beside a filename. Failure is not fatal: counts are
// decoration, and a repository with no commits yet has no HEAD to diff.
func (r *Repo) numstats(ctx context.Context) map[string]fileStat {
	stats := make(map[string]fileStat)

	raw, err := runRaw(ctx, r.Root, "diff", "--numstat", "-z", "HEAD")
	if err != nil {
		return stats
	}

	fields := strings.Split(raw, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}

		path := parts[2]
		// A rename reports an empty path and carries old and new as the next two
		// NUL-separated fields instead.
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			i += 2
		}

		stat := fileStat{binary: parts[0] == "-" || parts[1] == "-"}
		stat.insertions, _ = strconv.Atoi(parts[0])
		stat.deletions, _ = strconv.Atoi(parts[1])
		stats[path] = stat
	}
	return stats
}
