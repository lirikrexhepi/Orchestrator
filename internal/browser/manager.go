package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"composer/internal/logger"
	"composer/internal/process"
)

var (
	// ErrTabNotFound is returned when a requested tab session does not exist.
	ErrTabNotFound = errors.New("browser tab not found")
	// ErrBrowserClosed is returned when the browser manager is stopped.
	ErrBrowserClosed = errors.New("browser manager is closed")
)

var devToolsWsRegex = regexp.MustCompile(`DevTools listening on (ws://[^\s\r\n]+)`)

// SessionInfo holds the public state of a single CDP browser tab.
type SessionInfo struct {
	ID           string `json:"id"`
	ThreadID     string `json:"threadId"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	Loading      bool   `json:"loading"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

// MouseEvent represents pointer interactions forwarded to CDP.
type MouseEvent struct {
	Type       string  `json:"type"` // "mouseMoved", "mousePressed", "mouseReleased", "mouseWheel"
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button"`  // "none", "left", "middle", "right"
	Buttons    int     `json:"buttons"` // bitmask: 1=left, 2=right, 4=middle
	ClickCount int     `json:"clickCount"`
	DeltaX     float64 `json:"deltaX"`
	DeltaY     float64 `json:"deltaY"`
	Modifiers  int     `json:"modifiers"` // Alt=1, Ctrl=2, Meta=4, Shift=8
}

// KeyEvent represents keyboard interactions forwarded to CDP.
type KeyEvent struct {
	Type                  string `json:"type"` // "keyDown", "keyUp", "rawKeyDown", "char"
	Key                   string `json:"key"`
	Code                  string `json:"code"`
	Text                  string `json:"text"`
	UnmodifiedText        string `json:"unmodifiedText"`
	WindowsVirtualKeyCode int    `json:"windowsVirtualKeyCode"`
	NativeVirtualKeyCode  int    `json:"nativeVirtualKeyCode"`
	Modifiers             int    `json:"modifiers"`
}

type tabEvent struct {
	isBinary bool
	binary   []byte
	state    SessionInfo
	err      string
}

type tab struct {
	id           string
	threadID     string
	targetID     string
	sessionID    string
	url          string
	title        string
	loading      bool
	canGoBack    bool
	canGoForward bool
	width        int
	height       int
	dpr          float64
	paused       bool
	lastFrame    []byte
	subscribers  map[chan tabEvent]struct{}
	mu           sync.RWMutex
}

func (t *tab) info() SessionInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return SessionInfo{
		ID:           t.id,
		ThreadID:     t.threadID,
		URL:          t.url,
		Title:        t.title,
		Loading:      t.loading,
		CanGoBack:    t.canGoBack,
		CanGoForward: t.canGoForward,
		Width:        t.width,
		Height:       t.height,
	}
}

// Manager lazily manages a single headless Chromium instance and multiplexes tabs via CDP.
type Manager struct {
	profileDir     string
	isTempProfile  bool
	anchorTargetID string

	mu         sync.Mutex
	proc       *process.Process
	cdp        *CDPClient
	tabs       map[string]*tab
	nextTabNum int
	idleTimer  *time.Timer
	isClosed   bool
}

// NewManager creates a browser manager configured with a profile directory.
// It does NOT launch Chromium until the first tab is opened.
func NewManager(profileDir string) *Manager {
	return &Manager{
		profileDir: profileDir,
		tabs:       make(map[string]*tab),
	}
}

