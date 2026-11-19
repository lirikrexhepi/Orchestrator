package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"composer/internal/domain"
)

type mockBackend struct {
	providers    []ProviderInfo
	threads      map[string]ThreadSummary
	delegateFunc func(req DelegateRequest) (DelegateResult, error)
	readFunc     func(threadID, view string, limit, maxChars int) (ThreadReadResult, error)
	sendFunc     func(callerID, targetID, message string) error
	stopFunc     func(callerID, targetID string, interruptOnly bool) error
}

func (m *mockBackend) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	return m.providers, nil
}
func (m *mockBackend) ListThreads(ctx context.Context, limit int) ([]ThreadSummary, error) {
	var list []ThreadSummary
	for _, t := range m.threads {
		list = append(list, t)
		if len(list) >= limit {
			break
		}
	}
	return list, nil
}
func (m *mockBackend) GetThread(ctx context.Context, threadID string) (ThreadSummary, bool, error) {
	t, ok := m.threads[threadID]
	return t, ok, nil
}
func (m *mockBackend) DelegateTask(ctx context.Context, req DelegateRequest) (DelegateResult, error) {
	if m.delegateFunc != nil {
		return m.delegateFunc(req)
	}
	return DelegateResult{ThreadID: "t-child", Title: req.Title, Driver: string(req.Driver), Status: "running"}, nil
}
func (m *mockBackend) ReadThread(ctx context.Context, threadID string, view string, limit int, maxChars int) (ThreadReadResult, error) {
	if m.readFunc != nil {
		return m.readFunc(threadID, view, limit, maxChars)
	}
	return ThreadReadResult{
		Thread: ThreadSummary{ThreadID: threadID},
		Items:  []ThreadReadItem{},
	}, nil
}
func (m *mockBackend) SendToThread(ctx context.Context, callerID, targetID, message string) error {
	if m.sendFunc != nil {
		return m.sendFunc(callerID, targetID, message)
	}
	return nil
}
func (m *mockBackend) StopThread(ctx context.Context, callerID, targetID string, interruptOnly bool) error {
	if m.stopFunc != nil {
		return m.stopFunc(callerID, targetID, interruptOnly)
	}
	return nil
}

func TestTokenBudgetsAndSchemas(t *testing.T) {
	tools := AllTools()
	if len(tools) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(tools))
	}

	for _, tool := range tools {
		b, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("failed to marshal tool %s: %v", tool.Name, err)
		}
		if len(b) >= 500 {
			t.Errorf("tool %s exceeds 500 bytes budget: got %d bytes", tool.Name, len(b))
		}

		// Ensure all enums are non-empty strings and no non-string types
		for propName, prop := range tool.InputSchema.Properties {
			for _, enumVal := range prop.Enum {
				if enumVal == "" {
					t.Errorf("tool %s prop %s has empty enum value", tool.Name, propName)
				}
			}
		}
	}

	listResult := map[string]any{"tools": tools}
	listBytes, err := json.Marshal(listResult)
	if err != nil {
		t.Fatalf("failed to marshal tools/list: %v", err)
	}
	t.Logf("tools/list exact serialized byte count: %d bytes", len(listBytes))

	if len(listBytes) >= 2400 {
		t.Errorf("tools/list exceeds 2,400 bytes budget: got %d bytes", len(listBytes))
	}
}

