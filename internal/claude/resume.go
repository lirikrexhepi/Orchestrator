package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func sessionConfigDir(env map[string]string) string {
	for key, value := range env {
		if strings.EqualFold(key, "CLAUDE_CONFIG_DIR") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return configDir(home)
}

func resumeCwd(config, sessionID, cwd string) (string, error) {
	if config == "" || sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return cwd, nil
	}
	matches, _ := filepath.Glob(filepath.Join(config, "projects", "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return cwd, fmt.Errorf("claude session %s not found under %s", sessionID, filepath.Join(config, "projects"))
	}
	own := projectDirName(cwd)
	for _, path := range matches {
		if strings.EqualFold(filepath.Base(filepath.Dir(path)), own) {
			return cwd, nil
		}
	}
	for _, path := range matches {
		recorded := transcriptCwd(path)
		if recorded == "" {
			continue
		}
		if samePath(recorded, cwd) {
			return cwd, nil
		}
		if info, err := os.Stat(recorded); err == nil && info.IsDir() {
			return recorded, nil
		}
	}
	return cwd, nil
}

func transcriptCwd(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for i := 0; i < 50; i++ {
		line, readErr := reader.ReadBytes('\n')
		var record struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(line, &record) == nil && record.Cwd != "" {
			return record.Cwd
		}
		if readErr != nil {
			return ""
		}
	}
	return ""
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
