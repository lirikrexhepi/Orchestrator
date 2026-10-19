package git

import (
	"context"
	"os/exec"
	"strings"

	"composer/internal/shell"
)

// Ignored reports which of the given paths git ignores. Paths are relative to
// dir, slash-separated, and come back in the same form. Asking about one
// directory's entries at a time keeps this cheap however large an ignored
// folder like node_modules is, since git never walks into it.
func Ignored(ctx context.Context, dir string, paths []string) map[string]bool {
	ignored := make(map[string]bool)
	if len(paths) == 0 {
		return ignored
	}

	cmd := exec.CommandContext(ctx, binary(), "check-ignore", "-z", "--stdin")
	cmd.Dir = dir
	cmd.Env = shell.Slice(shell.BaseEnvironment())
	hideWindow(cmd)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")

	// Exit status 1 means "nothing is ignored", which is an answer, not a
	// failure; any other error leaves the listing undecorated.
	out, _ := cmd.Output()
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			ignored[strings.TrimSuffix(path, "/")] = true
		}
	}
	return ignored
}