func (m *Manager) ensureChromiumLocked(ctx context.Context) error {
	if m.isClosed {
		return ErrBrowserClosed
	}
	if m.cdp != nil && m.proc != nil {
		if !m.cdp.Alive() {
			m.stopChromiumLocked()
		} else {
			select {
			case <-m.proc.Done():
				// Process died unexpectedly, clean up state
				m.stopChromiumLocked()
			case <-m.cdp.Done():
				m.stopChromiumLocked()
			default:
				return nil
			}
		}
	}

	execPath, err := FindChromium()
	if err != nil {
		return err
	}

	profile := m.profileDir
	if profile == "" {
		tempDir, err := os.MkdirTemp("", "orchestrator-chrome-profile-*")
		if err != nil {
			return fmt.Errorf("failed to create temp browser profile: %w", err)
		}
		profile = tempDir
		m.isTempProfile = true
		m.profileDir = profile
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return fmt.Errorf("failed to create browser profile directory: %w", err)
	}

	// Clean stale SingletonLock / SingletonSocket / SingletonCookie before launch when no process is tracked.
	if m.proc == nil && !m.isTempProfile {
		if cleanStaleProfileLocks(profile) {
			logger.Infof("BrowserManager", "Removed stale Chromium lock files in %s", profile)
		}
	}

	wsURL, lockFailure, err := m.launchChromiumLocked(ctx, execPath, profile)
	if err != nil && lockFailure && !m.isTempProfile {
		logger.Warnf("BrowserManager", "Chromium exited with profile lock error (%v), cleaning locks and retrying...", err)
		_ = cleanStaleProfileLocks(profile)
		wsURL, lockFailure, err = m.launchChromiumLocked(ctx, execPath, profile)
		if err != nil && lockFailure {
			// Persistent profile is genuinely locked by another active Chromium instance; fall back cleanly to a temp profile.
			tempDir, tempErr := os.MkdirTemp("", "orchestrator-chrome-profile-*")
			if tempErr == nil {
				logger.Warnf("BrowserManager", "Persistent profile %s remains locked, falling back to temp profile %s", profile, tempDir)
				m.isTempProfile = true
				m.profileDir = tempDir
				wsURL, _, err = m.launchChromiumLocked(ctx, execPath, tempDir)
			}
		}
	}
	if err != nil {
		return err
	}

	cdpClient, err := DialCDP(ctx, wsURL)
	if err != nil {
		m.stopChromiumLocked()
		return fmt.Errorf("failed to connect to chromium CDP: %w", err)
	}
	m.cdp = cdpClient

	// Query and establish an internal anchor page target so headless Chrome never exits when user tabs close
	var targetsResult struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	targetsCtx, targetsCancel := context.WithTimeout(ctx, 3*time.Second)
	_ = m.cdp.Call(targetsCtx, "", "Target.getTargets", nil, &targetsResult)
	targetsCancel()

	for _, info := range targetsResult.TargetInfos {
		if info.Type == "page" {
			m.anchorTargetID = info.TargetID
			break
		}
	}
	if m.anchorTargetID == "" {
		var createAnchor struct {
			TargetID string `json:"targetId"`
		}
		createCtx, createCancel := context.WithTimeout(ctx, 3*time.Second)
		_ = m.cdp.Call(createCtx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &createAnchor)
		createCancel()
		m.anchorTargetID = createAnchor.TargetID
	}

	logger.Infof("BrowserManager", "Headless Chromium started (pid=%d, cdp=%s, anchor=%s)", m.proc.PID(), wsURL, m.anchorTargetID)

	// Watch for unexpected Chromium/CDP exit while tabs are open so viewers never hang black/spinning.
	watchedProc := m.proc
	watchedCDP := m.cdp
	go func() {
		select {
		case <-watchedProc.Done():
		case <-watchedCDP.Done():
		}
		m.mu.Lock()
		if m.cdp != watchedCDP && m.proc != watchedProc {
			m.mu.Unlock()
			return
		}
		stderrTail := ""
		if watchedProc != nil {
			stderrTail = strings.TrimSpace(watchedProc.StderrTail())
		}
		openTabs := make([]*tab, 0, len(m.tabs))
		for _, t := range m.tabs {
			openTabs = append(openTabs, t)
		}
		m.mu.Unlock()

		if len(openTabs) == 0 {
			return
		}
		logger.Warnf("BrowserManager", "Headless Chromium exited unexpectedly with %d open tab(s) (stderr: %s)", len(openTabs), stderrTail)
		errMsg := "Headless Chromium exited unexpectedly"
		if stderrTail != "" {
			errMsg += ": " + stderrTail
		}
		for _, t := range openTabs {
			t.mu.Lock()
			t.loading = false
			subs := make([]chan tabEvent, 0, len(t.subscribers))
			for ch := range t.subscribers {
				subs = append(subs, ch)
			}
			t.mu.Unlock()
			m.broadcastState(t)
			for _, ch := range subs {
				select {
				case ch <- tabEvent{err: errMsg}:
				default:
				}
			}
		}
	}()

	return nil
}

