//go:build !windows

package browser

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func hideConsoleWindow(cmd *exec.Cmd) {
	// No-op on non-Windows platforms
}

func isLiveSingletonTarget(target string) bool {
	idx := strings.LastIndex(target, "-")
	if idx < 0 || idx+1 >= len(target) {
		return false
	}
	host := target[:idx]
	if currentHost, err := os.Hostname(); err == nil && currentHost != "" && host != "" && host != currentHost {
		return false
	}
	pid, err := strconv.Atoi(target[idx+1:])
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
