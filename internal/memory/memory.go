package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const (
	projectMemoryFile = "MEMORY.md"
	workspaceRecapFile = "RECAP.md"
)

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) projectDir(cwd string) string {
	clean := filepath.Clean(strings.TrimSpace(cwd))
	sum := sha1.Sum([]byte(clean))
	base := filepath.Base(clean)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "project"
	}
	return filepath.Join(s.root, "projects", sanitize(base)+"-"+hex.EncodeToString(sum[:])[:12])
}

func (s *Store) workspaceDir(workspaceID string) string {
	return filepath.Join(s.root, "workspaces", sanitize(workspaceID))
}

func sanitize(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" || name == "." || name == ".." {
		return "_"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func readFile(path string) string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(payload)
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (s *Store) LoadProjectMemory(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return readFile(filepath.Join(s.projectDir(cwd), projectMemoryFile))
}

func (s *Store) SaveProjectMemory(cwd, content string) error {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}
	return writeFile(filepath.Join(s.projectDir(cwd), projectMemoryFile), content)
}

func (s *Store) LoadRecap(workspaceID string) string {
	if workspaceID == "" {
		return ""
	}
	return readFile(filepath.Join(s.workspaceDir(workspaceID), workspaceRecapFile))
}

func (s *Store) SaveRecap(workspaceID, content string) error {
	if workspaceID == "" {
		return nil
	}
	return writeFile(filepath.Join(s.workspaceDir(workspaceID), workspaceRecapFile), content)
}

func (s *Store) ProjectMemoryPath(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return filepath.Join(s.projectDir(cwd), projectMemoryFile)
}