func (m *Manager) launchChromiumLocked(ctx context.Context, execPath, profile string) (string, bool, error) {
	wsURLCh := make(chan string, 1)
	var once sync.Once

	flags := []string{
		"--headless=new",
		"--remote-debugging-port=0",
		"--remote-allow-origins=*",
		"--user-data-dir=" + profile,
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows",
		"--disable-dev-shm-usage",
		"--disable-gpu-sandbox",
		"--window-size=1280,800",
	}

	spec := process.Spec{
		Command:       execPath,
		Args:          flags,
		KillTree:      true,
		DieWithParent: true,
		Stderr: func(line string) {
			if matches := devToolsWsRegex.FindStringSubmatch(line); len(matches) > 1 {
				once.Do(func() {
					wsURLCh <- matches[1]
				})
			}
		},
	}

	startAt := time.Now()
	// Chromium's lifetime is managed by Manager (idle timer / CloseAll / DieWithParent),
	// never by the short-lived caller ctx of a single Open() RPC (whose defer cancel()
	// would otherwise kill Chromium as soon as Open returns).
	proc, err := process.Start(context.Background(), spec)
	if err != nil {
		return "", false, fmt.Errorf("failed to start headless chromium: %w", err)
	}
	m.proc = proc

	select {
	case u := <-wsURLCh:
		return u, false, nil
	case <-proc.Done():
		stderrTail := proc.StderrTail()
		exitErr := proc.ExitError()
		quickExit := time.Since(startAt) <= 2*time.Second
		lockErr := isProfileLockError(stderrTail) || (quickExit && hasProfileLockFiles(profile))
		m.stopChromiumLocked()
		return "", lockErr, fmt.Errorf("chromium exited prematurely: %v (stderr: %s)", exitErr, stderrTail)
	case <-time.After(15 * time.Second):
		stderrTail := proc.StderrTail()
		m.stopChromiumLocked()
		return "", false, fmt.Errorf("timed out waiting for DevTools WebSocket URL from chromium (stderr: %s)", stderrTail)
	case <-ctx.Done():
		m.stopChromiumLocked()
		return "", false, ctx.Err()
	}
}

var profileLockFiles = []string{"SingletonLock", "SingletonSocket", "SingletonCookie"}

func hasProfileLockFiles(profileDir string) bool {
	if profileDir == "" {
		return false
	}
	for _, name := range profileLockFiles {
		if _, err := os.Lstat(filepath.Join(profileDir, name)); err == nil {
			return true
		}
	}
	return false
}

func cleanStaleProfileLocks(profileDir string) bool {
	if profileDir == "" {
		return false
	}
	singletonPath := filepath.Join(profileDir, "SingletonLock")
	if target, err := os.Readlink(singletonPath); err == nil && target != "" {
		if isLiveSingletonTarget(target) {
			return false
		}
	}
	removed := false
	for _, name := range profileLockFiles {
		p := filepath.Join(profileDir, name)
		if _, err := os.Lstat(p); err == nil {
			if rmErr := os.Remove(p); rmErr == nil {
				removed = true
			}
		}
	}
	return removed
}

func isProfileLockError(stderrTail string) bool {
	if stderrTail == "" {
		return false
	}
	msg := strings.ToLower(stderrTail)
	return strings.Contains(msg, "singletonlock") ||
		strings.Contains(msg, "processsingleton") ||
		strings.Contains(msg, "profile appears to be in use") ||
		strings.Contains(msg, "in use by another") ||
		strings.Contains(msg, "cannot create a lock")
}

func (m *Manager) stopChromiumLocked() {
	m.anchorTargetID = ""
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}

	if m.cdp != nil {
		_ = m.cdp.Close()
		m.cdp = nil
	}

	if m.proc != nil {
		pid := m.proc.PID()
		_ = m.proc.Shutdown(1 * time.Second)
		m.proc = nil

		// Fallback for Windows to kill child processes in tree
		if runtime.GOOS == "windows" && pid > 0 {
			killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
			hideConsoleWindow(killCmd)
			_ = killCmd.Run()
		}
	}

	if m.isTempProfile && m.profileDir != "" {
		_ = os.RemoveAll(m.profileDir)
		m.profileDir = ""
		m.isTempProfile = false
	}
}

