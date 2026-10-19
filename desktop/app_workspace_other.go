//go:build !windows

package main

import (
	"context"
	"os/exec"
)

func workspaceCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func startDetached(binary, path string) error {
	return exec.Command(binary, path).Start()
}
