//go:build windows

package updater

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// LaunchInstaller starts the downloaded NSIS installer silently as a detached
// process so it survives Orchestrator exiting, updates in place, and relaunches.
func LaunchInstaller(installerPath, installDir string) error {
	// NSIS requires /D=<dir> to be the LAST argument and UNQUOTED even when
	// the path contains spaces. Build the exact Windows command line explicitly.
	cleanInstaller := filepath.Clean(installerPath)
	cmdLine := fmt.Sprintf(`"%s" /S /RELAUNCH`, cleanInstaller)
	if trimmed := strings.TrimSpace(installDir); trimmed != "" {
		cmdLine += " /D=" + filepath.Clean(trimmed)
	}

	cmd := exec.Command(cleanInstaller)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
		CmdLine:       cmdLine,
	}
	return cmd.Start()
}
