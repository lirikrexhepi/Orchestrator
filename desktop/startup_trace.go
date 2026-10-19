package main

import (
	"os"
	"strings"
	"time"

	"composer/internal/logger"
)

var processStart = time.Now()

func traceStartup(phase string) {
	logger.Infof("Startup", "%s +%dms", phase, time.Since(processStart).Milliseconds())
}

func profileName() string {
	return strings.TrimSpace(os.Getenv("ORCHESTRATOR_PROFILE"))
}

func profileSuffix() string {
	if p := profileName(); p != "" {
		return "-" + p
	}
	return ""
}
