//go:build windows

package reveal

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// open hands the path to Explorer.
//
// A file is passed with /select, so Explorer opens the containing folder with
// that entry highlighted. Explorer exits with a non-zero status even when it
// succeeds, so its exit code is deliberately not treated as failure.
func open(target string) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		absolute = target
	}

	args := []string{absolute}
	if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
		args = []string{"/select,", absolute}
	}

	cmd := exec.Command("explorer.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}

func launch(target string) error {
	absolute, err := filepath.Abs(target)
	if err != nil {
		absolute = target
	}
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", absolute)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}
