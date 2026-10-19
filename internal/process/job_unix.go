//go:build !windows

package process

func attachJob(*Process, bool) {}

func closeJob(*Process) bool { return false }
