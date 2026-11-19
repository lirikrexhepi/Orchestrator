package domain

import (
	"fmt"
	"strconv"
	"strings"
)

var (
	writeToolNames = map[string]struct{}{
		"write":         {},
		"write_file":    {},
		"write_to_file": {},
		"writefile":     {},
		"create":        {},
		"create_file":   {},
	}
	editToolNames = map[string]struct{}{
		"edit":                       {},
		"edit_file":                  {},
		"editfile":                   {},
		"notebookedit":               {},
		"replace_file_content":       {},
		"multi_replace_file_content": {},
		"multi_edit":                 {},
		"multiedit":                  {},
		"patch":                      {},
		"patch_file":                 {},
		"apply_patch":                {},
		"filechange":                 {},
	}
	readToolNames = map[string]struct{}{
		"read":             {},
		"read_file":        {},
		"readfile":         {},
		"view_file":        {},
		"cat":              {},
		"read_url_content": {},
		"webfetch":         {},
	}
	searchToolNames = map[string]struct{}{
		"glob":         {},
		"grep":         {},
		"grep_search":  {},
		"find_by_name": {},
		"list_dir":     {},
		"listfiles":    {},
		"search":       {},
		"find":         {},
		"websearch":    {},
	}
	commandToolNames = map[string]struct{}{
		"bash":             {},
		"powershell":       {},
		"run_command":      {},
		"commandexecution": {},
		"terminal":         {},
		"cmd":              {},
		"command":          {},
		"sh":               {},
		"shell":            {},
	}
	todoToolNames = map[string]struct{}{
		"todowrite":   {},
		"manage_task": {},
		"todo":        {},
	}
	subagentToolNames = map[string]struct{}{
		"task":          {},
		"agent":         {},
		"subagent":      {},
		"delegate_task": {},
	}

	filePathKeys = []string{
		"targetfile",
		"target_file",
		"absolutepath",
		"absolute_path",
		"filepath",
		"file_path",
		"path",
		"directorypath",
		"directory_path",
		"notebookpath",
		"file",
		"filename",
		"uri",
		"url",
		"target",
	}

	commandKeys = []string{
		"command",
		"commandline",
		"cmd",
		"script",
		"input",
	}

	searchQueryKeys = []string{
		"pattern",
		"query",
		"search_term",
		"searchterm",
		"regex",
		"glob",
	}

	searchPathKeys = []string{
		"searchpath",
		"searchdirectory",
		"search_path",
		"directory",
		"dir",
		"path",
		"cwd",
	}

	actionKeys = []string{
		"action",
		"toolaction",
		"tool_action",
	}

	titleKeys = []string{
		"title",
	}

	summaryKeys = []string{
		"toolsummary",
		"tool_summary",
	}
)

// NormalizeEvent idempotently normalizes tool call fields on ev.Tool if present.
func NormalizeEvent(ev *RuntimeEvent) {
	if ev != nil && ev.Tool != nil {
		NormalizeToolCall(ev.Tool)
	}
}

