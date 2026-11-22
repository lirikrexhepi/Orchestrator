package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestLiveHeadlessChromiumCDP(t *testing.T) {
	if _, err := FindChromium(); err != nil {
		t.Skipf("Skipping live CDP test: %v", err)
	}

	// 1. HTTP server serving /blocked with X-Frame-Options: DENY and CSP frame-ancestors 'none'
	mux := http.NewServeMux()
	mux.HandleFunc("/blocked", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>CDP_OK_TITLE</title></head><body><h1>Loaded despite DENY</h1></body></html>"))
	})
	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><head><title>PAGE_2</title></head><body><h1>Page Two</h1></body></html>"))
	})
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	// 2. Start Manager and loopback WebSocket Server
	tempDir := t.TempDir()
	mgr := NewManager(tempDir)
	defer mgr.CloseAll()

	srv := NewServer(mgr)
	wsURL, token, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start browser server: %v", err)
	}
	defer func() { _ = srv.Close(context.Background()) }()

	// 3. Test Unauthorized request with bad token returns 401
	badURL := fmt.Sprintf("%s?id=any&token=invalid_token", wsURL)
	_, resp, err := websocket.DefaultDialer.Dial(badURL, nil)
	if err == nil {
		t.Fatal("expected dial error with bad token, got nil")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", resp.StatusCode)
	}

	// 4. Open tab to /blocked using a short-lived RPC context (mirrors desktop/app_browser.go
	// where BrowserOpen cancels ctx immediately on return — Chromium MUST stay alive after cancel).
	blockedURL := httpServer.URL + "/blocked"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	sess, err := mgr.Open(ctx, "test-thread-1", blockedURL, 800, 600)
	cancel()
	if err != nil {
		t.Fatalf("failed to open tab: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}

	chromePID := 0
	if mgr.proc != nil {
		chromePID = mgr.proc.PID()
	}

	// 5. Connect over loopback WebSocket
	tabWSURL := fmt.Sprintf("%s?id=%s&token=%s", wsURL, sess.ID, token)
	ws, _, err := websocket.DefaultDialer.Dial(tabWSURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer ws.Close()

	// 6. Assert state update reports title == "CDP_OK_TITLE" and valid JPEG binary frame arrives
	gotTitle := false
	gotFrame := false
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) && (!gotTitle || !gotFrame) {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			continue
		}

		if msgType == websocket.BinaryMessage {
			if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
				gotFrame = true
			}
		} else if msgType == websocket.TextMessage {
			var payload struct {
				Type  string      `json:"type"`
				State SessionInfo `json:"state"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.Type == "state" {
				if payload.State.Title == "CDP_OK_TITLE" && strings.HasSuffix(payload.State.URL, "/blocked") {
					gotTitle = true
				}
			}
		}
	}

	if !gotTitle {
		t.Errorf("did not receive state with title 'CDP_OK_TITLE' and URL ending in /blocked")
	}
	if !gotFrame {
		t.Errorf("did not receive valid JPEG screencast frame (0xFF 0xD8)")
	}

	// 7. Navigate to /page2
	page2URL := httpServer.URL + "/page2"
	if err := ws.WriteJSON(map[string]any{"type": "navigate", "url": page2URL}); err != nil {
		t.Fatalf("failed to write navigate: %v", err)
	}

	gotPage2 := false
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !gotPage2 {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			continue
		}
		if msgType == websocket.TextMessage {
			var payload struct {
				Type  string      `json:"type"`
				State SessionInfo `json:"state"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.Type == "state" {
				if payload.State.Title == "PAGE_2" {
					gotPage2 = true
				}
			}
		}
	}
	if !gotPage2 {
		t.Fatalf("did not reach PAGE_2 after navigate command")
	}

	// 8. Send "back" and assert state returns to /blocked (CDP_OK_TITLE)
	if err := ws.WriteJSON(map[string]any{"type": "back"}); err != nil {
		t.Fatalf("failed to write back: %v", err)
	}

	returnedToBlocked := false
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !returnedToBlocked {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			continue
		}
		if msgType == websocket.TextMessage {
			var payload struct {
				Type  string      `json:"type"`
				State SessionInfo `json:"state"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.Type == "state" {
				if payload.State.Title == "CDP_OK_TITLE" {
					returnedToBlocked = true
				}
			}
		}
	}
	if !returnedToBlocked {
		t.Errorf("did not return to CDP_OK_TITLE after back command")
	}

	// 9. Close websocket and CloseAll() to verify clean process-tree termination
	_ = ws.Close()
	mgr.CloseAll()

	if chromePID > 0 && runtime.GOOS == "windows" {
		time.Sleep(500 * time.Millisecond)
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", chromePID))
		hideConsoleWindow(cmd)
		out, _ := cmd.Output()
		if strings.Contains(string(out), strconv.Itoa(chromePID)) {
			t.Errorf("chrome process %d still running after CloseAll()", chromePID)
		}
	}
}

func TestHideConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo test")
	hideConsoleWindow(cmd)
	if runtime.GOOS == "windows" {
		if cmd.SysProcAttr == nil {
			t.Fatal("expected SysProcAttr to be non-nil on Windows")
		}
		if !cmd.SysProcAttr.HideWindow {
			t.Error("expected HideWindow to be true")
		}
		if cmd.SysProcAttr.CreationFlags&0x08000000 == 0 {
			t.Error("expected CREATE_NO_WINDOW (0x08000000) flag to be set")
		}
	}
}

func TestLiveCDP_OpenCloseReopenAndRecovery(t *testing.T) {
	if _, err := FindChromium(); err != nil {
		t.Skipf("Skipping live CDP test: %v", err)
	}

	tempDir := t.TempDir()
	mgr := NewManager(tempDir)
	defer mgr.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Open Tab 1
	sess1, err := mgr.Open(ctx, "thread-rec", "about:blank", 800, 600)
	if err != nil {
		t.Fatalf("failed to open Tab 1: %v", err)
	}
	if sess1.ID == "" {
		t.Fatal("expected non-empty session ID for Tab 1")
	}

	// 2. Close Tab 1
	if err := mgr.Close(sess1.ID); err != nil {
		t.Fatalf("failed to close Tab 1: %v", err)
	}

	// 3. Immediately open Tab 2 (must not fail with 'cdp connection closed')
	sess2, err := mgr.Open(ctx, "thread-rec", "about:blank", 800, 600)
	if err != nil {
		t.Fatalf("failed to open Tab 2 immediately after closing Tab 1 (anchor tab failed?): %v", err)
	}
	if sess2.ID == "" {
		t.Fatal("expected non-empty session ID for Tab 2")
	}

	// 4. Record chrome PID, kill chrome unexpectedly, and verify auto-recovery
	oldPID := 0
	if mgr.proc != nil {
		oldPID = mgr.proc.PID()
		_ = mgr.proc.Shutdown(0)
	}
	time.Sleep(300 * time.Millisecond)

	// 5. Open Tab 3: should auto-recover, relaunch Chromium cleanly, and succeed
	sess3, err := mgr.Open(ctx, "thread-rec", "about:blank", 800, 600)
	if err != nil {
		t.Fatalf("failed to auto-recover and open Tab 3 after process kill: %v", err)
	}
	if sess3.ID == "" {
		t.Fatal("expected non-empty session ID for Tab 3")
	}
	if mgr.cdp == nil || !mgr.cdp.Alive() {
		t.Fatal("expected active CDP connection after recovery")
	}

	newPID := 0
	if mgr.proc != nil {
		newPID = mgr.proc.PID()
	}

	// 6. CloseAll and verify zero leaked processes
	mgr.CloseAll()
	time.Sleep(500 * time.Millisecond)

	if runtime.GOOS == "windows" {
		for _, pid := range []int{oldPID, newPID} {
			if pid > 0 {
				cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid))
				hideConsoleWindow(cmd)
				out, _ := cmd.Output()
				if strings.Contains(string(out), strconv.Itoa(pid)) {
					t.Errorf("chrome process %d still running after CloseAll()", pid)
				}
			}
		}
	}
}

func TestScreencastParams_BoundedAndClamped(t *testing.T) {
	p1 := screencastParams(1280, 800, 1.0)
	if p1["maxWidth"] != 1280 || p1["maxHeight"] != 800 {
		t.Fatalf("expected 1280x800, got %vx%v", p1["maxWidth"], p1["maxHeight"])
	}
	if p1["quality"] != 95 {
		t.Fatalf("expected quality 95, got %v", p1["quality"])
	}

	p2 := screencastParams(1920, 1080, 2.0)
	if p2["maxWidth"] != 3840 || p2["maxHeight"] != 2160 {
		t.Fatalf("expected Retina 4K 3840x2160, got %vx%v", p2["maxWidth"], p2["maxHeight"])
	}

	p3 := screencastParams(100, 50, 0.5)
	if p3["maxWidth"] != 200 || p3["maxHeight"] != 160 {
		t.Fatalf("expected minimum clamp 200x160, got %vx%v", p3["maxWidth"], p3["maxHeight"])
	}
}

func TestCleanStaleProfileLocks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range profileLockFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0o644); err != nil {
			t.Fatalf("failed to write lock file %s: %v", name, err)
		}
	}

	if !cleanStaleProfileLocks(dir) {
		t.Fatal("expected cleanStaleProfileLocks to report removed files")
	}
	if hasProfileLockFiles(dir) {
		t.Fatal("expected all stale lock files to be removed")
	}
}

func TestLiveCDP_StaleSingletonLockRecovery(t *testing.T) {
	if _, err := FindChromium(); err != nil {
		t.Skipf("Skipping live CDP test: %v", err)
	}

	dir := t.TempDir()
	for _, name := range profileLockFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale-lock"), 0o644); err != nil {
			t.Fatalf("failed to seed lock file %s: %v", name, err)
		}
	}

	mgr := NewManager(dir)
	defer mgr.CloseAll()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sess, err := mgr.Open(ctx, "thread-lock-rec", "about:blank", 800, 600)
	if err != nil {
		t.Fatalf("expected stale SingletonLock recovery to succeed, got error: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

func startMockCDPServer(t *testing.T, handler func(req cdpRequest, writeResp func(any))) (*CDPClient, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var writeMu sync.Mutex
		writeResp := func(v any) {
			writeMu.Lock()
			defer writeMu.Unlock()
			_ = conn.WriteJSON(v)
		}
		for {
			var req cdpRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			handler(req, writeResp)
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := DialCDP(ctx, wsURL)
	if err != nil {
		srv.Close()
		t.Fatalf("DialCDP failed: %v", err)
	}
	return client, func() {
		_ = client.Close()
		srv.Close()
	}
}

func TestCDP_ScreencastBufferAndAckOnOverflow(t *testing.T) {
	ackedCh := make(chan int, 2000)
	var serverConn *websocket.Conn
	var connMu sync.Mutex
	connected := make(chan struct{})

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()
		close(connected)

		for {
			var req cdpRequest
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			if req.Method == "Page.screencastFrameAck" {
				var p struct {
					SessionID int `json:"sessionId"`
				}
				b, _ := json.Marshal(req.Params)
				_ = json.Unmarshal(b, &p)
				ackedCh <- p.SessionID
			}
			connMu.Lock()
			_ = conn.WriteJSON(map[string]any{"id": req.ID, "result": map[string]any{}})
			connMu.Unlock()
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := DialCDP(ctx, wsURL)
	if err != nil {
		t.Fatalf("DialCDP failed: %v", err)
	}
	defer client.Close()

	<-connected

	releaseHandler := make(chan struct{})
	handlerEntered := make(chan struct{})
	var enterOnce sync.Once
	client.RegisterSession("sess-1", func(method string, params json.RawMessage) {
		enterOnce.Do(func() { close(handlerEntered) })
		<-releaseHandler
	})

	client.mu.RLock()
	disp := client.sessions["sess-1"]
	client.mu.RUnlock()
	if cap(disp.events) < 1024 {
		t.Fatalf("expected session event buffer >= 1024, got %d", cap(disp.events))
	}

	// Send 1st frame so worker enters and blocks on releaseHandler, then overflow > 1024 frames.
	const totalFrames = 1050
	for i := 1; i <= totalFrames; i++ {
		connMu.Lock()
		err := serverConn.WriteJSON(map[string]any{
			"method":    "Page.screencastFrame",
			"sessionId": "sess-1",
			"params": map[string]any{
				"sessionId": i,
				"data":      "",
			},
		})
		connMu.Unlock()
		if err != nil {
			t.Fatalf("failed to write frame %d: %v", i, err)
		}
		if i == 1 {
			<-handlerEntered
		}
	}

	// Expect overflowed frames to be ACKed without waiting on the blocked handler.
	expectedOverflowAcks := totalFrames - 1 - cap(disp.events)
	deadline := time.After(5 * time.Second)
	gotAcks := 0
	for gotAcks < expectedOverflowAcks {
		select {
		case <-ackedCh:
			gotAcks++
		case <-deadline:
			close(releaseHandler)
			t.Fatalf("timed out waiting for overflow ACKs: got %d, want at least %d", gotAcks, expectedOverflowAcks)
		}
	}
	close(releaseHandler)
}

func TestDispatchKey_EnterAndMouseButtons34(t *testing.T) {
	var mu sync.Mutex
	var recorded []cdpRequest

	client, cleanup := startMockCDPServer(t, func(req cdpRequest, writeResp func(any)) {
		mu.Lock()
		recorded = append(recorded, req)
		mu.Unlock()

		switch req.Method {
		case "Page.getNavigationHistory":
			writeResp(map[string]any{
				"id": req.ID,
				"result": map[string]any{
					"currentIndex": 1,
					"entries": []map[string]any{
						{"id": 10},
						{"id": 11},
						{"id": 12},
					},
				},
			})
		default:
			writeResp(map[string]any{"id": req.ID, "result": map[string]any{}})
		}
	})
	defer cleanup()

	mgr := NewManager(t.TempDir())
	mgr.cdp = client
	mgr.tabs["tab-1"] = &tab{
		id:        "tab-1",
		sessionID: "sess-1",
		width:     1280,
		height:    800,
		dpr:       1.0,
	}

	ctx := context.Background()

	// 1. Enter keyDown with empty text/unmodifiedText should populate "\r"
	if err := mgr.DispatchKey(ctx, "tab-1", KeyEvent{
		Type: "keyDown",
		Key:  "Enter",
		Code: "Enter",
	}); err != nil {
		t.Fatalf("DispatchKey Enter failed: %v", err)
	}

	// 2. Mouse button "3" (back) -> navigates history back to entry 10
	if err := mgr.DispatchMouse(ctx, "tab-1", MouseEvent{
		Type:   "mousePressed",
		Button: "3",
	}); err != nil {
		t.Fatalf("DispatchMouse button 3 failed: %v", err)
	}
	// mouseReleased button "3" should be a clean no-op (no double navigation)
	if err := mgr.DispatchMouse(ctx, "tab-1", MouseEvent{
		Type:   "mouseReleased",
		Button: "3",
	}); err != nil {
		t.Fatalf("DispatchMouse button 3 release failed: %v", err)
	}

	// 3. Mouse button "4" (forward) -> navigates history forward to entry 12
	if err := mgr.DispatchMouse(ctx, "tab-1", MouseEvent{
		Type:   "mousePressed",
		Button: "4",
	}); err != nil {
		t.Fatalf("DispatchMouse button 4 failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	var keyParams map[string]any
	var navEntries []int
	for _, r := range recorded {
		b, _ := json.Marshal(r.Params)
		switch r.Method {
		case "Input.dispatchKeyEvent":
			_ = json.Unmarshal(b, &keyParams)
		case "Page.navigateToHistoryEntry":
			var p struct {
				EntryID int `json:"entryId"`
			}
			_ = json.Unmarshal(b, &p)
			navEntries = append(navEntries, p.EntryID)
		}
	}

	if keyParams["text"] != "\r" || keyParams["unmodifiedText"] != "\r" {
		t.Fatalf("expected Enter keyDown text/unmodifiedText to be '\\r', got %v / %v", keyParams["text"], keyParams["unmodifiedText"])
	}
	if len(navEntries) != 2 || navEntries[0] != 10 || navEntries[1] != 12 {
		t.Fatalf("expected history entries [10, 12] for mouse buttons 3 & 4, got %v", navEntries)
	}
}

func TestServer_CaptureScreenshotWS(t *testing.T) {
	wantPNG := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	client, cleanup := startMockCDPServer(t, func(req cdpRequest, writeResp func(any)) {
		if req.Method == "Page.captureScreenshot" {
			writeResp(map[string]any{
				"id":     req.ID,
				"result": map[string]any{"data": wantPNG},
			})
			return
		}
		writeResp(map[string]any{"id": req.ID, "result": map[string]any{}})
	})
	defer cleanup()

	mgr := NewManager(t.TempDir())
	mgr.cdp = client
	mgr.tabs["tab-cap"] = &tab{
		id:          "tab-cap",
		sessionID:   "sess-cap",
		width:       1280,
		height:      800,
		dpr:         1.0,
		subscribers: make(map[chan tabEvent]struct{}),
	}

	srv := NewServer(mgr)
	wsURL, token, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("srv.Start failed: %v", err)
	}
	defer func() { _ = srv.Close(context.Background()) }()

	ws, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("%s?id=tab-cap&token=%s", wsURL, token), nil)
	if err != nil {
		t.Fatalf("WS dial failed: %v", err)
	}
	defer ws.Close()

	if err := ws.WriteJSON(map[string]any{
		"type":      "capture",
		"requestId": "req-42",
	}); err != nil {
		t.Fatalf("failed to write capture command: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("WS ReadMessage failed: %v", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		var resp struct {
			Type      string `json:"type"`
			RequestID string `json:"requestId"`
			Data      string `json:"data"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(data, &resp); err == nil && resp.Type == "screenshot" {
			if resp.Error != "" {
				t.Fatalf("unexpected screenshot error: %s", resp.Error)
			}
			if resp.RequestID != "req-42" {
				t.Fatalf("expected requestId req-42, got %q", resp.RequestID)
			}
			if resp.Data != wantPNG {
				t.Fatalf("expected screenshot data %q, got %q", wantPNG, resp.Data)
			}
			return
		}
	}
	t.Fatal("timed out waiting for screenshot WS response")
}
