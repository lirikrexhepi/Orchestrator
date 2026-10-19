//go:build darwin

package reveal

import (
	"os"
	"os/exec"
)

func open(target string) error {
	args := []string{target}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		args = []string{"-R", target}
	}
	return exec.Command("open", args...).Start()
}

func launch(target string) error {
	return exec.Command("open", target).Start()
}