func TestAuthAndSecurity(t *testing.T) {
	backend := &mockBackend{}
	srv := NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(context.Background())

	scope := CallerScope{
		ThreadID:   "th-1",
		Driver:     domain.DriverClaude,
		Cwd:        "/workspace",
		Permission: domain.PermissionDefault,
	}
	cred, err := srv.Registry().Issue(endpoint, scope)
	if err != nil {
		t.Fatalf("failed to issue credential: %v", err)
	}

	client := &http.Client{}

	// 1. Missing Authorization header -> 401
	{
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got %s", resp.Header.Get("Cache-Control"))
		}
		if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "invalid_token") {
			t.Errorf("expected WWW-Authenticate header, got %s", resp.Header.Get("WWW-Authenticate"))
		}
	}

	// 2. Bad token -> 401
	{
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer bad-token-xyz")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	}

	// 3. Origin header present -> 403 Forbidden
	{
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		req.Header.Set("Origin", "http://attacker.example.com")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	}

	// 4. Initialize session -> 200 OK + Mcp-Session-Id header
	var sessionID string
	{
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", resp.StatusCode)
		}
		sessionID = resp.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			t.Fatalf("expected Mcp-Session-Id header")
		}
	}

	// 5. DELETE /mcp with sessionID -> 204 No Content
	{
		req, _ := http.NewRequest(http.MethodDelete, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		req.Header.Set("Mcp-Session-Id", sessionID)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("expected 204 No Content, got %d", resp.StatusCode)
		}
	}

	// 6. DELETE /mcp again -> 404 Not Found
	{
		req, _ := http.NewRequest(http.MethodDelete, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		req.Header.Set("Mcp-Session-Id", sessionID)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 Not Found, got %d", resp.StatusCode)
		}
	}

	// 7. Oversized body (>1 MB rejected)
	{
		oversized := make([]byte, (1<<20)+1024)
		req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(oversized))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413 Request Entity Too Large, got %d", resp.StatusCode)
		}
	}

	// 8. Revoking thread cascades to HTTP sessions (401)
	{
		cred2, _ := srv.Registry().Issue(endpoint, CallerScope{ThreadID: "th-cascade", Permission: domain.PermissionDefault})
		// Initialize session for cred2
		req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred2.Token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		s2ID := resp.Header.Get("Mcp-Session-Id")
		resp.Body.Close()

		if s2ID == "" {
			t.Fatalf("expected session ID for cred2")
		}

		// Verify session is active
		srv.mu.Lock()
		_, existsBefore := srv.sessions[s2ID]
		srv.mu.Unlock()
		if !existsBefore {
			t.Fatalf("session should exist before revoke")
		}

		// Revoke thread
		srv.Registry().RevokeThread("th-cascade")

		// Verify session was cascaded and removed
		srv.mu.Lock()
		_, existsAfter := srv.sessions[s2ID]
		srv.mu.Unlock()
		if existsAfter {
			t.Errorf("expected session to be cascade-deleted after thread revocation")
		}

		// Request with revoked token returns 401
		req2, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+cred2.Token)
		resp2, err := client.Do(req2)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 after revocation, got %d", resp2.StatusCode)
		}
	}
}

func TestToolsCallContentAndOmission(t *testing.T) {
	backend := &mockBackend{
		providers: []ProviderInfo{{Driver: "claude", Models: []string{"claude-3-5-sonnet"}}},
	}
	srv := NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(context.Background())

	cred, _ := srv.Registry().Issue(endpoint, CallerScope{
		ThreadID:   "th-main",
		Driver:     domain.DriverClaude,
		Cwd:        "/repo",
		Permission: domain.PermissionDefault,
	})

	client := &http.Client{}

	// Successful tool call
	callPayload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"orchestrator_capabilities","arguments":{}}}`
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(callPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Verify structuredContent is NOT present
	if strings.Contains(bodyStr, "structuredContent") {
		t.Errorf("found duplicate structuredContent field: %s", bodyStr)
	}
	// Verify isError is omitted when false
	if strings.Contains(bodyStr, "isError") {
		t.Errorf("isError should be omitted when false: %s", bodyStr)
	}
	// Verify content text is present
	if !strings.Contains(bodyStr, `"content":[{"type":"text"`) {
		t.Errorf("expected content: [{ type: text, text }]: %s", bodyStr)
	}

	// Tool call error (unknown argument) -> isError MUST be true
	errPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"orchestrator_capabilities","arguments":{"invalidKey":"val"}}}`
	req2, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(errPayload))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+cred.Token)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	body2, _ := io.ReadAll(resp2.Body)
	bodyStr2 := string(body2)

	if !strings.Contains(bodyStr2, `"isError":true`) {
		t.Errorf("expected isError: true on tool error: %s", bodyStr2)
	}
	if !strings.Contains(bodyStr2, "unknown argument") {
		t.Errorf("expected error message mentioning unknown argument: %s", bodyStr2)
	}
}

