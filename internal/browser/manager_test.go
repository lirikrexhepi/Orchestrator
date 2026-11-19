package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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

	// 4. Open tab to /blocked
	blockedURL := httpServer.URL + "/blocked"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sess, err := mgr.Open(ctx, "test-thread-1", blockedURL, 800, 600)
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
		out, _ := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", chromePID)).Output()
		if strings.Contains(string(out), strconv.Itoa(chromePID)) {
			t.Errorf("chrome process %d still running after CloseAll()", chromePID)
		}
	}
}
