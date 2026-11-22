//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Signal the whole process group so agent CLIs that fork helpers tear down with
// the parent instead of leaking.
func terminate(p *Process) {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if pgid, err := syscall.Getpgid(p.cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
}
