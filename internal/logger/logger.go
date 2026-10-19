package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const rotateBytes = 20 * 1024 * 1024

var (
	mu      sync.Mutex
	writers []io.Writer
	debug   = os.Getenv("ORCHESTRATOR_DEBUG") != ""
)

func DebugEnabled() bool {
	return debug
}

func rotate(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < rotateBytes {
		return
	}
	ext := filepath.Ext(path)
	_ = os.Rename(path, strings.TrimSuffix(path, ext)+".old"+ext)
}

func Init(logPaths ...string) {
	mu.Lock()
	defer mu.Unlock()

	writers = []io.Writer{os.Stderr}

	for _, p := range logPaths {
		if p == "" {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(p), 0755)
		rotate(p)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			writers = append(writers, f)
			stamp := time.Now().Format("2006-01-02 15:04:05.000")
			_, _ = fmt.Fprintf(f, "\n=== Orchestrator Log Session Started at %s ===\n", stamp)
		}
	}
}

func logMessage(level, tag, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()

	stamp := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	entry := fmt.Sprintf("[%s] [%-5s] [%-12s] %s\n", stamp, level, tag, msg)

	for _, w := range writers {
		_, _ = io.WriteString(w, entry)
	}
}

func Debugf(tag, format string, args ...any) {
	if !debug {
		return
	}
	logMessage("DEBUG", tag, format, args...)
}

func Infof(tag, format string, args ...any) {
	logMessage("INFO", tag, format, args...)
}

func Warnf(tag, format string, args ...any) {
	logMessage("WARN", tag, format, args...)
}

func Errorf(tag, format string, args ...any) {
	logMessage("ERROR", tag, format, args...)
}