func TestPermissionFencing(t *testing.T) {
	backend := &mockBackend{
		threads: map[string]ThreadSummary{
			"th-high": {
				ThreadID:   "th-high",
				Permission: domain.PermissionBypass,
			},
			"th-low": {
				ThreadID:   "th-low",
				Permission: domain.PermissionPlan,
			},
		},
	}
	srv := NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(context.Background())

	// Caller with PermissionDefault (rank 1)
	cred, _ := srv.Registry().Issue(endpoint, CallerScope{
		ThreadID:   "th-caller",
		Driver:     domain.DriverClaude,
		Cwd:        "/app",
		Permission: domain.PermissionDefault,
	})

	client := &http.Client{}

	callTool := func(name string, args map[string]any) (CallToolResult, error) {
		argsBytes, _ := json.Marshal(args)
		payload := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": json.RawMessage(argsBytes),
			},
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		resp, err := client.Do(req)
		if err != nil {
			return CallToolResult{}, err
		}
		defer resp.Body.Close()
		var r struct {
			Result CallToolResult `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return CallToolResult{}, err
		}
		return r.Result, nil
	}

	// 1. Delegate task: escalate to bypassPermissions (rank 3 > caller rank 1) -> denied
	res, err := callTool("delegate_task", map[string]any{
		"title":          "Child",
		"task":           "Do work",
		"permissionMode": "bypassPermissions",
	})
	if err != nil {
		t.Fatalf("delegate_task failed: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "escalation denied") {
		t.Errorf("expected escalation denied error, got: %+v", res)
	}

	// 2. Delegate task: requested permissionPlan (rank 0 <= rank 1) -> allowed
	res, err = callTool("delegate_task", map[string]any{
		"title":          "Child",
		"task":           "Do work",
		"permissionMode": "plan",
	})
	if err != nil {
		t.Fatalf("delegate_task failed: %v", err)
	}
	if res.IsError {
		t.Errorf("expected delegate_task allowed, got error: %+v", res)
	}

	// 3. Send to target with higher permission (th-high: rank 3 > caller rank 1) -> denied
	res, err = callTool("send_to_thread", map[string]any{
		"threadId": "th-high",
		"message":  "hello",
	})
	if err != nil {
		t.Fatalf("send_to_thread failed: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "permission denied") {
		t.Errorf("expected permission denied for higher permission target, got: %+v", res)
	}

	// 4. Send to target with lower permission (th-low: rank 0 <= rank 1) -> allowed
	res, err = callTool("send_to_thread", map[string]any{
		"threadId": "th-low",
		"message":  "hello",
	})
	if err != nil {
		t.Fatalf("send_to_thread failed: %v", err)
	}
	if res.IsError {
		t.Errorf("expected send_to_thread allowed, got error: %+v", res)
	}

	// 5. Send to self thread -> denied
	res, err = callTool("send_to_thread", map[string]any{
		"threadId": "th-caller",
		"message":  "self message",
	})
	if err != nil {
		t.Fatalf("send_to_thread failed: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "caller's own thread") {
		t.Errorf("expected self-thread denied, got: %+v", res)
	}

	// 6. Stop target with higher permission -> denied
	res, err = callTool("stop_thread", map[string]any{
		"threadId": "th-high",
	})
	if err != nil {
		t.Fatalf("stop_thread failed: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "permission denied") {
		t.Errorf("expected permission denied for stop_thread, got: %+v", res)
	}

	// 7. Stop self thread -> denied
	res, err = callTool("stop_thread", map[string]any{
		"threadId": "th-caller",
	})
	if err != nil {
		t.Fatalf("stop_thread failed: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "caller's own thread") {
		t.Errorf("expected self-stop denied, got: %+v", res)
	}
}

func TestReadThreadDefaultsAndSafetyNet(t *testing.T) {
	// 10,000 runes text including multi-byte UTF-8 emojis and characters
	sampleRune := "🚀 Hello 世界! "
	var sb strings.Builder
	for sb.Len() < 10000 {
		sb.WriteString(sampleRune)
	}
	bigText := sb.String()

	var recordedView string
	backend := &mockBackend{
		readFunc: func(threadID, view string, limit, maxChars int) (ThreadReadResult, error) {
			recordedView = view
			return ThreadReadResult{
				Thread: ThreadSummary{ThreadID: threadID},
				Items: []ThreadReadItem{
					{Index: 0, Kind: "user", Text: bigText},
					{Index: 1, Kind: "assistant", Text: "second item"},
				},
			}, nil
		},
	}

	srv := NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Close(context.Background())

	cred, _ := srv.Registry().Issue(endpoint, CallerScope{ThreadID: "th-read", Permission: domain.PermissionDefault})

	client := &http.Client{}
	callPayload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_thread","arguments":{"threadId":"th-target","maxChars":800}}}`
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(callPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var r struct {
		Result CallToolResult `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)

	// Verify view default was "messages"
	if recordedView != "messages" {
		t.Errorf("expected default view 'messages', got %q", recordedView)
	}

	var readRes ThreadReadResult
	if err := json.Unmarshal([]byte(r.Result.Content[0].Text), &readRes); err != nil {
		t.Fatalf("failed to parse read_thread result: %v", err)
	}

	if !readRes.Truncated {
		t.Errorf("expected Truncated: true")
	}

	totalRunes := 0
	for _, item := range readRes.Items {
		if !utf8.ValidString(item.Text) {
			t.Errorf("trimmed text is not valid UTF-8: %s", item.Text)
		}
		totalRunes += utf8.RuneCountInString(item.Text)
	}

	if totalRunes > 800 {
		t.Errorf("expected total runes <= 800, got %d", totalRunes)
	}
}

func TestSessionCapAndOldestEviction(t *testing.T) {
	backend := &mockBackend{}
	srv := NewServer(backend)

	// Allocate 260 sessions
	for i := 0; i < 260; i++ {
		srv.allocateSession("hash-test")
	}

	srv.mu.Lock()
	count := len(srv.sessions)
	srv.mu.Unlock()

	if count > 256 {
		t.Errorf("expected session count capped at <= 256, got %d", count)
	}
}
