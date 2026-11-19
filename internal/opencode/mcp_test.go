package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"composer/internal/domain"
)

func TestOpenCodeMCPConfigPayloadAndRegistration(t *testing.T) {
	var mcpCalls int32
	var lastReq AddMcpRequest
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Session{ID: "ses-mcp-1"})
		case "/mcp":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			atomic.AddInt32(&mcpCalls, 1)
			body, _ := io.ReadAll(r.Body)
			var req AddMcpRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			mu.Lock()
			lastReq = req
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"orchestrator":{"status":"connected"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{ServerURL: ts.URL}, emit)

	mcpCfg := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:40000/mcp",
		AuthHeader: "Bearer token-abc-123",
	}

	tempDir := t.TempDir()
	sess, err := adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-mcp-1",
		Cwd:      tempDir,
		MCP:      mcpCfg,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	if sess.ProviderSessionID != "ses-mcp-1" {
		t.Errorf("sess.ProviderSessionID = %q, want 'ses-mcp-1'", sess.ProviderSessionID)
	}

	if calls := atomic.LoadInt32(&mcpCalls); calls != 1 {
		t.Fatalf("expected 1 POST /mcp call, got %d", calls)
	}

	mu.Lock()
	req := lastReq
	mu.Unlock()

	if req.Name != "orchestrator" {
		t.Errorf("req.Name = %q, want 'orchestrator'", req.Name)
	}
	if req.Config.Type != "remote" {
		t.Errorf("req.Config.Type = %q, want 'remote'", req.Config.Type)
	}
	if req.Config.URL != "http://127.0.0.1:40000/mcp" {
		t.Errorf("req.Config.URL = %q, want 'http://127.0.0.1:40000/mcp'", req.Config.URL)
	}
	if !req.Config.Enabled {
		t.Errorf("req.Config.Enabled = false, want true")
	}
	if req.Config.Headers["Authorization"] != "Bearer token-abc-123" {
		t.Errorf("req.Config.Headers[Authorization] = %q, want 'Bearer token-abc-123'", req.Config.Headers["Authorization"])
	}
	if req.Config.OAuth {
		t.Errorf("req.Config.OAuth = true, want false")
	}
}

func TestOpenCodeMCPZeroDuplicateRegistration(t *testing.T) {
	var mcpCalls int32
	var mu sync.Mutex
	var tokens []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Session{ID: "ses-dup-" + r.URL.Query().Get("id")})
		case "/mcp":
			if r.Method == http.MethodPost {
				atomic.AddInt32(&mcpCalls, 1)
				body, _ := io.ReadAll(r.Body)
				var req AddMcpRequest
				_ = json.Unmarshal(body, &req)
				mu.Lock()
				tokens = append(tokens, req.Config.Headers["Authorization"])
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"orchestrator":{"status":"connected"}}`))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{ServerURL: ts.URL}, emit)
	tempDir := t.TempDir()

	mcpCfg1 := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:40000/mcp",
		AuthHeader: "Bearer token-initial",
	}

	// 1. First session registers MCP
	_, err := adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-1",
		Cwd:      tempDir,
		MCP:      mcpCfg1,
	})
	if err != nil {
		t.Fatalf("first StartSession failed: %v", err)
	}
	if calls := atomic.LoadInt32(&mcpCalls); calls != 1 {
		t.Fatalf("expected 1 mcp call after first session, got %d", calls)
	}

	// 2. Second session in same project with same MCP config must NOT re-register (zero duplicates)
	_, err = adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-2",
		Cwd:      tempDir,
		MCP:      mcpCfg1,
	})
	if err != nil {
		t.Fatalf("second StartSession failed: %v", err)
	}
	if calls := atomic.LoadInt32(&mcpCalls); calls != 1 {
		t.Fatalf("expected still 1 mcp call (no duplicates), got %d", calls)
	}

	// 3. Third session with updated token triggers re-registration
	mcpCfg2 := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:40000/mcp",
		AuthHeader: "Bearer token-updated",
	}
	_, err = adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-3",
		Cwd:      tempDir,
		MCP:      mcpCfg2,
	})
	if err != nil {
		t.Fatalf("third StartSession failed: %v", err)
	}
	if calls := atomic.LoadInt32(&mcpCalls); calls != 2 {
		t.Fatalf("expected 2 mcp calls after token update, got %d", calls)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(tokens) != 2 || tokens[0] != "Bearer token-initial" || tokens[1] != "Bearer token-updated" {
		t.Errorf("unexpected tokens recorded: %+v", tokens)
	}
}

func TestOpenCodeMCPPlanOnlySkipped(t *testing.T) {
	var mcpCalls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Session{ID: "ses-plan"})
		case "/mcp":
			atomic.AddInt32(&mcpCalls, 1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{ServerURL: ts.URL}, emit)

	mcpCfg := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:40000/mcp",
		AuthHeader: "Bearer token-abc",
	}

	_, err := adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-plan",
		Cwd:      t.TempDir(),
		PlanOnly: true,
		MCP:      mcpCfg,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	if calls := atomic.LoadInt32(&mcpCalls); calls != 0 {
		t.Fatalf("expected 0 mcp calls for PlanOnly, got %d", calls)
	}
}

func TestOpenCodeMCPRegistrationFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Session{ID: "ses-fail"})
		case "/mcp":
			http.Error(w, "internal server error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{ServerURL: ts.URL}, emit)

	mcpCfg := &domain.MCPConfig{
		ServerName: "orchestrator",
		Endpoint:   "http://127.0.0.1:40000/mcp",
		AuthHeader: "Bearer token-abc",
	}

	_, err := adapter.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "thread-fail",
		Cwd:      t.TempDir(),
		MCP:      mcpCfg,
	})
	if err == nil {
		t.Fatalf("expected StartSession to fail when MCP registration fails, got nil")
	}
}
