//go:build !windows && !darwin

package reveal

import (
	"os"
	"os/exec"
	"path/filepath"
)

// open uses the freedesktop handler, which takes a directory rather than a file
// to select, so a file is reduced to its parent.
func open(target string) error {
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		target = filepath.Dir(target)
	}
	return exec.Command("xdg-open", target).Start()
}

func launch(target string) error {
	return exec.Command("xdg-open", target).Start()
}
