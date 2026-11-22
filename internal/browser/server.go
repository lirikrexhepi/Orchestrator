package browser

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"composer/internal/logger"
	"github.com/gorilla/websocket"
)

// Server provides a secure loopback WebSocket endpoint for streaming CDP browser sessions.
type Server struct {
	mgr      *Manager
	token    string
	wsURL    string
	listener net.Listener
	httpSrv  *http.Server
	upgrader websocket.Upgrader
	mu       sync.RWMutex
}

// NewServer initializes a browser streaming server with a random 32-byte secret token.
func NewServer(mgr *Manager) *Server {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	s := &Server{
		mgr:   mgr,
		token: token,
	}

	s.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			u, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return isAllowedOrigin(u.Hostname())
		},
	}

	return s
}

func isAllowedOrigin(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "wails.localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return false
}

// Start binds to 127.0.0.1:0 and starts serving WebSockets.
func (s *Server) Start(ctx context.Context) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.wsURL, s.token, nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("failed to bind browser server: %w", err)
	}
	s.listener = ln
	port := ln.Addr().(*net.TCPAddr).Port
	s.wsURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.httpSrv = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// server closed
		}
	}()

	return s.wsURL, s.token, nil
}

// Endpoint returns the current WebSocket URL and secret token.
func (s *Server) Endpoint() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wsURL, s.token
}

// Close gracefully stops the HTTP server.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpSrv == nil {
		return nil
	}
	err := s.httpSrv.Shutdown(ctx)
	s.httpSrv = nil
	s.listener = nil
	return err
}

type inboundMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`

	// navigate
	URL string `json:"url,omitempty"`

	// resize
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	DPR    float64 `json:"dpr,omitempty"`

	// pause
	Paused bool `json:"paused,omitempty"`

	// mouse
	MouseType  string  `json:"mouseType,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`

	// key
	KeyType               string `json:"keyType,omitempty"`
	Key                   string `json:"key,omitempty"`
	Code                  string `json:"code,omitempty"`
	Text                  string `json:"text,omitempty"`
	UnmodifiedText        string `json:"unmodifiedText,omitempty"`
	WindowsVirtualKeyCode int    `json:"windowsVirtualKeyCode,omitempty"`
	NativeVirtualKeyCode  int    `json:"nativeVirtualKeyCode,omitempty"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !isAllowedOrigin(u.Hostname()) {
			logger.Warnf("BrowserServer", "Rejected WebSocket origin=%q", origin)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	token := r.URL.Query().Get("token")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		logger.Warnf("BrowserServer", "Rejected WebSocket unauthorized token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	tabID := r.URL.Query().Get("id")
	if tabID == "" {
		http.Error(w, "Missing tab id", http.StatusBadRequest)
		return
	}

	snapshot, lastFrame, eventCh, cancel, err := s.mgr.Subscribe(tabID)
	if err != nil {
		logger.Warnf("BrowserServer", "Subscribe failed for tab=%s: %v", tabID, err)
		http.Error(w, "Tab not found", http.StatusNotFound)
		return
	}
	defer cancel()

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warnf("BrowserServer", "WebSocket upgrade failed for tab=%s: %v", tabID, err)
		return
	}
	defer conn.Close()

	const writeWait = 5 * time.Second
	var writeMu sync.Mutex
	writeSafe := func(msgType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		return conn.WriteMessage(msgType, payload)
	}

	// Send initial state
	initialStatePayload, _ := json.Marshal(map[string]any{
		"type":  "state",
		"state": snapshot,
	})
	if err := writeSafe(websocket.TextMessage, initialStatePayload); err != nil {
		return
	}

	// Send initial frame if available
	if len(lastFrame) > 0 {
		if err := writeSafe(websocket.BinaryMessage, lastFrame); err != nil {
			return
		}
	}

	done := make(chan struct{})
	readerDone := make(chan struct{})

	// Writer pump: forwards binary JPEG frames and JSON state changes
	go func() {
		defer close(done)
		defer conn.Close()
		for {
			select {
			case <-readerDone:
				return
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				if ev.isBinary {
					if err := writeSafe(websocket.BinaryMessage, ev.binary); err != nil {
						return
					}
				} else if ev.err != "" {
					errPayload, _ := json.Marshal(map[string]any{
						"type":    "error",
						"message": ev.err,
					})
					if err := writeSafe(websocket.TextMessage, errPayload); err != nil {
						return
					}
				} else {
					statePayload, _ := json.Marshal(map[string]any{
						"type":  "state",
						"state": ev.state,
					})
					if err := writeSafe(websocket.TextMessage, statePayload); err != nil {
						return
					}
				}
			}
		}
	}()

	// Reader pump: handles client commands
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var cmd inboundMessage
		if err := json.Unmarshal(msg, &cmd); err != nil {
			continue
		}

		if cmd.Type == "capture" {
			reqID := cmd.RequestID
			go func() {
				capCtx, capCancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer capCancel()
				data, err := s.mgr.CaptureScreenshot(capCtx, tabID)
				resp := map[string]any{
					"type":      "screenshot",
					"requestId": reqID,
				}
				if err != nil {
					resp["error"] = err.Error()
				} else {
					resp["data"] = data
				}
				payload, _ := json.Marshal(resp)
				select {
				case <-done:
					return
				case <-readerDone:
					return
				default:
					_ = writeSafe(websocket.TextMessage, payload)
				}
			}()
			continue
		}

		switch cmd.Type {
		case "mouse":
			mType := cmd.MouseType
			if mType == "" {
				mType = "mouseMoved"
			}
			_ = s.mgr.DispatchMouse(context.Background(), tabID, MouseEvent{
				Type:       mType,
				X:          cmd.X,
				Y:          cmd.Y,
				Button:     cmd.Button,
				Buttons:    cmd.Buttons,
				ClickCount: cmd.ClickCount,
				DeltaX:     cmd.DeltaX,
				DeltaY:     cmd.DeltaY,
				Modifiers:  cmd.Modifiers,
			})
		case "key":
			kType := cmd.KeyType
			if kType == "" {
				kType = "keyDown"
			}
			_ = s.mgr.DispatchKey(context.Background(), tabID, KeyEvent{
				Type:                  kType,
				Key:                   cmd.Key,
				Code:                  cmd.Code,
				Text:                  cmd.Text,
				UnmodifiedText:        cmd.UnmodifiedText,
				WindowsVirtualKeyCode: cmd.WindowsVirtualKeyCode,
				NativeVirtualKeyCode:  cmd.NativeVirtualKeyCode,
				Modifiers:             cmd.Modifiers,
			})
		case "paste":
			if cmd.Text != "" {
				_ = s.mgr.InsertText(context.Background(), tabID, cmd.Text)
			}
		case "navigate", "back", "forward", "reload", "resize", "pause":
			// Run potentially slow CDP lifecycle/navigation RPCs asynchronously
			// so they never stall incoming mouse/wheel/keyboard events.
			c := cmd
			go func() {
				ctx, cmdCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cmdCancel()
				switch c.Type {
				case "navigate":
					if navErr := s.mgr.Navigate(ctx, tabID, c.URL); navErr != nil {
						errPayload, _ := json.Marshal(map[string]any{
							"type":    "error",
							"message": navErr.Error(),
						})
						_ = writeSafe(websocket.TextMessage, errPayload)
					}
				case "back":
					_ = s.mgr.Back(ctx, tabID)
				case "forward":
					_ = s.mgr.Forward(ctx, tabID)
				case "reload":
					_ = s.mgr.Reload(ctx, tabID)
				case "resize":
					_ = s.mgr.Resize(ctx, tabID, c.Width, c.Height, c.DPR)
				case "pause":
					_ = s.mgr.SetPaused(ctx, tabID, c.Paused)
				}
			}()
		}
	}

	close(readerDone)
	<-done
}
