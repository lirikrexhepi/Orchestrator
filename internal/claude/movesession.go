package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"composer/internal/provider"
)

func projectDirName(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (a *Adapter) MoveSession(sessionID, cwd string) error {
	config := sessionConfigDir(provider.LaunchEnv(a.settings))
	if config == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return fmt.Errorf("no Claude session to move")
	}
	return moveSessionFile(config, sessionID, cwd)
}

func moveSessionFile(config, sessionID, cwd string) error {
	if strings.TrimSpace(cwd) == "" || !filepath.IsAbs(cwd) {
		return fmt.Errorf("a project folder is required to move a session")
	}
	matches, _ := filepath.Glob(filepath.Join(config, "projects", "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return fmt.Errorf("claude session %s not found under %s", sessionID, filepath.Join(config, "projects"))
	}
	target := projectDirName(cwd)
	for _, match := range matches {
		if strings.EqualFold(filepath.Base(filepath.Dir(match)), target) {
			return nil
		}
	}
	source := matches[0]
	destinationDir := filepath.Join(config, "projects", target)
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, filepath.Join(destinationDir, sessionID+".jsonl")); err != nil {
		return err
	}
	sidecar := filepath.Join(filepath.Dir(source), sessionID)
	if info, err := os.Stat(sidecar); err == nil && info.IsDir() {
		_ = os.Rename(sidecar, filepath.Join(destinationDir, sessionID))
	}
	return nil
}
