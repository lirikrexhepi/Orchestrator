// Package claudeimport reads Claude Code session transcripts from disk so the
// user can bring outside chat history into Composer.
//
// Claude Code stores one JSONL file per conversation under
// ~/.claude/projects/<encoded-cwd>/<sessionId>.jsonl (CLAUDE_CONFIG_DIR
// overrides the base). Each line carries a top-level type: user and assistant
// hold the conversation, everything else is queue bookkeeping, file snapshots
// or UI hints. Records with isSidechain set belong to a subagent whose work
// the parent already summarises, so they are skipped.
//
// Conversion targets domain.RuntimeEvent, the same shape live adapters emit,
// which means an imported transcript replays through the existing frontend
// reducer with no special casing.
package claudeimport

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
)

const (
	maxTextChars       = 30000
	maxToolOutputChars = 8000
	maxListFiles       = 2000
	maxParseBytes      = 256 * 1024 * 1024
	maxLineBytes       = 64 * 1024 * 1024
)

type cachedSession struct {
	size    int64
	modTime time.Time
	session *ExternalSession
}

var (
	listCacheMu sync.Mutex
	listCache   = map[string]cachedSession{}
)

const continuationPrefix = "This session is being continued from a previous conversation"

// ExternalSession is one Claude Code conversation found on disk, described
// with only what the import picker needs to show.
type ExternalSession struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Cwd          string `json:"cwd"`
	Model        string `json:"model,omitempty"`
	Preview      string `json:"preview,omitempty"`
	FilePath     string `json:"filePath"`
	MessageCount int    `json:"messageCount"`
	StartedAt    int64  `json:"startedAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	// AgentRun marks transcripts produced by an agent rather than typed by
	// the user: Composer/catalyst worktree runs, orchestrator-spawned CLIs
	// and test sessions. UIs show hand-written chats only, so these stay out
	// of the list even when they contain text.
	AgentRun   bool   `json:"agentRun"`
	Entrypoint string `json:"entrypoint,omitempty"`
	Provider   string `json:"provider,omitempty"`
}

// ParsedSession is a fully converted transcript, ready to be stored. Events
// carry no thread id yet; the importer assigns that when it creates the task.
type ParsedSession struct {
	SessionID  string                `json:"sessionId"`
	Title      string                `json:"title"`
	Prompt     string                `json:"prompt"`
	Cwd        string                `json:"cwd"`
	Model      string                `json:"model,omitempty"`
	Entrypoint string                `json:"entrypoint,omitempty"`
	StartedAt  int64                 `json:"startedAt"`
	UpdatedAt  int64                 `json:"updatedAt"`
	Events     []domain.RuntimeEvent `json:"events"`
}

// ProjectsDir reports where Claude Code keeps its transcripts, honouring the
// documented CLAUDE_CONFIG_DIR override.
func ProjectsDir() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// List scans the projects tree and describes every conversation a user typed
// into, newest first. Transcripts with no human-written message — tool-result
// only runs and assistant-only side tasks the AI ran on its own — are skipped,
// as are files that cannot be parsed, rather than failing the whole listing.
func List() ([]ExternalSession, error) {
	root := ProjectsDir()
	if root == "" {
		return nil, nil
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ExternalSession, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if len(out) >= maxListFiles {
			break
		}
		seen[path] = true
		session := describe(path)
		if session == nil {
			continue
		}
		out = append(out, *session)
	}
	listCacheMu.Lock()
	for path := range listCache {
		if !seen[path] {
			delete(listCache, path)
		}
	}
	listCacheMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

func describe(path string) *ExternalSession {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxParseBytes {
		return nil
	}
	listCacheMu.Lock()
	cached, ok := listCache[path]
	listCacheMu.Unlock()
	if ok && cached.size == info.Size() && cached.modTime.Equal(info.ModTime()) {
		return cached.session
	}
	session := describeLite(path, info)
	if session == nil {
		session = describeFull(path)
	}
	listCacheMu.Lock()
	listCache[path] = cachedSession{size: info.Size(), modTime: info.ModTime(), session: session}
	listCacheMu.Unlock()
	return session
}

func FindSessionFile(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	root := ProjectsDir()
	if sessionID == "" || root == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// ParseFile converts one Claude Code JSONL transcript into runtime events.
func ParseFile(path string) (*ParsedSession, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, _ := file.Stat()
	fallbackAt := time.Now().UnixMilli()
	if info != nil {
		fallbackAt = info.ModTime().UnixMilli()
	}

	parsed := &ParsedSession{}
	converter := &converter{fallbackAt: fallbackAt}
	reader := bufio.NewReaderSize(file, 256*1024)
	for {
		line, err := readLine(reader)
		if len(line) > 0 {
			var record fileRecord
			if json.Unmarshal(line, &record) == nil {
				converter.visit(record, parsed)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	if parsed.SessionID == "" {
		parsed.SessionID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	converter.attachSubagents(path, parsed)
	parsed.Title = firstNonEmpty(converter.customTitle, converter.aiTitle, converter.summary, previewText(converter.titlePrompt, 40))
	if parsed.Title == "" {
		parsed.Title = "Imported Claude Code chat"
	}
	if parsed.StartedAt == 0 {
		parsed.StartedAt = fallbackAt
	}
	if parsed.UpdatedAt == 0 || parsed.UpdatedAt < parsed.StartedAt {
		parsed.UpdatedAt = firstNonZero(firstEventAt(parsed.Events), fallbackAt)
	}
	for i := range parsed.Events {
		parsed.Events[i].Seq = uint64(i + 1)
	}
	return parsed, nil
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	var buf []byte
	oversized := false
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if !oversized {
			if len(buf)+len(chunk) > maxLineBytes {
				oversized = true
				buf = nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err != nil {
			if oversized {
				return nil, err
			}
			return buf, err
		}
		if !isPrefix {
			if oversized {
				return nil, nil
			}
			return buf, nil
		}
	}
}

type fileRecord struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Timestamp   string          `json:"timestamp"`
	UUID        string          `json:"uuid"`
	Cwd         string          `json:"cwd"`
	IsSidechain bool            `json:"isSidechain"`
	AITitle     string          `json:"aiTitle"`
	CustomTitle string          `json:"customTitle"`
	Entrypoint  string          `json:"entrypoint"`
	Summary     string          `json:"summary"`
	Message     json.RawMessage `json:"message"`

	Operation     string          `json:"operation"`
	QueueContent  json.RawMessage `json:"content"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type fileMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
	Usage   *fileUsage      `json:"usage,omitempty"`
}

type fileUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type converter struct {
	customTitle string
	aiTitle     string
	summary     string
	titlePrompt string
	fallbackAt  int64
	lastAt      int64
	seq         int
	turn        int
	turnID      string
	launched    map[string]int64
	finished    map[string]bool
}

func (c *converter) visit(record fileRecord, parsed *ParsedSession) {
	if record.IsSidechain {
		return
	}
	if record.SessionID != "" && parsed.SessionID == "" {
		parsed.SessionID = record.SessionID
	}
	if record.Cwd != "" && parsed.Cwd == "" {
		parsed.Cwd = record.Cwd
	}
	if record.Entrypoint != "" && parsed.Entrypoint == "" {
		parsed.Entrypoint = record.Entrypoint
	}
	if title := strings.TrimSpace(record.CustomTitle); title != "" {
		c.customTitle = title
	}
	if title := strings.TrimSpace(record.AITitle); title != "" {
		c.aiTitle = title
	}
	if record.Type == "summary" && c.summary == "" {
		c.summary = strings.TrimSpace(record.Summary)
	}
	at := parseAt(record.Timestamp, c.fallbackAt)
	if at < c.lastAt {
		at = c.lastAt
	}
	c.lastAt = at

	switch record.Type {
	case "user":
		c.visitUser(record, at, parsed)
	case "assistant":
		c.visitAssistant(record, at, parsed)
	case "queue-operation":
		c.visitQueue(record, at, parsed)
	}
}

func (c *converter) visitUser(record fileRecord, at int64, parsed *ParsedSession) {
	if len(record.Message) == 0 {
		return
	}
	var msg fileMessage
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return
	}
	if msg.Role != "" && msg.Role != "user" {
		return
	}
	text, results := splitUserContent(msg.Content)
	text = strings.TrimSpace(text)
	if text != "" {
		c.turn++
		c.turnID = "import-turn-" + itoa(c.turn)
		if parsed.Prompt == "" {
			parsed.Prompt = truncate(text, 2000)
		}
		if c.titlePrompt == "" && !strings.HasPrefix(text, continuationPrefix) && !strings.HasPrefix(text, "<") {
			c.titlePrompt = text
		}
		if parsed.StartedAt == 0 {
			parsed.StartedAt = at
		}
		parsed.UpdatedAt = at
		c.emit(parsed, domain.RuntimeEvent{
			Kind:   domain.EventUserMessage,
			TurnID: c.turnID,
			ItemID: firstNonEmpty(record.UUID, "import-user-"+itoa(c.turn)),
			At:     at,
			Text:   truncate(text, maxTextChars),
		})
	}
	for _, result := range results {
		parsed.UpdatedAt = at
		tool := &domain.ToolCall{
			ID:     result.id,
			Name:   result.name,
			Status: result.status,
			Output: truncate(result.output, maxToolOutputChars),
		}
		c.applyToolUseResult(tool, record.ToolUseResult, at)
		c.emit(parsed, domain.RuntimeEvent{
			Kind:   domain.EventToolResult,
			TurnID: c.turnID,
			ItemID: result.id,
			At:     at,
			Tool:   tool,
		})
	}
}

