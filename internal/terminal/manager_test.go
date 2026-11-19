package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRingBuffer_WrapAroundAndExactCap(t *testing.T) {
	// 1. Exact default capacity
	r := NewRingBuffer()
	if r.Cap() != defaultScrollbackBytes {
		t.Fatalf("expected cap %d, got %d", defaultScrollbackBytes, r.Cap())
	}
	if r.Len() != 0 {
		t.Fatalf("expected len 0, got %d", r.Len())
	}
	if snap := r.Snapshot(); snap != nil {
		t.Fatalf("expected nil snapshot on empty buffer, got %v", snap)
	}

	// 2. Wrap-around on small capacity
	small := NewRingBuffer(10)
	_, _ = small.Write([]byte("hello"))
	if string(small.Snapshot()) != "hello" {
		t.Fatalf("expected 'hello', got '%s'", string(small.Snapshot()))
	}
	if small.Len() != 5 {
		t.Fatalf("expected len 5, got %d", small.Len())
	}

	_, _ = small.Write([]byte("world!")) // len 6 -> total 11 > 10, drops 'h'
	if string(small.Snapshot()) != "elloworld!" {
		t.Fatalf("expected 'elloworld!', got '%s'", string(small.Snapshot()))
	}
	if small.Len() != 10 {
		t.Fatalf("expected len 10, got %d", small.Len())
	}

	// 3. Write larger than capacity
	_, _ = small.Write([]byte("0123456789abcdef")) // len 16 > 10, keeps last 10
	if string(small.Snapshot()) != "6789abcdef" {
		t.Fatalf("expected '6789abcdef', got '%s'", string(small.Snapshot()))
	}
	if small.Len() != 10 {
		t.Fatalf("expected len 10, got %d", small.Len())
	}

	// 4. Exact 64 KB cap under heavy writes
	heavy := NewRingBuffer(defaultScrollbackBytes)
	chunk := bytes.Repeat([]byte("A"), 1024)
	for i := 0; i < 100; i++ { // 100 KB total
		_, _ = heavy.Write(chunk)
	}
	if heavy.Len() != defaultScrollbackBytes {
		t.Fatalf("expected exact len %d, got %d", defaultScrollbackBytes, heavy.Len())
	}
	snap := heavy.Snapshot()
	if len(snap) != defaultScrollbackBytes {
		t.Fatalf("expected snapshot len %d, got %d", defaultScrollbackBytes, len(snap))
	}

	// End marker
	marker := []byte("END_OF_BUFFER_DATA")
	_, _ = heavy.Write(marker)
	snap = heavy.Snapshot()
	if len(snap) != defaultScrollbackBytes {
		t.Fatalf("expected exact len %d after write, got %d", defaultScrollbackBytes, len(snap))
	}
	if !bytes.HasSuffix(snap, marker) {
		t.Fatalf("expected buffer to end with marker")
	}
}

func TestServer_AuthAndOriginRejection(t *testing.T) {
	mgr := NewManager()
	defer mgr.CloseAll()

	srv := NewServer(mgr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wsURL, token, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(ctx)

	// Create a dummy session with os.TempDir() so t.TempDir is not locked
	sess, err := mgr.Open("test-thread", os.TempDir(), "Test", 80, 24)
	if err != nil && runtime.GOOS == "windows" {
		t.Fatalf("Open failed: %v", err)
	}
	if sess.ID != "" {
		defer mgr.Close(sess.ID)
	}

	testSessID := sess.ID
	if testSessID == "" {
		testSessID = "dummy-id"
	}

	// 1. Missing token -> 401 Unauthorized
	{
		url := fmt.Sprintf("%s?id=%s", wsURL, testSessID)
		_, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			t.Fatal("expected dial error with missing token, got nil")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401, got resp: %+v", resp)
		}
	}

	// 2. Bad token -> 401 Unauthorized
	{
		url := fmt.Sprintf("%s?id=%s&token=invalid-secret-token", wsURL, testSessID)
		_, resp, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			t.Fatal("expected dial error with bad token, got nil")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401, got resp: %+v", resp)
		}
	}

	// 3. External Origin -> 403 Forbidden
	{
		url := fmt.Sprintf("%s?id=%s&token=%s", wsURL, testSessID, token)
		headers := http.Header{
			"Origin": []string{"http://evil-attacker.com"},
		}
		_, resp, err := websocket.DefaultDialer.Dial(url, headers)
		if err == nil {
			t.Fatal("expected dial error with external Origin, got nil")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected HTTP 403, got resp: %+v", resp)
		}
	}
}