// Open creates a new browser tab within headless Chromium.
func (m *Manager) Open(ctx context.Context, threadID, initialURL string, width, height int) (SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isClosed {
		return SessionInfo{}, ErrBrowserClosed
	}

	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}

	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}
	width = clamp(width, 200, 2560)
	height = clamp(height, 160, 1600)

	if err := m.ensureChromiumLocked(ctx); err != nil {
		return SessionInfo{}, err
	}

	// 1. Create target with auto-recovery retry on dead CDP connection
	var createResult struct {
		TargetID string `json:"targetId"`
	}
	createTargetParams := map[string]any{
		"url": "about:blank",
	}
	if err := m.cdp.Call(ctx, "", "Target.createTarget", createTargetParams, &createResult); err != nil {
		stderrTail := ""
		if m.proc != nil {
			stderrTail = m.proc.StderrTail()
		}
		if isClosedCDPError(err) || !m.cdp.Alive() {
			logger.Warnf("BrowserManager", "Target.createTarget failed with closed CDP (%v), resetting and retrying once... (stderr: %s)", err, stderrTail)
			m.stopChromiumLocked()
			if recErr := m.ensureChromiumLocked(ctx); recErr != nil {
				return SessionInfo{}, fmt.Errorf("failed to recover chromium after cdp error: %w (stderr: %s)", recErr, stderrTail)
			}
			if retryErr := m.cdp.Call(ctx, "", "Target.createTarget", createTargetParams, &createResult); retryErr != nil {
				if m.proc != nil {
					stderrTail = m.proc.StderrTail()
				}
				return SessionInfo{}, fmt.Errorf("failed to create target after recovery: %w (stderr: %s)", retryErr, stderrTail)
			}
		} else {
			return SessionInfo{}, fmt.Errorf("failed to create target: %w (stderr: %s)", err, stderrTail)
		}
	}

	// 2. Attach with flatten: true
	var attachResult struct {
		SessionID string `json:"sessionId"`
	}
	attachParams := map[string]any{
		"targetId": createResult.TargetID,
		"flatten":  true,
	}
	if err := m.cdp.Call(ctx, "", "Target.attachToTarget", attachParams, &attachResult); err != nil {
		_ = m.cdp.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": createResult.TargetID}, nil)
		return SessionInfo{}, fmt.Errorf("failed to attach to target: %w", err)
	}

	m.nextTabNum++
	tabID := fmt.Sprintf("tab-%d-%s", m.nextTabNum, createResult.TargetID[:min(8, len(createResult.TargetID))])

	initURL := initialURL
	if initURL == "" {
		initURL = "about:blank"
	}

	t := &tab{
		id:          tabID,
		threadID:    threadID,
		targetID:    createResult.TargetID,
		sessionID:   attachResult.SessionID,
		url:         initURL,
		loading:     initURL != "about:blank",
		width:       width,
		height:      height,
		dpr:         1.0,
		subscribers: make(map[chan tabEvent]struct{}),
	}
	m.tabs[tabID] = t

	// 3. Enable Page domain and set device metrics
	_ = m.cdp.Call(ctx, t.sessionID, "Page.enable", nil, nil)
	_ = m.cdp.Call(ctx, t.sessionID, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": 1.0,
		"mobile":            false,
	}, nil)

	// 4. Register event handler
	m.cdp.RegisterSession(t.sessionID, func(method string, params json.RawMessage) {
		m.handleTabCDPEvent(t, method, params)
	})

	// 5. Navigate if initial URL provided
	if initialURL != "" && initialURL != "about:blank" {
		go func() {
			navCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if navErr := m.Navigate(navCtx, tabID, initialURL); navErr != nil {
				t.mu.RLock()
				subs := make([]chan tabEvent, 0, len(t.subscribers))
				for ch := range t.subscribers {
					subs = append(subs, ch)
				}
				t.mu.RUnlock()
				for _, ch := range subs {
					select {
					case ch <- tabEvent{err: navErr.Error()}:
					default:
					}
				}
			}
		}()
	}

	return t.info(), nil
}

