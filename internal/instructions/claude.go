package instructions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const maxImportDepth = 4

func managedClaudePath() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\Program Files\ClaudeCode\CLAUDE.md`
	case "darwin":
		return "/Library/Application Support/ClaudeCode/CLAUDE.md"
	default:
		return "/etc/claude-code/CLAUDE.md"
	}
}

func claude(in Input) Source {
	c := newCollector("claude")

	c.addIfExists(managedClaudePath(), GroupGlobal, "Managed policy CLAUDE.md", "Set by your organisation. Applies to every Claude chat on this machine.")

	var main *ClaudeAccount
	for i := range in.ClaudeAccounts {
		if in.ClaudeAccounts[i].Default {
			main = &in.ClaudeAccounts[i]
		}
	}
	multi := len(in.ClaudeAccounts) > 1

	for _, account := range in.ClaudeAccounts {
		if account.Dir == "" {
			continue
		}
		label := "Global CLAUDE.md"
		accountName := ""
		if multi {
			accountName = account.Name
		}
		note := "Your personal instructions. Loaded into every Claude chat."
		if multi {
			note = "Loaded into every Claude chat that uses the " + account.Name + " account."
		}
		if account.InUse && multi && in.Cwd != "" {
			note += " This project uses this account."
		}
		path := filepath.Join(account.Dir, "CLAUDE.md")
		file := read(path)
		file.Group, file.Label, file.Note, file.Account = GroupGlobal, label, note, accountName
		if main != nil && !account.Default && main.Dir != "" {
			reference := read(filepath.Join(main.Dir, "CLAUDE.md"))
			switch {
			case reference.Exists && !file.Exists:
				file.Warning = true
				file.Note = "Missing. Chats on the " + account.Name + " account don't get the instructions in " + display(reference.Path, in.Home) + "."
			case reference.Exists && file.Exists && strings.TrimSpace(reference.Content) != strings.TrimSpace(file.Content):
				file.Warning = true
				file.Note = "Different from " + display(reference.Path, in.Home) + ". Chats on the " + account.Name + " account only see this copy."
			}
		}
		if added := c.add(file); added != nil {
			c.addImports(*added, in.Home, 1)
		}

		for _, rule := range markdownUnder(filepath.Join(account.Dir, "rules")) {
			if added := c.addPath(rule, GroupGlobal, "Rule: "+relativeLabel(rule, filepath.Join(account.Dir, "rules")), "Global rule file."); added != nil {
				added.Account = accountName
			}
		}
		c.addOutputStyle(account.Dir, in.Cwd, accountName)
	}

	if in.Cwd != "" {
		for _, dir := range outermostFirst(ancestors(in.Cwd, nil)) {
			scope := "Loaded because this project is inside " + display(dir, in.Home) + "."
			if samePath(dir, in.Cwd) {
				scope = "Loaded for chats in this project."
			}
			for _, candidate := range []struct{ path, label string }{
				{filepath.Join(dir, "CLAUDE.md"), "CLAUDE.md"},
				{filepath.Join(dir, ".claude", "CLAUDE.md"), ".claude/CLAUDE.md"},
				{filepath.Join(dir, "CLAUDE.local.md"), "CLAUDE.local.md (personal, not committed)"},
			} {
				label := candidate.label
				if !samePath(dir, in.Cwd) {
					label = display(dir, in.Home) + " · " + candidate.label
				}
				if added := c.addIfExists(candidate.path, GroupProject, label, scope); added != nil {
					c.addImports(*added, in.Home, 1)
				}
			}
			rules := filepath.Join(dir, ".claude", "rules")
			for _, rule := range markdownUnder(rules) {
				c.addPath(rule, GroupProject, "Rule: "+relativeLabel(rule, rules), "Project rule file.")
			}
		}

		for _, account := range in.ClaudeAccounts {
			if account.Dir == "" {
				continue
			}
			accountName := ""
			if multi {
				accountName = account.Name
			}
			c.addAutoMemory(account.Dir, in.Cwd, accountName)
			if root := repoRoot(in.Cwd); root != "" && !samePath(root, in.Cwd) {
				c.addAutoMemory(account.Dir, root, accountName)
			}
		}
	}

	return Source{Driver: "claude", Name: "Claude Code", Files: c.files}
}

func (c *collector) addImports(parent File, home string, depth int) {
	if depth > maxImportDepth || !parent.Exists || parent.Content == "" {
		return
	}
	for _, target := range imports(parent.Content, filepath.Dir(parent.Path), home) {
		added := c.addPath(target, parent.Group, "Imported: "+filepath.Base(target), "Pulled in by @"+filepath.Base(target)+" in "+filepath.Base(parent.Path)+".")
		if added == nil {
			continue
		}
		added.Account = parent.Account
		c.addImports(*added, home, depth+1)
	}
}

func (c *collector) addAutoMemory(configDir, project, accountName string) {
	dir := filepath.Join(configDir, "projects", Slug(project), "memory")
	files := markdownUnder(dir)
	if len(files) == 0 {
		return
	}
	sort.SliceStable(files, func(i, j int) bool {
		return strings.EqualFold(filepath.Base(files[i]), "MEMORY.md") && !strings.EqualFold(filepath.Base(files[j]), "MEMORY.md")
	})
	for _, path := range files {
		note := "Saved by Claude. Read when relevant to the task."
		if strings.EqualFold(filepath.Base(path), "MEMORY.md") {
			note = "Memory index Claude keeps for this project. Its first 200 lines load into every chat here."
		}
		if added := c.addPath(path, GroupMemory, relativeLabel(path, dir), note); added != nil {
			added.Account = accountName
		}
	}
}

func readSettings(path string) map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var settings map[string]any
	if json.Unmarshal([]byte(stripJSONC(string(raw))), &settings) != nil {
		return nil
	}
	return settings
}

func (c *collector) addOutputStyle(configDir, cwd, accountName string) {
	style := ""
	dirs := []string{filepath.Join(configDir, "output-styles")}
	for _, path := range []string{filepath.Join(configDir, "settings.json")} {
		if value, ok := readSettings(path)["outputStyle"].(string); ok {
			style = value
		}
	}
	if cwd != "" {
		if root := firstNonEmpty(repoRoot(cwd), cwd); root != "" {
			dirs = append([]string{filepath.Join(root, ".claude", "output-styles")}, dirs...)
			for _, path := range []string{filepath.Join(root, ".claude", "settings.json"), filepath.Join(root, ".claude", "settings.local.json")} {
				if value, ok := readSettings(path)["outputStyle"].(string); ok {
					style = value
				}
			}
		}
	}
	style = strings.TrimSpace(style)
	if style == "" || strings.EqualFold(style, "default") {
		return
	}
	for _, dir := range dirs {
		for _, path := range markdownUnder(dir) {
			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			file := read(path)
			if strings.EqualFold(name, style) || strings.EqualFold(frontmatterName(file.Content), style) {
				file.Group, file.Label, file.Account = GroupGlobal, "Output style: "+style, accountName
				file.Note = "Active output style. It changes how Claude writes every reply."
				c.add(file)
				return
			}
		}
	}
	c.add(File{
		Group: GroupGlobal, Label: "Output style: " + style, Account: accountName, Exists: true,
		Note: "Built-in output style selected in settings.json. It changes how Claude writes every reply.",
	})
}

func frontmatterName(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "\n---")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(content[3:3+end], "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "name:"); ok {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}
