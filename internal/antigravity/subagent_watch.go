package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
)

var (
	convIDPattern = regexp.MustCompile(`(?i)(?:"conversationId"|conversationId)\s*[:=]\s*"?([A-Za-z0-9-]{8,64})"?`)
	logURIPattern = regexp.MustCompile(`(?i)(?:logAbsoluteUri|transcript)\s*[:=]\s*"?(file://[^\s"'\x60<>]+)"?`)
)

type invokeSubagentRef struct {
	conversationID string
	logAbsoluteURI string
}

type nativeTranscriptLine struct {
	StepIndex int    `json:"step_index"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Content   string `json:"content"`
	Error     string `json:"error"`
	ToolCalls []struct {
		Name string `json:"name"`
		Args any    `json:"args"`
	} `json:"tool_calls"`
}

type nativeInboxMessage struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Timestamp string `json:"timestamp"`
	Content   string `json:"content"`
}

type nativeSubagentRecord struct {
	ConversationID string `json:"conversationId"`
	State          string `json:"state"`
}

func parseInvokeSubagentOutput(output string) (invokeSubagentRef, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return invokeSubagentRef{}, false
	}

	var ref invokeSubagentRef
	var parsed struct {
		ConversationID string `json:"conversationId"`
		LogAbsoluteURI string `json:"logAbsoluteUri"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && parsed.ConversationID != "" {
		ref.conversationID = strings.TrimSpace(parsed.ConversationID)
		ref.logAbsoluteURI = strings.TrimSpace(parsed.LogAbsoluteURI)
		return ref, true
	}

	if start := strings.IndexByte(trimmed, '{'); start >= 0 {
		if end := strings.LastIndexByte(trimmed, '}'); end > start {
			if err := json.Unmarshal([]byte(trimmed[start:end+1]), &parsed); err == nil && parsed.ConversationID != "" {
				ref.conversationID = strings.TrimSpace(parsed.ConversationID)
				ref.logAbsoluteURI = strings.TrimSpace(parsed.LogAbsoluteURI)
				return ref, true
			}
		}
	}

	if m := convIDPattern.FindStringSubmatch(trimmed); len(m) == 2 {
		ref.conversationID = strings.TrimSpace(m[1])
		if lm := logURIPattern.FindStringSubmatch(trimmed); len(lm) == 2 {
			ref.logAbsoluteURI = strings.TrimSpace(lm[1])
		}
		return ref, true
	}
	return invokeSubagentRef{}, false
}

func fileURIToPath(rawURI string) string {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return ""
	}
	u, err := url.Parse(rawURI)
	if err != nil {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return filepath.Clean(rawURI)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return filepath.FromSlash(`\\` + u.Host + p)
		}
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
	}
	return filepath.FromSlash(p)
}