func (m *Manager) handleTabCDPEvent(t *tab, method string, params json.RawMessage) {
	switch method {
	case "Page.screencastFrame":
		var ev struct {
			Data      string `json:"data"`
			SessionID any    `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &ev); err != nil {
			return
		}

		// Non-blocking immediate ACK so Chrome continues frame delivery
		go func(sessID string, ackID any) {
			m.mu.Lock()
			cdp := m.cdp
			m.mu.Unlock()
			if cdp != nil && ackID != nil {
				ackCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = cdp.Call(ackCtx, sessID, "Page.screencastFrameAck", map[string]any{
					"sessionId": ackID,
				}, nil)
			}
		}(t.sessionID, ev.SessionID)

		jpegBytes, err := base64.StdEncoding.DecodeString(ev.Data)
		if err != nil {
			return
		}

		t.mu.Lock()
		t.lastFrame = jpegBytes
		subscribers := make([]chan tabEvent, 0, len(t.subscribers))
		for ch := range t.subscribers {
			subscribers = append(subscribers, ch)
		}
		t.mu.Unlock()

		for _, ch := range subscribers {
			select {
			case ch <- tabEvent{isBinary: true, binary: jpegBytes}:
			default:
				// Lagging subscriber: drop stale frame so latency never builds up
			}
		}

	case "Page.frameNavigated":
		var ev struct {
			Frame struct {
				ID       string `json:"id"`
				ParentID string `json:"parentId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"frame"`
		}
		if err := json.Unmarshal(params, &ev); err == nil && ev.Frame.ParentID == "" {
			t.mu.Lock()
			t.url = ev.Frame.URL
			if ev.Frame.Title != "" {
				t.title = ev.Frame.Title
			}
			t.mu.Unlock()
			m.refreshNavHistory(t)
			m.broadcastState(t)
		}

	case "Page.navigatedWithinDocument":
		var ev struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(params, &ev); err == nil {
			t.mu.Lock()
			t.url = ev.URL
			t.mu.Unlock()
			m.refreshNavHistory(t)
			m.broadcastState(t)
		}

	case "Page.frameStartedLoading":
		t.mu.Lock()
		t.loading = true
		t.mu.Unlock()
		m.broadcastState(t)

	case "Page.frameStoppedLoading":
		t.mu.Lock()
		t.loading = false
		hasFrame := len(t.lastFrame) > 0
		paused := t.paused
		t.mu.Unlock()
		m.refreshNavHistory(t)
		m.broadcastState(t)
		if !hasFrame && !paused {
			go m.pushFallbackFrame(t)
		}

	case "Page.loadEventFired":
		t.mu.RLock()
		hasFrame := len(t.lastFrame) > 0
		paused := t.paused
		t.mu.RUnlock()
		m.refreshNavHistory(t)
		m.broadcastState(t)
		if !hasFrame && !paused {
			go m.pushFallbackFrame(t)
		}

	case "Page.windowOpen":
		var ev struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(params, &ev); err == nil && ev.URL != "" {
			go func() {
				_ = m.Navigate(context.Background(), t.id, ev.URL)
			}()
		}
	}
}

func (m *Manager) refreshNavHistory(t *tab) {
	m.mu.Lock()
	cdp := m.cdp
	m.mu.Unlock()
	if cdp == nil {
		return
	}

	var res struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID    int    `json:"id"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"entries"`
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := cdp.Call(ctx, t.sessionID, "Page.getNavigationHistory", nil, &res); err != nil {
		return
	}

	t.mu.Lock()
	t.canGoBack = res.CurrentIndex > 0
	t.canGoForward = res.CurrentIndex < len(res.Entries)-1
	if res.CurrentIndex >= 0 && res.CurrentIndex < len(res.Entries) {
		current := res.Entries[res.CurrentIndex]
		if current.URL != "" {
			t.url = current.URL
		}
		if current.Title != "" {
			t.title = current.Title
		}
	}
	t.mu.Unlock()
}

func (m *Manager) broadcastState(t *tab) {
	info := t.info()
	t.mu.RLock()
	subscribers := make([]chan tabEvent, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subscribers = append(subscribers, ch)
	}
	t.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- tabEvent{state: info}:
		default:
		}
	}
}

