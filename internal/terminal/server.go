package terminal

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

// Server provides a secure loopback HTTP and WebSocket endpoint for streaming terminal sessions.
type Server struct {
	mgr      *Manager
	token    string
	wsURL    string
	listener net.Listener
	httpSrv  *http.Server
	upgrader websocket.Upgrader
	mu       sync.RWMutex
}

// NewServer initializes a terminal server with a random 32-byte secret token.
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
		return "", "", fmt.Errorf("failed to bind terminal server: %w", err)
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

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	snapshot, eventCh, cancel, info, err := s.mgr.Subscribe(sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	defer cancel()

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	const writeWait = 5 * time.Second

	// Immediately send the ring buffer snapshot (if non-empty) as a BinaryMessage
	if len(snapshot) > 0 {
		conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.BinaryMessage, snapshot); err != nil {
			return
		}
	}

	// If session has already exited, send a JSON TextMessage {"type":"exit","code":<n>}
	if !info.Running && info.ExitCode != nil {
		exitPayload, _ := json.Marshal(map[string]any{
			"type": "exit",
			"code": *info.ExitCode,
		})
		conn.SetWriteDeadline(time.Now().Add(writeWait))
		_ = conn.WriteMessage(websocket.TextMessage, exitPayload)
	}

	done := make(chan struct{})

	// Writer loop forwards live PTY output as BinaryMessage and exit events as TextMessage
	go func() {
		defer conn.Close()
		defer cancel()

		for {
			select {
			case <-done:
				return
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				switch ev.Type {
				case "output":
					if err := conn.WriteMessage(websocket.BinaryMessage, ev.Data); err != nil {
						return
					}
				case "exit":
					exitPayload, _ := json.Marshal(map[string]any{
						"type": "exit",
						"code": ev.ExitCode,
					})
					_ = conn.WriteMessage(websocket.TextMessage, exitPayload)
					return
				}
			}
		}
	}()

	defer close(done)

	// Reader loop handles client frames
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch msgType {
		case websocket.BinaryMessage:
			_ = s.mgr.Write(sessionID, data)
		case websocket.TextMessage:
			var env struct {
				Type string `json:"type"`
				Data string `json:"data"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if err := json.Unmarshal(data, &env); err == nil {
				switch env.Type {
				case "input":
					if env.Data != "" {
						_ = s.mgr.Write(sessionID, []byte(env.Data))
					}
				case "resize":
					_ = s.mgr.Resize(sessionID, env.Cols, env.Rows)
				}
			}
		}
	}
}