func TestManager_LiveConPTYAndWebSocket(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Live ConPTY tests only run on Windows")
	}

	tempDir := t.TempDir()
	mgr := NewManager()
	defer mgr.CloseAll()

	srv := NewServer(mgr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wsURL, token, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(ctx)

	// 1. Open a real shell session
	sess, err := mgr.Open("thread-live", tempDir, "Live PTY Test", 100, 28)
	if err != nil {
		t.Fatalf("failed to open terminal session: %v", err)
	}
	defer mgr.Close(sess.ID)

	// Verify List contains the session
	list := mgr.List("thread-live")
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Fatalf("expected 1 session in thread-live, got %+v", list)
	}

	// 2. Connect via gorilla/websocket
	connURL := fmt.Sprintf("%s?id=%s&token=%s", wsURL, sess.ID, token)
	headers := http.Header{
		"Origin": []string{"http://wails.localhost"},
	}
	conn, resp, err := websocket.DefaultDialer.Dial(connURL, headers)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v (resp: %+v)", err, resp)
	}
	defer conn.Close()

	// 3. Send resize message (120x32)
	resizePayload, _ := json.Marshal(map[string]any{
		"type": "resize",
		"cols": 120,
		"rows": 32,
	})
	if err := conn.WriteMessage(websocket.TextMessage, resizePayload); err != nil {
		t.Fatalf("failed to send resize: %v", err)
	}

	// Give a small moment for resize to take effect and verify metadata
	time.Sleep(100 * time.Millisecond)
	updated, ok := mgr.Get(sess.ID)
	if !ok || updated.Cols != 120 || updated.Rows != 32 {
		t.Fatalf("expected resized session 120x32, got %+v", updated)
	}

	// 4. Send echo ORCTEST_OK_123\r\n
	testMarker := "ORCTEST_OK_123"
	cmdInput := fmt.Sprintf("echo %s\r\n", testMarker)
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(cmdInput)); err != nil {
		t.Fatalf("failed to write command: %v", err)
	}

	// Read binary output frames until testMarker is seen
	deadline := time.Now().Add(15 * time.Second)
	var accumulated bytes.Buffer
	foundMarker := false

	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage {
			accumulated.Write(data)
			if strings.Contains(accumulated.String(), testMarker) {
				foundMarker = true
				break
			}
		}
	}

	if !foundMarker {
		t.Fatalf("did not receive expected marker %q in terminal output: %s", testMarker, accumulated.String())
	}

	// 5. Send exit\r\n and verify exit handling
	_ = conn.WriteMessage(websocket.BinaryMessage, []byte("exit\r\n"))

	// Give exit command time to run or close session
	exitDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(exitDeadline) {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			var exitMsg struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			}
			if err := json.Unmarshal(data, &exitMsg); err == nil && exitMsg.Type == "exit" {
				break
			}
		}
	}

	// Close session and verify List removes it
	if err := mgr.Close(sess.ID); err != nil {
		t.Fatalf("failed to close session: %v", err)
	}
	remaining := mgr.List("thread-live")
	if len(remaining) != 0 {
		t.Fatalf("expected 0 remaining sessions after Close, got %+v", remaining)
	}
}

func TestManager_SnapshotReplay(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Live ConPTY tests only run on Windows")
	}

	tempDir := t.TempDir()
	mgr := NewManager()
	defer mgr.CloseAll()

	srv := NewServer(mgr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wsURL, token, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(ctx)

	sess, err := mgr.Open("thread-replay", tempDir, "Replay Test", 80, 24)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer mgr.Close(sess.ID)

	connURL := fmt.Sprintf("%s?id=%s&token=%s", wsURL, sess.ID, token)

	// Client 1 connects and echoes a marker
	conn1, _, err := websocket.DefaultDialer.Dial(connURL, nil)
	if err != nil {
		t.Fatalf("client 1 dial failed: %v", err)
	}

	marker := "REPLAY_MARKER_999"
	_ = conn1.WriteMessage(websocket.BinaryMessage, []byte(fmt.Sprintf("echo %s\r\n", marker)))

	// Read from client 1 until marker is seen
	deadline := time.Now().Add(10 * time.Second)
	var buf1 bytes.Buffer
	for time.Now().Before(deadline) {
		_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		msgType, data, err := conn1.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.BinaryMessage {
			buf1.Write(data)
			if strings.Contains(buf1.String(), marker) {
				break
			}
		}
	}
	conn1.Close()

	if !strings.Contains(buf1.String(), marker) {
		t.Fatalf("client 1 did not see marker %q; output was %q", marker, buf1.String())
	}

	// Client 2 connects fresh — initial snapshot must contain the marker
	conn2, _, err := websocket.DefaultDialer.Dial(connURL, nil)
	if err != nil {
		t.Fatalf("client 2 dial failed: %v", err)
	}
	defer conn2.Close()

	_ = conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
	msgType, data, err := conn2.ReadMessage()
	if err != nil {
		t.Fatalf("client 2 read initial snapshot failed: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Fatalf("expected binary message for snapshot, got type %d", msgType)
	}
	if !strings.Contains(string(data), marker) {
		t.Fatalf("client 2 initial snapshot missing %q: %s", marker, string(data))
	}
}