func (m *Manager) pushFallbackFrame(t *tab) {
	m.mu.Lock()
	cdp := m.cdp
	m.mu.Unlock()
	if cdp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var res struct {
		Data string `json:"data"`
	}
	if err := cdp.Call(ctx, t.sessionID, "Page.captureScreenshot", map[string]any{
		"format":  "jpeg",
		"quality": 82,
	}, &res); err != nil || res.Data == "" {
		return
	}
	jpegBytes, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil || len(jpegBytes) == 0 {
		return
	}
	t.mu.Lock()
	t.lastFrame = jpegBytes
	subscribers := make([]chan tabEvent, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subscribers = append(subscribers, ch)
	}
	t.mu.Unlock()
	for _, ch := range subscribers {
		select {
		case ch <- tabEvent{isBinary: true, binary: jpegBytes}:
		default:
		}
	}
}

// Navigate tells a tab to navigate to a URL.
func (m *Manager) Navigate(ctx context.Context, id, url string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	t.mu.Lock()
	t.loading = true
	t.mu.Unlock()
	m.broadcastState(t)

	var navRes struct {
		ErrorText string `json:"errorText"`
	}
	err := cdp.Call(ctx, t.sessionID, "Page.navigate", map[string]any{"url": url}, &navRes)
	if err != nil {
		logger.Warnf("BrowserManager", "Page.navigate failed for tab=%s url=%q: %v", id, url, err)
		t.mu.Lock()
		t.loading = false
		t.mu.Unlock()
		m.broadcastState(t)
		return err
	}
	if navRes.ErrorText != "" && navRes.ErrorText != "net::ERR_ABORTED" {
		logger.Warnf("BrowserManager", "Page.navigate errorText for tab=%s url=%q: %s", id, url, navRes.ErrorText)
		t.mu.Lock()
		t.loading = false
		t.mu.Unlock()
		m.broadcastState(t)
		return fmt.Errorf("navigation failed: %s", navRes.ErrorText)
	}
	return nil
}

// Back navigates backward in history.
func (m *Manager) Back(ctx context.Context, id string) error {
	return m.moveHistory(ctx, id, -1)
}

// Forward navigates forward in history.
func (m *Manager) Forward(ctx context.Context, id string) error {
	return m.moveHistory(ctx, id, 1)
}

func (m *Manager) moveHistory(ctx context.Context, id string, offset int) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	var res struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}

	if err := cdp.Call(ctx, t.sessionID, "Page.getNavigationHistory", nil, &res); err != nil {
		return err
	}

	targetIndex := res.CurrentIndex + offset
	if targetIndex < 0 || targetIndex >= len(res.Entries) {
		return nil
	}

	return cdp.Call(ctx, t.sessionID, "Page.navigateToHistoryEntry", map[string]any{
		"entryId": res.Entries[targetIndex].ID,
	}, nil)
}

// Reload reloads the current page.
func (m *Manager) Reload(ctx context.Context, id string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	t.mu.Lock()
	t.loading = true
	t.mu.Unlock()
	m.broadcastState(t)

	return cdp.Call(ctx, t.sessionID, "Page.reload", map[string]any{"ignoreCache": false}, nil)
}

func screencastParams(width, height int, dpr float64) map[string]any {
	if dpr < 1.0 {
		dpr = 1.0
	} else if dpr > 2.0 {
		dpr = 2.0
	}
	maxW := clamp(int(math.Round(float64(width)*dpr)), 200, 2560)
	maxH := clamp(int(math.Round(float64(height)*dpr)), 160, 1600)
	return map[string]any{
		"format":        "jpeg",
		"quality":       82,
		"everyNthFrame": 1,
		"maxWidth":      maxW,
		"maxHeight":     maxH,
	}
}

