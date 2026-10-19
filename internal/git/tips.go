package git

import (
	"context"
	"strconv"
	"strings"
)

type Tips struct {
	Head     string
	Base     string
	Upstream string
	Unpushed int
	Unpulled int
}

func (r *Repo) BranchTips(ctx context.Context, branch, base string) (Tips, bool) {
	if branch == "" {
		return Tips{}, false
	}
	patterns := []string{"refs/heads/" + branch}
	if base != "" {
		patterns = append(patterns, "refs/heads/"+base, "refs/remotes/"+base)
	}
	args := append([]string{"for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(upstream:short)%00%(upstream:track,nobracket)"}, patterns...)
	raw, err := runRaw(ctx, r.Root, args...)
	if err != nil {
		return Tips{}, false
	}

	var tips Tips
	var remoteBase string
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\x00")
		if len(fields) < 4 {
			continue
		}
		if fields[0] == "refs/heads/"+branch {
			tips.Head = fields[1]
			if fields[3] != "gone" {
				tips.Upstream = fields[2]
				tips.Unpushed, tips.Unpulled = parseTrack(fields[3])
			}
		}
		if base != "" {
			switch fields[0] {
			case "refs/heads/" + base:
				tips.Base = fields[1]
			case "refs/remotes/" + base:
				remoteBase = fields[1]
			}
		}
	}
	if tips.Base == "" {
		tips.Base = remoteBase
	}
	return tips, tips.Head != ""
}

func parseTrack(track string) (ahead, behind int) {
	for _, part := range strings.Split(track, ",") {
		label, count, ok := strings.Cut(strings.TrimSpace(part), " ")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(count)
		switch label {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind
}
