package instructions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func antigravity(in Input) Source {
	c := newCollector("antigravity")
	gemini := filepath.Join(in.Home, ".gemini")
	config := filepath.Join(gemini, "config")

	found := false
	for _, path := range []string{
		filepath.Join(config, "GEMINI.md"),
		filepath.Join(config, "AGENTS.md"),
		filepath.Join(gemini, "GEMINI.md"),
		filepath.Join(gemini, "AGENTS.md"),
	} {
		if c.addIfExists(path, GroupGlobal, "Global "+filepath.Base(path), "Global rules. Loaded into every Antigravity chat.") != nil {
			found = true
		}
	}
	if !found {
		c.addPath(filepath.Join(config, "AGENTS.md"), GroupGlobal, "Global AGENTS.md", "Antigravity reads global rules from here (GEMINI.md works too).")
	}
	rules := filepath.Join(config, "rules")
	for _, rule := range markdownUnder(rules) {
		c.addPath(rule, GroupGlobal, "Rule: "+relativeLabel(rule, rules), "Global rule file.")
	}

	if in.Cwd != "" {
		root := repoRoot(in.Cwd)
		dirs := []string{in.Cwd}
		if root != "" {
			dirs = ancestors(in.Cwd, func(dir string) bool { return samePath(dir, root) })
		}
		for _, dir := range outermostFirst(dirs) {
			prefix := ""
			if !samePath(dir, in.Cwd) {
				prefix = display(dir, in.Home) + " · "
			}
			for _, name := range []string{"GEMINI.md", "AGENTS.md"} {
				c.addIfExists(filepath.Join(dir, name), GroupProject, prefix+name, "Project rules. Loaded when the agent works on files under this folder.")
			}
			for _, folder := range []string{".agents", ".agent", "_agents", "_agent"} {
				base := filepath.Join(dir, folder, "rules")
				for _, rule := range markdownUnder(base) {
					c.addPath(rule, GroupProject, "Rule: "+folder+"/rules/"+relativeLabel(rule, base), "Project rule file.")
				}
			}
		}
	}

	knowledge := filepath.Join(gemini, "antigravity-cli", "knowledge")
	for _, path := range markdownUnder(knowledge) {
		c.addPath(path, GroupMemory, relativeLabel(path, knowledge), "Knowledge item Antigravity saved. Used when relevant.")
	}

	return Source{Driver: "antigravity", Name: "Antigravity", Files: c.files}
}

func openCodeConfigDir(in Input) string {
	if in.OpenCodeConfigDir != "" {
		return in.OpenCodeConfigDir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	return filepath.Join(in.Home, ".config", "opencode")
}

func openCode(in Input) Source {
	c := newCollector("opencode")
	config := openCodeConfigDir(in)
	v2 := in.OpenCodeMajor >= 2
	claudeFallback := !v2 && strings.TrimSpace(os.Getenv("OPENCODE_DISABLE_CLAUDE_CODE")) == ""

	global := filepath.Join(config, "AGENTS.md")
	note := "Global rules. Loaded into every OpenCode chat."
	if !isFile(global) && claudeFallback {
		note = "Not created, so OpenCode 1 uses your Claude CLAUDE.md below instead."
	}
	c.addPath(global, GroupGlobal, "Global AGENTS.md", note)
	if !isFile(global) && claudeFallback {
		c.addIfExists(filepath.Join(in.Home, ".claude", "CLAUDE.md"), GroupGlobal, "Claude's global CLAUDE.md", "OpenCode 1 falls back to this because there is no global AGENTS.md. OpenCode 2 ignores CLAUDE.md.")
	}
	c.addConfigInstructions(filepath.Join(config, "opencode.json"), config, in.Home, GroupGlobal)
	c.addConfigInstructions(filepath.Join(config, "opencode.jsonc"), config, in.Home, GroupGlobal)

	if in.Cwd != "" {
		root := repoRoot(in.Cwd)
		var dirs []string
		switch {
		case v2 && within(in.Cwd, in.Home):
			dirs = ancestors(in.Cwd, func(dir string) bool { return samePath(dir, in.Home) })
		case root != "":
			dirs = ancestors(in.Cwd, func(dir string) bool { return samePath(dir, root) })
		default:
			dirs = []string{in.Cwd}
		}
		names := []string{"AGENTS.md"}
		if claudeFallback {
			names = append(names, "CLAUDE.md")
		}
		for _, name := range names {
			added := false
			for _, dir := range outermostFirst(dirs) {
				label := name
				if !samePath(dir, in.Cwd) {
					label = display(dir, in.Home) + " · " + name
				}
				detail := "Project rules. Loaded into OpenCode chats in this project."
				if name == "CLAUDE.md" {
					detail = "OpenCode 1 uses CLAUDE.md here because the project has no AGENTS.md."
				}
				if c.addIfExists(filepath.Join(dir, name), GroupProject, label, detail) != nil {
					added = true
				}
			}
			if added {
				break
			}
		}
		project := firstNonEmpty(root, in.Cwd)
		for _, path := range []string{
			filepath.Join(project, "opencode.json"),
			filepath.Join(project, "opencode.jsonc"),
			filepath.Join(project, ".opencode", "opencode.json"),
			filepath.Join(project, ".opencode", "opencode.jsonc"),
		} {
			c.addConfigInstructions(path, project, in.Home, GroupProject)
		}
	}

	name := "OpenCode"
	if v2 {
		name = "OpenCode 2"
	}
	return Source{Driver: "opencode", Name: name, Files: c.files}
}

func (c *collector) addConfigInstructions(configPath, base, home, group string) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	var config struct {
		Instructions []string `json:"instructions"`
	}
	if json.Unmarshal([]byte(stripJSONC(string(raw))), &config) != nil {
		return
	}
	for _, entry := range config.Instructions {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "http://") || strings.HasPrefix(entry, "https://") {
			c.add(File{Group: group, Label: entry, Exists: true, Note: "Fetched from the web by the instructions list in " + filepath.Base(configPath) + "."})
			continue
		}
		pattern := expandHome(entry, home)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(base, pattern)
		}
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if isFile(match) {
				c.addPath(match, group, relativeLabel(match, base), "Listed under instructions in "+filepath.Base(configPath)+".")
			}
		}
	}
}

func app(in Input) Source {
	c := newCollector("app")
	if strings.TrimSpace(in.AppNote) != "" {
		c.add(File{
			Group: GroupApp, Label: "Orchestrator note", Exists: true, Content: in.AppNote, Size: int64(len(in.AppNote)),
			Note: "Sent by this app with every chat on every provider. It shapes how agents report progress.",
		})
	}
	if in.ProjectMemoryPath != "" {
		c.addIfExists(in.ProjectMemoryPath, GroupApp, "Orchestrator project memory", "Given to the orchestrator chat for this project.")
	}
	return Source{Driver: "app", Name: "This app", Files: c.files}
}

func stripJSONC(text string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			b.WriteByte(ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(text) && text[i+1] == '/' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		if ch == '/' && i+1 < len(text) && text[i+1] == '*' {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				break
			}
			i += end + 3
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(text) && strings.ContainsRune(" \t\r\n", rune(text[j])) {
				j++
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}