func (c *converter) visitAssistant(record fileRecord, at int64, parsed *ParsedSession) {
	if len(record.Message) == 0 {
		return
	}
	var msg fileMessage
	if err := json.Unmarshal(record.Message, &msg); err != nil {
		return
	}
	if msg.Model != "" {
		parsed.Model = msg.Model
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		var text string
		if json.Unmarshal(msg.Content, &text) == nil && strings.TrimSpace(text) != "" {
			blocks = []contentBlock{{Type: "text", Text: text}}
		} else {
			return
		}
	}
	turnID := c.turnID
	if turnID == "" {
		c.turn++
		turnID = "import-turn-" + itoa(c.turn)
		c.turnID = turnID
	}
	if parsed.StartedAt == 0 {
		parsed.StartedAt = at
	}
	parsed.UpdatedAt = at
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) == "" {
				continue
			}
			c.emit(parsed, domain.RuntimeEvent{
				Kind:   domain.EventAgentMessage,
				TurnID: turnID,
				ItemID: firstNonEmpty(record.UUID+"-"+block.Type, "import-text-"+itoa(c.seq+1)),
				At:     at,
				Text:   truncate(block.Text, maxTextChars),
			})
		case "thinking":
			if strings.TrimSpace(block.Thinking) == "" {
				continue
			}
			c.emit(parsed, domain.RuntimeEvent{
				Kind:   domain.EventAgentThought,
				TurnID: turnID,
				ItemID: firstNonEmpty(record.UUID+"-thinking", "import-thinking-"+itoa(c.seq+1)),
				At:     at,
				Text:   truncate(block.Thinking, maxTextChars),
			})
		case "tool_use":
			if block.ID == "" && block.Name == "" {
				continue
			}
			tool := &domain.ToolCall{ID: block.ID, Name: block.Name, Status: domain.ToolInProgress}
			if len(block.Input) > 0 {
				var input map[string]any
				if json.Unmarshal(block.Input, &input) == nil {
					tool.Input = input
				}
			}
			c.emit(parsed, domain.RuntimeEvent{
				Kind:   domain.EventToolCall,
				TurnID: turnID,
				ItemID: firstNonEmpty(block.ID, "import-tool-"+itoa(c.seq+1)),
				At:     at,
				Tool:   tool,
			})
		}
	}
	if msg.Usage != nil && (msg.Usage.InputTokens > 0 || msg.Usage.OutputTokens > 0) {
		c.emit(parsed, domain.RuntimeEvent{
			Kind:   domain.EventUsage,
			TurnID: turnID,
			At:     at,
			Usage: &domain.Usage{
				InputTokens:      msg.Usage.InputTokens,
				OutputTokens:     msg.Usage.OutputTokens,
				CacheReadTokens:  msg.Usage.CacheReadInputTokens,
				CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
			},
		})
	}
}

type taskResultInfo struct {
	Status            string `json:"status"`
	TotalDurationMs   int64  `json:"totalDurationMs"`
	TotalToolUseCount int    `json:"totalToolUseCount"`
	TotalTokens       int64  `json:"totalTokens"`
}

func (c *converter) applyToolUseResult(tool *domain.ToolCall, raw json.RawMessage, at int64) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var info taskResultInfo
	if json.Unmarshal(raw, &info) != nil {
		return
	}
	if info.Status == "async_launched" {
		tool.Async = true
		tool.Output = ""
		if c.launched == nil {
			c.launched = map[string]int64{}
		}
		c.launched[tool.ID] = at
		return
	}
	tool.DurationMs = info.TotalDurationMs
	tool.ToolCount = info.TotalToolUseCount
	tool.Tokens = info.TotalTokens
}

func tagValue(text, tag string) string {
	open := "<" + tag + ">"
	start := strings.Index(text, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(text[start:], "</"+tag+">")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[start : start+end])
}

func (c *converter) visitQueue(record fileRecord, at int64, parsed *ParsedSession) {
	if record.Operation != "enqueue" || len(record.QueueContent) == 0 {
		return
	}
	var content string
	if json.Unmarshal(record.QueueContent, &content) != nil || !strings.Contains(content, "<task-notification>") {
		return
	}
	toolID := tagValue(content, "tool-use-id")
	if toolID == "" {
		return
	}
	status := domain.ToolCompleted
	switch tagValue(content, "status") {
	case "failed", "killed", "error", "stopped":
		status = domain.ToolFailed
	}
	tool := &domain.ToolCall{ID: toolID, Status: status}
	tool.ToolCount, _ = strconv.Atoi(tagValue(content, "tool_uses"))
	tool.DurationMs, _ = strconv.ParseInt(tagValue(content, "duration_ms"), 10, 64)
	tool.Tokens, _ = strconv.ParseInt(tagValue(content, "total_tokens"), 10, 64)
	if c.finished == nil {
		c.finished = map[string]bool{}
	}
	c.finished[toolID] = true
	c.emit(parsed, domain.RuntimeEvent{
		Kind:   domain.EventToolResult,
		TurnID: c.turnID,
		ItemID: toolID,
		At:     at,
		Tool:   tool,
	})
}

