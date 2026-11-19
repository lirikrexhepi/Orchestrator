//go:build !windows

package terminal

import (
	"context"
	"errors"
)

type PTY interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Wait(ctx context.Context) (int, error)
	Close() error
}

func startPTY(cwd string, shellCmd string, cols, rows int, extraEnv []string) (PTY, string, error) {
	return nil, "", errors.New("terminal: ConPTY is only supported on Windows")
}
