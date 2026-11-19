//go:build windows

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/UserExistsError/conpty"
)

type PTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Wait(ctx context.Context) (int, error)
	Close() error
}

type windowsPTY struct {
	cpty *conpty.ConPty
}

func (p *windowsPTY) Read(buf []byte) (int, error) {
	return p.cpty.Read(buf)
}

func (p *windowsPTY) Write(buf []byte) (int, error) {
	return p.cpty.Write(buf)
}

func (p *windowsPTY) Resize(cols, rows int) error {
	return p.cpty.Resize(cols, rows)
}

func (p *windowsPTY) Wait(ctx context.Context) (int, error) {
	code, err := p.cpty.Wait(ctx)
	return int(code), err
}

func (p *windowsPTY) Close() error {
	return p.cpty.Close()
}

func startPTY(cwd string, shellCmd string, cols, rows int, extraEnv []string) (PTY, string, error) {
	var cmdLine string
	var label string

	if shellCmd != "" {
		cmdLine = shellCmd
		label = filepath.Base(shellCmd)
		if idx := strings.Index(label, " "); idx != -1 {
			label = label[:idx]
		}
		label = strings.TrimSuffix(label, ".exe")
	} else {
		// Prefer pwsh.exe if on PATH, else powershell.exe (with -NoLogo), else cmd.exe
		if pwshPath, err := exec.LookPath("pwsh.exe"); err == nil && pwshPath != "" {
			cmdLine = `"` + pwshPath + `" -NoLogo`
			label = "PowerShell"
		} else if psPath, err := exec.LookPath("powershell.exe"); err == nil && psPath != "" {
			cmdLine = `"` + psPath + `" -NoLogo`
			label = "PowerShell"
		} else if comspec := os.Getenv("ComSpec"); comspec != "" {
			cmdLine = `"` + comspec + `"`
			label = "cmd"
		} else {
			cmdLine = "cmd.exe"
			label = "cmd"
		}
	}

	// Resolve cwd: if empty or invalid directory, fall back to os.UserHomeDir()
	if cwd == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			cwd = home
		}
	} else {
		if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				cwd = home
			}
		}
	}

	// Build environment inheriting os.Environ() plus TERM=xterm-256color, COLORTERM=truecolor, TERM_PROGRAM=Orchestrator, and any extra env.
	mergedEnv := os.Environ()
	mergedEnv = append(mergedEnv,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=Orchestrator",
	)
	if len(extraEnv) > 0 {
		mergedEnv = append(mergedEnv, extraEnv...)
	}

	cpty, err := conpty.Start(
		cmdLine,
		conpty.ConPtyDimensions(cols, rows),
		conpty.ConPtyWorkDir(cwd),
		conpty.ConPtyEnv(mergedEnv),
	)
	if err != nil {
		return nil, "", fmt.Errorf("conpty.Start failed: %w", err)
	}

	return &windowsPTY{cpty: cpty}, label, nil
}
