package titler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFirstChars  = 1200
	maxRecentChars = 500
	keepRecent     = 6
	maxTitleChars  = 60
	maxTitleWords  = 8
)

const (
	SourceAI   = "ai"
	SourceUser = "user"
)

type Request struct {
	Current string
	First   string
	Recent  []string
	Refine  bool
}

func Clip(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

const rules = "Rules: 2 to 6 words. Sentence case. No quotes, no emoji, no trailing punctuation. " +
	"Same language as the user. Name the actual work (the feature, bug or topic), never greetings or meta requests like \"help me\" or \"a few things\". " +
	"Reply with the title only."

func Prompt(request Request) string {
	var b strings.Builder
	if request.Refine && strings.TrimSpace(request.Current) != "" {
		b.WriteString("You keep the title of a coding chat up to date.\n\n")
		b.WriteString("Current title: " + Clip(request.Current, maxTitleChars) + "\n\n")
		if first := Clip(request.First, maxFirstChars); first != "" {
			b.WriteString("First message:\n" + first + "\n\n")
		}
		if len(request.Recent) > 0 {
			b.WriteString("Latest messages, oldest first:\n")
			for _, message := range request.Recent {
				b.WriteString("- " + Clip(message, maxRecentChars) + "\n")
			}
			b.WriteString("\n")
		}
		b.WriteString("If the current title still describes the chat, return it unchanged. " +
			"If the work has become more specific or moved on, keep the words of the current title that still fit and change or add one to three words so it reflects what was actually discussed; " +
			"only replace it fully when the old title no longer fits at all.\n")
		b.WriteString(rules)
		return b.String()
	}
	b.WriteString("Write a title for this coding chat.\n\n")
	b.WriteString("First message:\n" + Clip(request.First, maxFirstChars) + "\n\n")
	if len(request.Recent) > 0 {
		b.WriteString("Follow-up messages, oldest first:\n")
		for _, message := range request.Recent {
			b.WriteString("- " + Clip(message, maxRecentChars) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(rules)
	return b.String()
}

var (
	labelPrefix = regexp.MustCompile(`(?i)^(new\s+|updated\s+|chat\s+)?title\s*[:\-–—]\s*`)
	wrapChars   = "\"'`*_#“”‘’「」 "
)

func Clean(raw string) (string, bool) {
	var line string
	for _, candidate := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			line = candidate
			break
		}
	}
	line = labelPrefix.ReplaceAllString(line, "")
	line = strings.Trim(line, wrapChars)
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsPunct(r) && r != ')' && r != ']'
	})
	line = strings.Trim(line, wrapChars)
	line = strings.Join(strings.Fields(line), " ")
	if line == "" || utf8.RuneCountInString(line) > maxTitleChars || len(strings.Fields(line)) > maxTitleWords {
		return "", false
	}
	return line, true
}

var placeholderTitles = map[string]bool{"": true, "new chat": true, "session": true, "agent": true, "untitled chat": true, "chat": true}

func LooksAutomatic(title, prompt string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	if placeholderTitles[normalized] {
		return true
	}
	stem := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), "…."))
	if stem == "" {
		return true
	}
	flatPrompt := strings.ToLower(strings.Join(strings.Fields(prompt), " "))
	flatStem := strings.ToLower(strings.Join(strings.Fields(stem), " "))
	return flatPrompt != "" && strings.HasPrefix(flatPrompt, flatStem)
}

func refineGap(titledAt int) int {
	switch {
	case titledAt < 4:
		return 2
	case titledAt < 10:
		return 3
	case titledAt < 30:
		return 6
	default:
		return 12
	}
}

type State struct {
	Title     string
	Source    string
	Prompt    string
	TitleTurn int
	UserTurns int
}

func Due(state State) (refine bool, due bool) {
	switch state.Source {
	case SourceUser:
		return false, false
	case SourceAI:
		return true, state.UserTurns-state.TitleTurn >= refineGap(state.TitleTurn)
	default:
		return false, LooksAutomatic(state.Title, state.Prompt)
	}
}

type Tracker struct {
	mu      sync.Mutex
	recent  map[string][]string
	running map[string]bool
	failed  map[string]int
}

func NewTracker() *Tracker {
	return &Tracker{recent: make(map[string][]string), running: make(map[string]bool), failed: make(map[string]int)}
}

func (t *Tracker) Note(threadID, text string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if clipped := Clip(text, maxRecentChars); clipped != "" {
		list := append(t.recent[threadID], clipped)
		if len(list) > keepRecent {
			list = list[len(list)-keepRecent:]
		}
		t.recent[threadID] = list
	}
	return append([]string(nil), t.recent[threadID]...)
}

func (t *Tracker) Begin(threadID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running[threadID] {
		return false
	}
	t.running[threadID] = true
	return true
}

func (t *Tracker) Failed(threadID string, turn int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed[threadID] = turn
}

func (t *Tracker) Cooling(threadID string, turn int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	failedAt, ok := t.failed[threadID]
	return ok && turn-failedAt < 2
}

func (t *Tracker) End(threadID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.running, threadID)
}

type Backend struct {
	Name    string
	Timeout time.Duration
	Run     func(ctx context.Context, prompt string) (string, error)
}

func Generate(ctx context.Context, backends []Backend, prompt string) (string, string, error) {
	var failures []string
	for _, backend := range backends {
		if ctx.Err() != nil {
			break
		}
		timeout := backend.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		raw, err := backend.Run(attempt, prompt)
		cancel()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", backend.Name, err))
			continue
		}
		if title, ok := Clean(raw); ok {
			return title, backend.Name, nil
		}
		failures = append(failures, fmt.Sprintf("%s: unusable reply %q", backend.Name, Clip(raw, 80)))
	}
	if len(failures) == 0 {
		return "", "", errors.New("no title backend available")
	}
	return "", "", errors.New(strings.Join(failures, "; "))
}