func (c *converter) emit(parsed *ParsedSession, event domain.RuntimeEvent) {
	event.Driver = domain.DriverClaude
	c.seq++
	parsed.Events = append(parsed.Events, event)
}

type toolResult struct {
	id     string
	name   string
	status domain.ToolStatus
	output string
}

func splitUserContent(raw json.RawMessage) (string, []toolResult) {
	if len(raw) == 0 {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil
	}
	var builder strings.Builder
	var results []toolResult
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				if builder.Len() > 0 {
					builder.WriteString("\n")
				}
				builder.WriteString(block.Text)
			}
		case "tool_result":
			status := domain.ToolCompleted
			if block.IsError {
				status = domain.ToolFailed
			}
			results = append(results, toolResult{
				id:     block.ToolUseID,
				status: status,
				output: flattenToolContent(block.Content),
			})
		}
	}
	return builder.String(), results
}

func flattenToolContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var builder strings.Builder
		for _, block := range blocks {
			if block.Text != "" {
				if builder.Len() > 0 {
					builder.WriteString("\n")
				}
				builder.WriteString(block.Text)
			}
		}
		if builder.Len() > 0 {
			return builder.String()
		}
	}
	return string(raw)
}

func parseAt(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UnixMilli()
	}
	var millis int64
	if err := json.Unmarshal([]byte(raw), &millis); err == nil && millis > 0 {
		if millis < 1e12 {
			millis *= 1000
		}
		return millis
	}
	return fallback
}

// isAgentRun reports transcripts produced by an agent rather than typed by
// the user. Composer spawns a `claude` CLI per task, and every one of those
// leaves a transcript behind: orchestrator planning prompts, worktree scratch
// runs (alpha.txt/beta.txt style), and `go test` sessions under the temp dir.
// Those dwarf hand-written chats in the picker, so they are flagged rather
// than listed alongside them.
func isAgentRun(parsed *ParsedSession) bool {
	if strings.HasPrefix(parsed.Entrypoint, "sdk") {
		return true
	}
	if strings.HasPrefix(parsed.Prompt, "You are the orchestrator in Composer") ||
		strings.HasPrefix(parsed.Prompt, "Reply with exactly:") {
		return true
	}
	lowered := strings.ToLower(parsed.Cwd)
	if strings.Contains(lowered, "worktrees") &&
		(strings.Contains(lowered, "composer") || strings.Contains(lowered, "catalyst")) {
		return true
	}
	if temp := strings.ToLower(os.TempDir()); temp != "" && temp != `\` && temp != "/" {
		if lowered == temp || strings.HasPrefix(lowered, temp+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// hasHumanText reports whether the transcript contains at least one message
// the user typed. Tool results riding on user records and assistant-only
// output do not count: the list is meant to show chats the user opened and
// wrote in, not side tasks the AI ran on its own.
func hasHumanText(events []domain.RuntimeEvent) bool {
	for _, event := range events {
		if event.Kind == domain.EventUserMessage && strings.TrimSpace(event.Text) != "" {
			return true
		}
	}
	return false
}

func countMessages(events []domain.RuntimeEvent) int {
	count := 0
	for _, event := range events {
		if event.Kind == domain.EventUserMessage || event.Kind == domain.EventAgentMessage {
			count++
		}
	}
	return count
}

func previewText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if idx := strings.IndexAny(text, "\r\n"); idx != -1 {
		text = strings.TrimSpace(text[:idx])
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n[truncated]"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstEventAt(events []domain.RuntimeEvent) int64 {
	for _, event := range events {
		if event.At > 0 {
			return event.At
		}
	}
	return 0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func describeFull(path string) *ExternalSession {
	parsed, err := ParseFile(path)
	if err != nil || parsed == nil || !hasHumanText(parsed.Events) {
		return nil
	}
	return &ExternalSession{
		ID:           parsed.SessionID,
		Title:        parsed.Title,
		Cwd:          parsed.Cwd,
		Model:        parsed.Model,
		Preview:      previewText(parsed.Prompt, 140),
		FilePath:     path,
		MessageCount: countMessages(parsed.Events),
		StartedAt:    parsed.StartedAt,
		UpdatedAt:    parsed.UpdatedAt,
		AgentRun:     isAgentRun(parsed),
		Entrypoint:   parsed.Entrypoint,
		Provider:     ProviderClaude,
	}
}