func resolveBrainRoot(childConvID, logAbsoluteURI string) string {
	if local := fileURIToPath(logAbsoluteURI); local != "" {
		dir := local
		for i := 0; i < 4; i++ {
			dir = filepath.Dir(dir)
			if strings.EqualFold(filepath.Base(dir), childConvID) {
				return filepath.Dir(dir)
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	return ""
}

func (a *Adapter) maybeStartNativeSubagentWatch(s *session, turnID string, tool *domain.ToolCall) bool {
	if s == nil || tool == nil || !strings.EqualFold(tool.Name, "invoke_subagent") || tool.Status != domain.ToolCompleted {
		return false
	}
	ref, ok := parseInvokeSubagentOutput(tool.Output)
	if !ok || ref.conversationID == "" {
		return false
	}

	s.mu.Lock()
	if s.nativeSubagents == nil {
		s.nativeSubagents = make(map[string]bool)
	}
	if s.nativeSubagents[tool.ID] {
		s.mu.Unlock()
		return true
	}
	s.nativeSubagents[tool.ID] = true
	s.mu.Unlock()

	ctx := s.procCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.watchNativeSubagent(ctx, s, turnID, tool.ID, ref.conversationID, ref.logAbsoluteURI, time.Now())
	return true
}

func (a *Adapter) watchNativeSubagent(
	ctx context.Context,
	s *session,
	turnID string,
	toolID string,
	childConvID string,
	logAbsoluteURI string,
	startedAt time.Time,
) {
	brainRoot := resolveBrainRoot(childConvID, logAbsoluteURI)
	if brainRoot == "" {
		return
	}

	transcriptPath := fileURIToPath(logAbsoluteURI)
	if transcriptPath == "" {
		transcriptPath = filepath.Join(brainRoot, childConvID, ".system_generated", "logs", "transcript.jsonl")
	}
	s.mu.Lock()
	parentSessionID := s.sessionID
	s.mu.Unlock()

	var offset int64
	seenTools := 0
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		offset, seenTools = a.pollTranscriptSteps(s, turnID, toolID, transcriptPath, offset, seenTools)

		if parentSessionID != "" {
			if msg, found := findChildInboxMessage(brainRoot, parentSessionID, childConvID, startedAt); found {
				ev := a.event(s, turnID, domain.EventToolResult)
				ev.ItemID = toolID
				ev.Tool = &domain.ToolCall{
					ID:         toolID,
					Name:       "invoke_subagent",
					Category:   domain.ToolCategorySubagent,
					Status:     domain.ToolCompleted,
					Async:      false,
					DurationMs: time.Since(startedAt).Milliseconds(),
					Output:     strings.TrimSpace(msg.Content),
				}
				a.emit.Emit(ev)
				return
			}

			if failedState, isFailed := checkChildSubagentFailed(brainRoot, parentSessionID, childConvID); isFailed {
				ev := a.event(s, turnID, domain.EventToolResult)
				ev.ItemID = toolID
				ev.Tool = &domain.ToolCall{
					ID:         toolID,
					Name:       "invoke_subagent",
					Category:   domain.ToolCategorySubagent,
					Status:     domain.ToolFailed,
					Async:      false,
					DurationMs: time.Since(startedAt).Milliseconds(),
					Output:     "Subagent terminated (" + failedState + ")",
				}
				a.emit.Emit(ev)
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Adapter) pollTranscriptSteps(
	s *session,
	turnID string,
	parentToolID string,
	transcriptPath string,
	offset int64,
	seenTools int,
) (int64, int) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return offset, seenTools
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() <= offset {
		return offset, seenTools
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, seenTools
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// Advance only on complete newline-delimited JSON lines.
			if line[len(line)-1] == '\n' {
				offset += int64(len(line))
				var step nativeTranscriptLine
				if jsonErr := json.Unmarshal(line, &step); jsonErr == nil {
					for _, tc := range step.ToolCalls {
						if strings.TrimSpace(tc.Name) == "" {
							continue
						}
						seenTools++
						subToolID := parentToolID + ":step:" + itoaInt(seenTools)
						inputMap, _ := tc.Args.(map[string]any)
						callEv := a.event(s, turnID, domain.EventSubagentToolCall)
						callEv.ParentToolID = parentToolID
						callEv.Tool = &domain.ToolCall{
							ID:     subToolID,
							Name:   tc.Name,
							Status: domain.ToolInProgress,
							Input:  inputMap,
						}
						a.emit.Emit(callEv)

						doneEv := a.event(s, turnID, domain.EventSubagentToolDone)
						doneEv.ParentToolID = parentToolID
						doneEv.Tool = &domain.ToolCall{
							ID:     subToolID,
							Name:   tc.Name,
							Status: domain.ToolCompleted,
							Input:  inputMap,
						}
						a.emit.Emit(doneEv)
					}
					if step.Type == "PLANNER_RESPONSE" && strings.TrimSpace(step.Content) != "" {
						msgEv := a.event(s, turnID, domain.EventSubagentMessage)
						msgEv.ParentToolID = parentToolID
						msgEv.ItemID = parentToolID + ":step:" + itoaInt(step.StepIndex)
						msgEv.Text = step.Content
						a.emit.Emit(msgEv)
					}
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Debugf("Antigravity", "tail subagent transcript %s: %v", transcriptPath, err)
			}
			break
		}
	}
	return offset, seenTools
}

func findChildInboxMessage(brainRoot, parentSessionID, childConvID string, launchedAt time.Time) (nativeInboxMessage, bool) {
	msgRoot := filepath.Join(brainRoot, parentSessionID, ".system_generated", "messages")
	dirs := []string{msgRoot, filepath.Join(msgRoot, "undelivered")}

	type candidate struct {
		msg nativeInboxMessage
		ts  int64
	}
	var matches []candidate

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") || strings.EqualFold(entry.Name(), "read.json") {
				continue
			}
			fullPath := filepath.Join(dir, entry.Name())
			raw, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			var msg nativeInboxMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(msg.Sender), childConvID) {
				continue
			}
			var ts int64
			if t, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
				if !launchedAt.IsZero() && t.Add(2*time.Second).Before(launchedAt) {
					continue
				}
				ts = t.UnixNano()
			}
			matches = append(matches, candidate{msg: msg, ts: ts})
		}
	}
	if len(matches) == 0 {
		return nativeInboxMessage{}, false
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ts > matches[j].ts })
	return matches[0].msg, true
}

func checkChildSubagentFailed(brainRoot, parentSessionID, childConvID string) (string, bool) {
	recordPath := filepath.Join(brainRoot, parentSessionID, ".system_generated", "subagents", childConvID+".json")
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		return "", false
	}
	var rec nativeSubagentRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return "", false
	}
	upper := strings.ToUpper(strings.TrimSpace(rec.State))
	for _, token := range []string{"KILLED", "FAILED", "ERROR", "STOPPED", "TERMINATED", "CANCELLED"} {
		if strings.Contains(upper, token) {
			return rec.State, true
		}
	}
	return "", false
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
