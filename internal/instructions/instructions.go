package instructions

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const maxContent = 256 * 1024

type File struct {
	ID        string `json:"id"`
	Group     string `json:"group"`
	Label     string `json:"label"`
	Path      string `json:"path,omitempty"`
	Account   string `json:"account,omitempty"`
	Exists    bool   `json:"exists"`
	Content   string `json:"content,omitempty"`
	Size      int64  `json:"size"`
	Modified  int64  `json:"modified,omitempty"`
	Note      string `json:"note,omitempty"`
	Warning   bool   `json:"warning,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type Source struct {
	Driver string `json:"driver"`
	Name   string `json:"name"`
	Files  []File `json:"files"`
}

type ClaudeAccount struct {
	ID      string
	Name    string
	Dir     string
	Default bool
	InUse   bool
}

type Input struct {
	Home              string
	Cwd               string
	ClaudeAccounts    []ClaudeAccount
	OpenCodeMajor     int
	OpenCodeConfigDir string
	AppNote           string
	ProjectMemoryPath string
}

const (
	GroupGlobal  = "Global"
	GroupProject = "This project"
	GroupMemory  = "Auto memory"
	GroupApp     = "Added by this app"
)

func Collect(in Input) []Source {
	if in.Home == "" {
		in.Home, _ = os.UserHomeDir()
	}
	if in.Cwd != "" {
		in.Cwd = filepath.Clean(in.Cwd)
	}
	return []Source{claude(in), antigravity(in), openCode(in), app(in)}
}

type collector struct {
	driver string
	files  []File
	seen   map[string]bool
}

func newCollector(driver string) *collector {
	return &collector{driver: driver, seen: make(map[string]bool)}
}

func key(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func (c *collector) has(path string) bool {
	return c.seen[key(path)]
}

func (c *collector) add(file File) *File {
	if file.Path != "" {
		if c.has(file.Path) {
			return nil
		}
		c.seen[key(file.Path)] = true
	}
	if file.ID == "" {
		file.ID = c.driver + ":" + file.Group + ":" + file.Account + ":" + firstNonEmpty(file.Path, file.Label)
	}
	c.files = append(c.files, file)
	return &c.files[len(c.files)-1]
}

func (c *collector) addPath(path, group, label, note string) *File {
	file := read(path)
	file.Group, file.Label, file.Note = group, label, note
	return c.add(file)
}

func (c *collector) addIfExists(path, group, label, note string) *File {
	if !isFile(path) {
		return nil
	}
	return c.addPath(path, group, label, note)
}

func read(path string) File {
	file := File{Path: path}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return file
	}
	file.Exists = true
	file.Size = info.Size()
	file.Modified = info.ModTime().UnixMilli()
	handle, err := os.Open(path)
	if err != nil {
		return file
	}
	defer handle.Close()
	buf := make([]byte, maxContent+1)
	n, _ := handle.Read(buf)
	for n < len(buf) {
		more, readErr := handle.Read(buf[n:])
		if more <= 0 || readErr != nil {
			break
		}
		n += more
	}
	if n > maxContent {
		n = maxContent
		file.Truncated = true
	}
	file.Content = string(buf[:n])
	return file
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func markdownUnder(root string) []string {
	if !isDir(root) {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func ancestors(start string, stop func(dir string) bool) []string {
	if start == "" {
		return nil
	}
	var dirs []string
	dir := filepath.Clean(start)
	for {
		dirs = append(dirs, dir)
		if stop != nil && stop(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return dirs
}

func outermostFirst(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, dir := range dirs {
		out[len(dirs)-1-i] = dir
	}
	return out
}

func repoRoot(start string) string {
	for _, dir := range ancestors(start, nil) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
	}
	return ""
}

func samePath(left, right string) bool {
	return left != "" && right != "" && key(left) == key(right)
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func display(path, home string) string {
	if home != "" && within(path, home) {
		rel, _ := filepath.Rel(home, path)
		if rel == "." {
			return "~"
		}
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

func relativeLabel(path, base string) string {
	if base != "" && within(path, base) {
		if rel, err := filepath.Rel(base, path); err == nil && rel != "." {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

func Slug(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

var (
	fencePattern  = regexp.MustCompile("(?s)```.*?```")
	inlinePattern = regexp.MustCompile("`[^`\n]*`")
	importPattern = regexp.MustCompile(`(?m)(?:^|[\s(])@(~?[\w./\\:-]+)`)
)

func imports(content, base, home string) []string {
	stripped := inlinePattern.ReplaceAllString(fencePattern.ReplaceAllString(content, ""), "")
	var out []string
	for _, match := range importPattern.FindAllStringSubmatch(stripped, -1) {
		target := strings.TrimRight(match[1], ".,;:)")
		if target == "" {
			continue
		}
		target = expandHome(target, home)
		if !filepath.IsAbs(target) {
			target = filepath.Join(base, target)
		}
		if isFile(target) {
			out = append(out, filepath.Clean(target))
		}
	}
	return out
}
