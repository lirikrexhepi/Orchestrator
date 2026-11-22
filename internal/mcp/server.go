package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type httpSession struct {
	id        string
	tokenHash string
	lastUsed  time.Time
}

// Server provides a low-RAM Streamable HTTP /mcp server on loopback.
type Server struct {
	backend    Backend
	registry   *Registry
	httpServer *http.Server
	listener   net.Listener
	endpoint   string

	mu       sync.Mutex
	sessions map[string]*httpSession
}

// NewServer initializes an MCP Server with the given backend.
func NewServer(backend Backend) *Server {
	s := &Server{
		backend:  backend,
		sessions: make(map[string]*httpSession),
	}
	s.registry = NewRegistry(func(revokedHashes []string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for id, sess := range s.sessions {
			for _, th := range revokedHashes {
				if sess.tokenHash == th {
					delete(s.sessions, id)
					break
				}
			}
		}
	})
	return s
}

// Registry returns the server's credential registry.
func (s *Server) Registry() *Registry {
	return s.registry
}

// Endpoint returns the loopback URL of the server (valid after Start).
func (s *Server) Endpoint() string {
	return s.endpoint
}

// Start binds to 127.0.0.1:0 and starts serving HTTP /mcp.
func (s *Server) Start(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to bind loopback listener: %w", err)
	}
	s.listener = ln
	port := ln.Addr().(*net.TCPAddr).Port
	s.endpoint = fmt.Sprintf("http://127.0.0.1:%d/mcp", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return s.endpoint, nil
}

// Close shuts down the HTTP server, clears all sessions, and revokes credentials.
func (s *Server) Close(ctx context.Context) error {
	if s.registry != nil {
		s.registry.RevokeAll()
	}
	s.mu.Lock()
	s.sessions = make(map[string]*httpSession)
	s.mu.Unlock()

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) allocateSession(tokenHash string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// Evict idle sessions (>30 min)
	for id, sess := range s.sessions {
		if now.Sub(sess.lastUsed) > 30*time.Minute {
			delete(s.sessions, id)
		}
	}

	// Cap at 256 sessions: evict oldest
	for len(s.sessions) >= 256 {
		var oldestID string
		var oldestTime time.Time
		first := true
		for id, sess := range s.sessions {
			if first || sess.lastUsed.Before(oldestTime) {
				oldestID = id
				oldestTime = sess.lastUsed
				first = false
			}
		}
		if oldestID != "" {
			delete(s.sessions, oldestID)
		} else {
			break
		}
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s.sessions[id] = &httpSession{
		id:        id,
		tokenHash: tokenHash,
		lastUsed:  now,
	}
	return id
}

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeJSONRPC(w http.ResponseWriter, statusCode int, resp jsonrpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	b, _ := json.Marshal(resp)
	_, _ = w.Write(b)
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	// 1. Origin check (before auth)
	if r.Header.Get("Origin") != "" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 2. Bearer auth
	auth := r.Header.Get("Authorization")
	var token string
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token = strings.TrimSpace(auth[7:])
	}
	if token == "" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scope, _, tokenHash, ok := s.registry.Resolve(token)
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 3. DELETE method
	if r.Method == http.MethodDelete {
		sessID := r.Header.Get("Mcp-Session-Id")
		if sessID == "" {
			http.Error(w, "Missing Mcp-Session-Id", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		sess, exists := s.sessions[sessID]
		if !exists || sess.tokenHash != tokenHash {
			s.mu.Unlock()
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		delete(s.sessions, sessID)
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// 4. POST method (1 MB limit)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Request body too large or read error", http.StatusRequestEntityTooLarge)
		return
	}

	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &jsonrpcError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	// Notifications (no id or id is null)
	isNotification := len(req.ID) == 0 || string(bytes.TrimSpace(req.ID)) == "null"
	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Method routing
	if req.Method == "initialize" {
		sessID := s.allocateSession(tokenHash)
		w.Header().Set("Mcp-Session-Id", sessID)

		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &initParams)
		}
		version := "2025-06-18"
		if initParams.ProtocolVersion == "2025-03-26" {
			version = "2025-03-26"
		}
		w.Header().Set("Mcp-Protocol-Version", version)

		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": version,
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "Orchestrator",
					"version": "0.1.0",
				},
			},
		})
		return
	}

	// For other methods, verify Mcp-Session-Id if provided
	sessID := r.Header.Get("Mcp-Session-Id")
	if sessID != "" {
		s.mu.Lock()
		sess, exists := s.sessions[sessID]
		if !exists || sess.tokenHash != tokenHash {
			s.mu.Unlock()
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}
		sess.lastUsed = time.Now()
		s.mu.Unlock()
	}

	switch req.Method {
	case "ping":
		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		})

	case "tools/list":
		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": AllTools()},
		})

	case "tools/call":
		var cp struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &cp); err != nil {
			writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonrpcError{Code: -32602, Message: "Invalid params"},
			})
			return
		}

		if !IsKnownTool(cp.Name) {
			writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonrpcError{Code: -32602, Message: fmt.Sprintf("Unknown tool %q", cp.Name)},
			})
			return
		}

		res := executeTool(r.Context(), s.backend, scope, cp.Name, cp.Arguments)
		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		})

	default:
		writeJSONRPC(w, http.StatusOK, jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonrpcError{Code: -32601, Message: fmt.Sprintf("Method %q not found", req.Method)},
		})
	}
}
