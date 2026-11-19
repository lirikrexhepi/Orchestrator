package antigravity

import (
	"encoding/json"
	"os"
	"strings"

	"composer/internal/domain"
)

const (
	maxBaselineBytes = 512 * 1024
	diffContextLines = 3
)

var hiddenTools = map[string]bool{
	"manage_task": true,
	"schedule":    true,
}

var editToolNames = map[string]bool{
	"write_to_file":              true,
	"replace_file_content":       true,
	"multi_replace_file_content": true,
	"edit_file":                  true,
	"write_file":                 true,
	"create_file":                true,
	"patch_file":                 true,
	"multi_edit":                 true,
}

var readToolNames = map[string]bool{
	"view_file": true,
	"read_file": true,
	"read":      true,
}

func isEditTool(name string) bool {
	return editToolNames[strings.ToLower(name)]
}

func isReadTool(name string) bool {
	return readToolNames[strings.ToLower(name)]
}

var toolPathCandidates = []string{
	"targetfile",
	"target_file",
	"absolutepath",
	"absolute_path",
	"filepath",
	"file_path",
	"path",
	"directorypath",
	"directory_path",
	"file",
}

func toolPath(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	for _, candidate := range toolPathCandidates {
		for k, v := range input {
			if strings.EqualFold(k, candidate) {
				if s, ok := v.(string); ok && s != "" {
					return normalizeACPPath(s)
				}
			}
		}
	}
	return ""
}

func normalizeACPPath(p string) string {
	if len(p) >= 4 && p[0] == '/' && ((p[1] >= 'a' && p[1] <= 'z') || (p[1] >= 'A' && p[1] <= 'Z')) && p[2] == ':' && (p[3] == '/' || p[3] == '\\') {
		return p[1:]
	}
	return p
}

func extractRawOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func mapToolStatus(status string) domain.ToolStatus {
	switch strings.ToLower(status) {
	case "pending":
		return domain.ToolPending
	case "in_progress":
		return domain.ToolInProgress
	case "completed":
		return domain.ToolCompleted
	case "failed", "error":
		return domain.ToolFailed
	default:
		return domain.ToolPending
	}
}

func readCapped(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxBaselineBytes {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (s *session) rememberFile(path string) {
	if path == "" {
		return
	}
	content, ok := readCapped(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.baselines[path] = content
	} else {
		delete(s.baselines, path)
	}
}

func (s *session) captureBaseline(path string) {
	if path == "" {
		return
	}
	s.mu.Lock()
	_, known := s.baselines[path]
	s.mu.Unlock()
	if known {
		return
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		s.mu.Lock()
		s.baselines[path] = ""
		s.mu.Unlock()
		return
	}
	s.rememberFile(path)
}

func (s *session) editDiff(path string) []domain.FileDiff {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	before, known := s.baselines[path]
	s.mu.Unlock()
	after, ok := readCapped(path)
	s.mu.Lock()
	if ok {
		s.baselines[path] = after
	} else {
		delete(s.baselines, path)
	}
	s.mu.Unlock()
	if !known || !ok || before == after {
		return nil
	}
	oldText, newText := trimToChange(before, after)
	return []domain.FileDiff{{Path: path, OldText: oldText, NewText: newText}}
}

func trimToChange(before, after string) (string, string) {
	a := strings.Split(strings.ReplaceAll(before, "\r\n", "\n"), "\n")
	b := strings.Split(strings.ReplaceAll(after, "\r\n", "\n"), "\n")
	if before == "" {
		a = nil
	}
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	start := max(0, prefix-diffContextLines)
	endA := min(len(a), len(a)-suffix+diffContextLines)
	endB := min(len(b), len(b)-suffix+diffContextLines)
	return strings.Join(a[start:endA], "\n"), strings.Join(b[start:endB], "\n")
}
