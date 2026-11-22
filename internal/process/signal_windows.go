//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000,
	}
}

// Windows has no SIGTERM. Terminate the entire Job Object when KillTree is
// active (killing grandchild Node processes behind .cmd shims), falling back
// to killing the direct child process.
func terminate(p *Process) {
	if terminateJob(p) {
		return
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}
