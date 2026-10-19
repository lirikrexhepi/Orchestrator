package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"composer/internal/claudeimport"
	"composer/internal/domain"
	"composer/internal/history"
	"composer/internal/logger"
	"composer/internal/opencode"
	"composer/internal/session"
	"composer/internal/titler"
)

const (
	chatTitleChannel = "chat:title"
	titleBudget      = 2 * time.Minute
)

type chatTitles struct {
	once    sync.Once
	tracker *titler.Tracker
	root    string

	mu       sync.Mutex
	opencode *opencode.TitleServer
	hiddenMu sync.Mutex
}

func (a *App) titleState() *chatTitles {
	t := &a.titles
	t.once.Do(func() {
		t.tracker = titler.NewTracker()
		t.root = filepath.Join(configRoot(), "titler")
		_ = os.MkdirAll(filepath.Join(t.root, "work"), 0o755)
		claudeimport.HideAntigravityConversations(readLines(filepath.Join(t.root, "antigravity-conversations.txt"))...)
	})
	return t
}

func readLines(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var out []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (a *App) rememberTitleConversation(id string) {
	t := a.titleState()
	claudeimport.HideAntigravityConversations(id)
	t.hiddenMu.Lock()
	defer t.hiddenMu.Unlock()
	file, err := os.OpenFile(filepath.Join(t.root, "antigravity-conversations.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(id + "\n")
}

func (a *App) closeTitles() {
	t := &a.titles
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.opencode != nil {
		t.opencode.Close()
		t.opencode = nil
	}
}

func (a *App) noteChatMessage(event domain.RuntimeEvent) {
	if a.recorder == nil || event.ThreadID == "" || event.ThreadID == session.CoordinatorThreadID {
		return
	}
	state, ok := a.recorder.NoteUserTurn(event.ThreadID)
	if !ok {
		return
	}
	t := a.titleState()
	recent := t.tracker.Note(state.ThreadID, event.Text)
	refine, due := titler.Due(titler.State{
		Title:     state.Title,
		Source:    state.Source,
		Prompt:    state.Prompt,
		TitleTurn: state.TitleTurn,
		UserTurns: state.UserTurns,
	})
	if !due || t.tracker.Cooling(state.ThreadID, state.UserTurns) || !t.tracker.Begin(state.ThreadID) {
		return
	}

	request := titler.Request{Current: state.Title, First: state.Prompt, Refine: refine}
	if refine || state.UserTurns > 1 || strings.TrimSpace(event.Text) != strings.TrimSpace(state.Prompt) {
		request.Recent = recent
	}
	if strings.TrimSpace(request.First) == "" {
		request.First = event.Text
	}
	cwd := a.recorder.WorkspaceCwd(event.ThreadID)

	go func() {
		defer t.tracker.End(state.ThreadID)
		ctx, cancel := context.WithTimeout(context.Background(), titleBudget)
		defer cancel()

		title, from, err := titler.Generate(ctx, a.titleBackends(cwd), titler.Prompt(request))
		if err != nil {
			logger.Infof("Titler", "no title for %s, keeping %q: %v", state.ThreadID, state.Title, err)
			t.tracker.Failed(state.ThreadID, state.UserTurns)
			return
		}
		logger.Infof("Titler", "title for %s from %s: %q (was %q)", state.ThreadID, from, title, state.Title)
		_ = a.applyTitle(state.ThreadID, title, titler.SourceAI, state.UserTurns)
	}()
}

func (a *App) titleBackends(cwd string) []titler.Backend {
	t := a.titleState()
	work := filepath.Join(t.root, "work")
	var backends []titler.Backend

	openCodeSettings := a.registry.Settings(domain.DriverOpenCode)
	if strings.TrimSpace(openCodeSettings.ServerURL) != "" || titler.Installed(binaryName(openCodeSettings, "opencode"), openCodeSettings) {
		t.mu.Lock()
		if t.opencode == nil {
			t.opencode = opencode.NewTitleServer(openCodeSettings, filepath.Join(t.root, "opencode"), work)
		}
		server := t.opencode
		t.mu.Unlock()
		backends = append(backends, titler.Backend{Name: "opencode", Timeout: 50 * time.Second, Run: server.Generate})
	}

	if settings, ok := a.titleLaunchSettings(domain.DriverAntigravity, cwd); ok && titler.Installed(binaryName(settings, "agy"), settings) {
		backends = append(backends, titler.Backend{Name: "antigravity", Timeout: 40 * time.Second, Run: titler.Antigravity(settings, work, a.rememberTitleConversation)})
	}

	if settings, ok := a.titleLaunchSettings(domain.DriverClaude, cwd); ok && titler.Installed(binaryName(settings, "claude"), settings) {
		backends = append(backends, titler.Backend{Name: "claude", Timeout: 40 * time.Second, Run: titler.Claude(settings, work)})
	}
	return backends
}

func binaryName(settings domain.ProviderSettings, fallback string) string {
	if strings.TrimSpace(settings.BinaryPath) != "" {
		return settings.BinaryPath
	}
	return fallback
}

func (a *App) titleLaunchSettings(kind domain.DriverKind, cwd string) (domain.ProviderSettings, bool) {
	account := domain.DefaultAccountID
	if a.manager != nil {
		if resolved, err := a.manager.ResolveAccount(kind, cwd, ""); err == nil {
			account = resolved
		}
	}
	settings, err := a.registry.LaunchSettings(kind, account)
	if err != nil {
		return domain.ProviderSettings{}, false
	}
	return settings, true
}

func (a *App) applyTitle(threadID, title, source string, turn int) error {
	if a.recorder == nil {
		return errors.New("history is unavailable")
	}
	state, err := a.recorder.SetTitle(threadID, title, source, turn)
	if err != nil {
		return err
	}
	if a.workspaces != nil {
		a.workspaces.SetTitle(state.ThreadID, title, source)
	}
	a.emit(chatTitleChannel, map[string]string{
		"threadId":    state.ThreadID,
		"workspaceId": state.WorkspaceID,
		"title":       title,
		"source":      source,
	})
	return nil
}

func (a *App) RenameChat(threadID, title string) error {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return nil
	}
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	return a.applyTitle(history.RootThreadID(threadID), title, titler.SourceUser, 0)
}