// Resize changes the viewport dimensions and device scale factor of the tab.
func (m *Manager) Resize(ctx context.Context, id string, width, height int, dpr float64) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	width = clamp(width, 200, 2560)
	height = clamp(height, 160, 1600)
	if dpr < 1.0 {
		dpr = 1.0
	} else if dpr > 2.0 {
		dpr = 2.0
	}

	t.mu.Lock()
	changed := t.width != width || t.height != height || t.dpr != dpr
	t.width = width
	t.height = height
	t.dpr = dpr
	subCount := len(t.subscribers)
	paused := t.paused
	sessID := t.sessionID
	t.mu.Unlock()

	if !changed {
		return nil
	}

	if err := cdp.Call(ctx, sessID, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width":             width,
		"height":            height,
		"deviceScaleFactor": dpr,
		"mobile":            false,
	}, nil); err != nil {
		return err
	}

	if subCount > 0 && !paused {
		_ = cdp.Call(ctx, sessID, "Page.stopScreencast", nil, nil)
		_ = cdp.Call(ctx, sessID, "Page.startScreencast", screencastParams(width, height, dpr), nil)
	}
	return nil
}

// SetPaused enables or halts screencast delivery on this tab.
func (m *Manager) SetPaused(ctx context.Context, id string, paused bool) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	t.mu.Lock()
	t.paused = paused
	subCount := len(t.subscribers)
	params := screencastParams(t.width, t.height, t.dpr)
	sessID := t.sessionID
	t.mu.Unlock()

	if paused {
		return cdp.Call(ctx, sessID, "Page.stopScreencast", nil, nil)
	}

	if subCount > 0 {
		return cdp.Call(ctx, sessID, "Page.startScreencast", params, nil)
	}

	return nil
}

// CaptureScreenshot captures a full-resolution PNG screenshot of the tab and returns base64-encoded PNG data.
func (m *Manager) CaptureScreenshot(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return "", ErrTabNotFound
	}
	if cdp == nil {
		return "", ErrBrowserClosed
	}

	var res struct {
		Data string `json:"data"`
	}
	if err := cdp.Call(ctx, t.sessionID, "Page.captureScreenshot", map[string]any{
		"format": "png",
	}, &res); err != nil {
		return "", err
	}
	return res.Data, nil
}

func normalizeMouseButton(button string) string {
	switch button {
	case "3":
		return "back"
	case "4":
		return "forward"
	default:
		return button
	}
}

// DispatchMouse sends mouse events into the CDP tab.
func (m *Manager) DispatchMouse(ctx context.Context, id string, ev MouseEvent) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	btn := normalizeMouseButton(ev.Button)
	switch ev.Type {
	case "mousePressed":
		if btn == "back" || ((btn == "" || btn == "none") && (ev.Buttons&8) != 0) {
			return m.Back(ctx, id)
		}
		if btn == "forward" || ((btn == "" || btn == "none") && (ev.Buttons&16) != 0) {
			return m.Forward(ctx, id)
		}
	case "mouseReleased":
		if btn == "back" || btn == "forward" {
			return nil
		}
	}

	params := map[string]any{
		"type":       ev.Type,
		"x":          ev.X,
		"y":          ev.Y,
		"button":     btn,
		"buttons":    ev.Buttons,
		"clickCount": ev.ClickCount,
		"deltaX":     ev.DeltaX,
		"deltaY":     ev.DeltaY,
		"modifiers":  ev.Modifiers,
	}
	return cdp.Call(ctx, t.sessionID, "Input.dispatchMouseEvent", params, nil)
}

// DispatchKey sends key events into the CDP tab.
func (m *Manager) DispatchKey(ctx context.Context, id string, ev KeyEvent) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	if ev.Type == "keyDown" && (ev.Key == "Enter" || ev.Code == "Enter" || ev.Code == "NumpadEnter") {
		if ev.Text == "" {
			ev.Text = "\r"
		}
		if ev.UnmodifiedText == "" {
			ev.UnmodifiedText = "\r"
		}
		if ev.WindowsVirtualKeyCode == 0 {
			ev.WindowsVirtualKeyCode = 13
		}
		if ev.NativeVirtualKeyCode == 0 {
			ev.NativeVirtualKeyCode = 13
		}
	}

	params := map[string]any{
		"type":                  ev.Type,
		"key":                   ev.Key,
		"code":                  ev.Code,
		"text":                  ev.Text,
		"unmodifiedText":        ev.UnmodifiedText,
		"windowsVirtualKeyCode": ev.WindowsVirtualKeyCode,
		"nativeVirtualKeyCode":  ev.NativeVirtualKeyCode,
		"modifiers":             ev.Modifiers,
	}
	return cdp.Call(ctx, t.sessionID, "Input.dispatchKeyEvent", params, nil)
}

