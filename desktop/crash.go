package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"composer/internal/logger"
)

func captureCrashes(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger.Warnf("Main", "Crash capture unavailable: %v", err)
		return
	}
	_, _ = fmt.Fprintf(f, "\n=== PID %d started %s ===\n", os.Getpid(), time.Now().Format("2006-01-02 15:04:05"))
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		logger.Warnf("Main", "Crash capture unavailable: %v", err)
	}
	_ = f.Close()
}