// NormalizeToolCall idempotently populates missing canonical fields on a ToolCall.
func NormalizeToolCall(tool *ToolCall) {
	if tool == nil {
		return
	}

	nameLower := strings.ToLower(strings.TrimSpace(tool.Name))
	kindLower := strings.ToLower(strings.TrimSpace(tool.Kind))

	// 1. Category (if empty)
	if tool.Category == "" {
		if len(tool.Diffs) > 0 || kindLower == "filechange" {
			if _, ok := writeToolNames[nameLower]; ok {
				tool.Category = ToolCategoryWrite
			} else {
				tool.Category = ToolCategoryEdit
			}
		} else if strings.HasPrefix(nameLower, "mcp__") || strings.HasPrefix(nameLower, "mcp_") || nameLower == "call_mcp_tool" {
			tool.Category = ToolCategoryMCP
		} else if _, ok := editToolNames[nameLower]; ok || hasInSet(kindLower, editToolNames) {
			tool.Category = ToolCategoryEdit
		} else if _, ok := writeToolNames[nameLower]; ok || hasInSet(kindLower, writeToolNames) {
			tool.Category = ToolCategoryWrite
		} else if _, ok := readToolNames[nameLower]; ok || hasInSet(kindLower, readToolNames) {
			tool.Category = ToolCategoryRead
		} else if _, ok := searchToolNames[nameLower]; ok || hasInSet(kindLower, searchToolNames) {
			tool.Category = ToolCategorySearch
		} else if _, ok := commandToolNames[nameLower]; ok || hasInSet(kindLower, commandToolNames) || kindLower == "commandexecution" {
			tool.Category = ToolCategoryCommand
		} else if _, ok := todoToolNames[nameLower]; ok || hasInSet(kindLower, todoToolNames) {
			tool.Category = ToolCategoryTodo
		} else if _, ok := subagentToolNames[nameLower]; ok || hasInSet(kindLower, subagentToolNames) {
			tool.Category = ToolCategorySubagent
		} else {
			tool.Category = ToolCategoryOther
		}
	}

	lowered := lowerMap(tool.Input)

	// 2. Target (if empty)
	if tool.Target == "" && lowered != nil {
		switch tool.Category {
		case ToolCategoryRead, ToolCategoryEdit, ToolCategoryWrite:
			tool.Target = extractFilePathWithLines(lowered)
		case ToolCategoryCommand:
			tool.Target = findFirstString(lowered, commandKeys)
		case ToolCategorySearch:
			target := findFirstString(lowered, searchQueryKeys)
			if target == "" {
				target = findFirstString(lowered, searchPathKeys)
			}
			tool.Target = target
		default:
			target := extractFilePathWithLines(lowered)
			if target == "" {
				target = findFirstString(lowered, commandKeys)
			}
			if target == "" {
				target = findFirstString(lowered, searchQueryKeys)
			}
			if target == "" {
				target = findFirstString(lowered, searchPathKeys)
			}
			if target == "" {
				target = findFirstString(lowered, actionKeys)
			}
			if target == "" {
				target = findFirstString(lowered, titleKeys)
			}
			if target == "" {
				target = findFirstString(lowered, summaryKeys)
			}
			tool.Target = target
		}
	}

	// 3. Summary (if empty)
	if tool.Summary == "" && lowered != nil {
		sum := findFirstString(lowered, []string{"toolsummary", "tool_summary", "description", "title"})
		sum = strings.TrimSpace(sum)
		if sum != "" && sum != tool.Target {
			tool.Summary = sum
		}
	}

	// 4. Diffs (if len(tool.Diffs) == 0 and category is edit or write, and tool.Input != nil)
	if len(tool.Diffs) == 0 && (tool.Category == ToolCategoryEdit || tool.Category == ToolCategoryWrite) && tool.Input != nil {
		filePath := findFirstString(lowered, filePathKeys)
		if filePath == "" && tool.Target != "" {
			filePath = tool.Target
		}

		var diffs []FileDiff

		if rawEdits, ok := lowered["edits"]; ok && rawEdits != nil {
			if slice := toSlice(rawEdits); slice != nil {
				for _, item := range slice {
					if m := toMapStringAny(item); m != nil {
						low := lowerMap(m)
						oldText, _ := findRawString(low, "old_string", "oldstring", "targetcontent")
						newText, _ := findRawString(low, "new_string", "newstring", "replacementcontent")
						diffs = append(diffs, FileDiff{
							Path:    filePath,
							OldText: oldText,
							NewText: newText,
						})
					}
				}
			}
		} else if rawChunks, ok := lowered["replacementchunks"]; ok && rawChunks != nil {
			if slice := toSlice(rawChunks); slice != nil {
				for _, item := range slice {
					if m := toMapStringAny(item); m != nil {
						low := lowerMap(m)
						oldText, _ := findRawString(low, "targetcontent")
						newText, _ := findRawString(low, "replacementcontent")
						diffs = append(diffs, FileDiff{
							Path:    filePath,
							OldText: oldText,
							NewText: newText,
						})
					}
				}
			}
		} else {
			oldText, hasOld := findRawString(lowered, "old_string", "oldstring", "targetcontent", "old_str")
			newText, hasNew := findRawString(lowered, "new_string", "newstring", "replacementcontent", "new_str")
			content, hasContent := findRawString(lowered, "content", "codecontent", "new_source", "file_text", "text")

			if hasOld || hasNew {
				diffs = append(diffs, FileDiff{
					Path:    filePath,
					OldText: oldText,
					NewText: newText,
				})
			} else if hasContent {
				diffs = append(diffs, FileDiff{
					Path:    filePath,
					OldText: "",
					NewText: content,
				})
			}
		}

		if len(diffs) > 0 {
			tool.Diffs = diffs
		}
	}
}

func hasInSet(val string, set map[string]struct{}) bool {
	if val == "" {
		return false
	}
	_, ok := set[val]
	return ok
}

func lowerMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	lowered := make(map[string]any, len(input))
	for k, v := range input {
		lowered[strings.ToLower(k)] = v
	}
	return lowered
}

func toMapStringAny(v any) map[string]any {
	if v == nil {
		return nil
	}
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[string]string:
		res := make(map[string]any, len(m))
		for k, val := range m {
			res[k] = val
		}
		return res
	default:
		return nil
	}
}

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []any:
		return s
	case []map[string]any:
		res := make([]any, len(s))
		for i, x := range s {
			res[i] = x
		}
		return res
	default:
		return nil
	}
}

func extractFilePathWithLines(m map[string]any) string {
	filePath := findFirstString(m, filePathKeys)
	if filePath == "" {
		return ""
	}
	start := getLineVal(m, "start_line", "startline", "line")
	end := getLineVal(m, "end_line", "endline")
	if start != "" {
		if end != "" && end != start {
			return fmt.Sprintf("%s:%s-%s", filePath, start, end)
		}
		return fmt.Sprintf("%s:%s", filePath, start)
	}
	return filePath
}

func getLineVal(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if val, ok := m[k]; ok && val != nil {
			switch v := val.(type) {
			case int:
				return strconv.Itoa(v)
			case int64:
				return strconv.FormatInt(v, 10)
			case float64:
				return strconv.Itoa(int(v))
			case string:
				s := strings.TrimSpace(v)
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func findFirstString(m map[string]any, keys []string) string {
	for _, k := range keys {
		val, ok := m[k]
		if !ok || val == nil {
			continue
		}
		switch v := val.(type) {
		case string:
			s := strings.TrimSpace(v)
			if s != "" {
				return s
			}
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		default:
		}
	}
	return ""
}

func findRawString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		val, ok := m[k]
		if !ok || val == nil {
			continue
		}
		switch v := val.(type) {
		case string:
			return v, true
		case fmt.Stringer:
			return v.String(), true
		default:
			return fmt.Sprint(v), true
		}
	}
	return "", false
}