// InsertText inserts text directly into the focused input element (e.g. paste).
func (m *Manager) InsertText(ctx context.Context, id, text string) error {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return ErrTabNotFound
	}
	if cdp == nil {
		return ErrBrowserClosed
	}

	return cdp.Call(ctx, t.sessionID, "Input.insertText", map[string]any{"text": text}, nil)
}

// Close destroys a single tab and triggers idle cleanup if no tabs remain.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tabs[id]
	if !ok || t == nil {
		return nil
	}

	delete(m.tabs, id)

	if m.cdp != nil {
		m.cdp.UnregisterSession(t.sessionID)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = m.cdp.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
		cancel()
	}

	t.mu.Lock()
	for ch := range t.subscribers {
		close(ch)
	}
	t.subscribers = nil
	t.mu.Unlock()

	if len(m.tabs) == 0 {
		m.scheduleIdleShutdownLocked()
	}

	return nil
}

func (m *Manager) scheduleIdleShutdownLocked() {
	if m.idleTimer != nil {
		m.idleTimer.Stop()
	}
	m.idleTimer = time.AfterFunc(15*time.Second, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if len(m.tabs) == 0 {
			logger.Infof("BrowserManager", "Idle grace expired (0 tabs), shutting down Chromium")
			m.stopChromiumLocked()
		}
	})
}

// CloseThread closes all tabs belonging to threadID.
func (m *Manager) CloseThread(threadID string) {
	m.mu.Lock()
	var toClose []string
	for id, t := range m.tabs {
		if t.threadID == threadID {
			toClose = append(toClose, id)
		}
	}
	m.mu.Unlock()

	for _, id := range toClose {
		_ = m.Close(id)
	}
}

// CloseAll immediately terminates all tabs and the Chromium process.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.isClosed = true
	for id, t := range m.tabs {
		delete(m.tabs, id)
		if m.cdp != nil {
			m.cdp.UnregisterSession(t.sessionID)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = m.cdp.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": t.targetID}, nil)
			cancel()
		}
		t.mu.Lock()
		for ch := range t.subscribers {
			close(ch)
		}
		t.subscribers = nil
		t.mu.Unlock()
	}

	m.stopChromiumLocked()
}

// Subscribe attaches a viewer to a tab, returning its initial state, cached last frame,
// and an event stream. Starts screencast if it is the first subscriber.
func (m *Manager) Subscribe(id string) (SessionInfo, []byte, chan tabEvent, func(), error) {
	m.mu.Lock()
	t, ok := m.tabs[id]
	cdp := m.cdp
	m.mu.Unlock()

	if !ok || t == nil {
		return SessionInfo{}, nil, nil, nil, ErrTabNotFound
	}

	eventCh := make(chan tabEvent, 32)

	t.mu.Lock()
	t.subscribers[eventCh] = struct{}{}
	initialInfo := SessionInfo{
		ID:           t.id,
		ThreadID:     t.threadID,
		URL:          t.url,
		Title:        t.title,
		Loading:      t.loading,
		CanGoBack:    t.canGoBack,
		CanGoForward: t.canGoForward,
		Width:        t.width,
		Height:       t.height,
	}
	lastFrame := make([]byte, len(t.lastFrame))
	copy(lastFrame, t.lastFrame)
	subCount := len(t.subscribers)
	paused := t.paused
	sessID := t.sessionID
	scParams := screencastParams(t.width, t.height, t.dpr)
	t.mu.Unlock()

	// Start screencast if first subscriber and not paused
	if subCount == 1 && !paused && cdp != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = cdp.Call(ctx, sessID, "Page.startScreencast", scParams, nil)
		}()
	}

	cancel := func() {
		t.mu.Lock()
		delete(t.subscribers, eventCh)
		remaining := len(t.subscribers)
		t.mu.Unlock()

		// Stop screencast if no subscribers left
		if remaining == 0 && cdp != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = cdp.Call(ctx, sessID, "Page.stopScreencast", nil, nil)
			}()
		}
	}

	return initialInfo, lastFrame, eventCh, cancel, nil
}

func clamp(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func isClosedCDPError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrCDPClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") || strings.Contains(msg, "eof") || strings.Contains(msg, "broken pipe")
}
