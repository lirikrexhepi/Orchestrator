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
	Type string `json:"type"`

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
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	token := r.URL.Query().Get("token")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
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
		http.Error(w, "Tab not found", http.StatusNotFound)
		return
	}
	defer cancel()

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	const writeWait = 5 * time.Second

	// Send initial state
	initialStatePayload, _ := json.Marshal(map[string]any{
		"type":  "state",
		"state": snapshot,
	})
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := conn.WriteMessage(websocket.TextMessage, initialStatePayload); err != nil {
		return
	}

	// Send initial frame if available
	if len(lastFrame) > 0 {
		conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.BinaryMessage, lastFrame); err != nil {
			return
		}
	}

	done := make(chan struct{})

	// Writer pump: forwards binary JPEG frames and JSON state changes
	go func() {
		defer close(done)
		for ev := range eventCh {
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if ev.isBinary {
				if err := conn.WriteMessage(websocket.BinaryMessage, ev.binary); err != nil {
					return
				}
			} else if ev.err != "" {
				errPayload, _ := json.Marshal(map[string]any{
					"type":    "error",
					"message": ev.err,
				})
				if err := conn.WriteMessage(websocket.TextMessage, errPayload); err != nil {
					return
				}
			} else {
				statePayload, _ := json.Marshal(map[string]any{
					"type":  "state",
					"state": ev.state,
				})
				if err := conn.WriteMessage(websocket.TextMessage, statePayload); err != nil {
					return
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

		ctx, cmdCancel := context.WithTimeout(context.Background(), 10*time.Second)
		switch cmd.Type {
		case "navigate":
			_ = s.mgr.Navigate(ctx, tabID, cmd.URL)
		case "back":
			_ = s.mgr.Back(ctx, tabID)
		case "forward":
			_ = s.mgr.Forward(ctx, tabID)
		case "reload":
			_ = s.mgr.Reload(ctx, tabID)
		case "resize":
			_ = s.mgr.Resize(ctx, tabID, cmd.Width, cmd.Height, cmd.DPR)
		case "pause":
			_ = s.mgr.SetPaused(ctx, tabID, cmd.Paused)
		case "mouse":
			mType := cmd.MouseType
			if mType == "" {
				mType = "mouseMoved"
			}
			_ = s.mgr.DispatchMouse(ctx, tabID, MouseEvent{
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
			_ = s.mgr.DispatchKey(ctx, tabID, KeyEvent{
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
			text := cmd.Text
			if text != "" {
				_ = s.mgr.InsertText(ctx, tabID, text)
			}
		}
		cmdCancel()
	}

	<-done
}
