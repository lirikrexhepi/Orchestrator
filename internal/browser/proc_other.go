//go:build !windows

package browser

import "os/exec"

func hideConsoleWindow(cmd *exec.Cmd) {
	// No-op on non-Windows platforms
}
