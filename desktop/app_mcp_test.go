package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/mcp"
	"composer/internal/provider"
	"composer/internal/session"
)

type testFakeDriver struct {
	kind    domain.DriverKind
	adapter *testFakeAdapter
}

func (d *testFakeDriver) Kind() domain.DriverKind { return d.kind }
func (d *testFakeDriver) DisplayName() string     { return string(d.kind) }
func (d *testFakeDriver) DefaultSettings() domain.ProviderSettings {
	return domain.ProviderSettings{}
}
func (d *testFakeDriver) Command() string { return "fake" }
func (d *testFakeDriver) Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot {
	return domain.ProviderSnapshot{
		Driver:       d.kind,
		Availability: domain.AvailabilityReady,
		Models:       []domain.Model{{ID: "fake-model-1", Default: true}},
	}
}
func (d *testFakeDriver) NewAdapter(settings domain.ProviderSettings, emit provider.Emitter) (provider.Adapter, error) {
	if d.adapter != nil {
		d.adapter.emit = emit
	}
	return d.adapter, nil
}
func (d *testFakeDriver) Binary(settings domain.ProviderSettings) string { return "fake" }
func (d *testFakeDriver) AccountEnv(dir string) map[string]string        { return nil }
func (d *testFakeDriver) AccountUnset() []string                         { return nil }
func (d *testFakeDriver) DefaultConfigDir(env map[string]string) string  { return "" }
func (d *testFakeDriver) SharedSettings() []string                       { return nil }
func (d *testFakeDriver) SignInArgs() []string                           { return nil }
func (d *testFakeDriver) StatusArgs() []string                           { return nil }
func (d *testFakeDriver) ParseStatus(result provider.CommandResult) provider.AccountStatus {
	return provider.AccountStatus{}
}

type testFakeAdapter struct {
	mu       sync.Mutex
	emit     provider.Emitter
	started  []domain.SessionStartInput
	sessions map[string]domain.SessionStartInput
}

func (a *testFakeAdapter) Driver() domain.DriverKind           { return domain.DriverClaude }
func (a *testFakeAdapter) Capabilities() provider.Capabilities { return provider.Capabilities{} }
func (a *testFakeAdapter) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	a.mu.Lock()
	a.started = append(a.started, in)
	if a.sessions == nil {
		a.sessions = make(map[string]domain.SessionStartInput)
	}
	a.sessions[in.ThreadID] = in
	a.mu.Unlock()
	return domain.Session{ThreadID: in.ThreadID, Driver: domain.DriverClaude, Cwd: in.Cwd, Model: in.Model}, nil
}
func (a *testFakeAdapter) SendTurn(ctx context.Context, in domain.SendTurnInput) error { return nil }
func (a *testFakeAdapter) InterruptTurn(ctx context.Context, threadID string) error    { return nil }
func (a *testFakeAdapter) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	return nil
}
func (a *testFakeAdapter) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	return nil
}
func (a *testFakeAdapter) StopSession(ctx context.Context, threadID string) error {
	a.mu.Lock()
	delete(a.sessions, threadID)
	a.mu.Unlock()
	return nil
}
func (a *testFakeAdapter) StopAll(ctx context.Context) error { return nil }
func (a *testFakeAdapter) HasSession(threadID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.sessions[threadID]
	return ok
}

func TestReadThreadCollapsingAndViews(t *testing.T) {
	app := &App{
		workspaces: session.NewWorkspaces(),
	}
	backend := &appMCPBackend{app: app}

	ws := app.workspaces.Create("Workspace 1", "prompt", "/tmp/repo")
	task, ok := app.workspaces.AddTask(ws.ID, domain.Task{
		ThreadID:   "thread-read-test",
		Title:      "Test Task",
		Driver:     domain.DriverClaude,
		Permission: domain.PermissionDefault,
	})
	if !ok {
		t.Fatalf("failed to add task")
	}

	events := []domain.RuntimeEvent{
		{
			Kind:     domain.EventUserMessage,
			ThreadID: task.ThreadID,
			Text:     "Initial prompt",
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "I will ",
			Delta:    true,
			ItemID:   "msg-1",
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "check the repo.",
			Delta:    true,
			ItemID:   "msg-1",
		},
		{
			Kind:     domain.EventToolCall,
			ThreadID: task.ThreadID,
			Tool: &domain.ToolCall{
				ID:     "tool-1",
				Name:   "bash",
				Target: "ls",
				Status: domain.ToolPending,
				Input:  map[string]any{"cmd": "ls -la"},
			},
		},
		{
			Kind:     domain.EventToolResult,
			ThreadID: task.ThreadID,
			Tool: &domain.ToolCall{
				ID:     "tool-1",
				Name:   "bash",
				Target: "ls",
				Status: domain.ToolCompleted,
				Output: "file1.go\nfile2.go",
			},
		},
		{
			Kind:     domain.EventAgentMessage,
			ThreadID: task.ThreadID,
			Text:     "Found files.",
		},
		{
			Kind:     domain.EventTurnFailed,
			ThreadID: task.ThreadID,
			Error:    "failed to compile",
		},
	}

	itemsMessages := collapseAndFilterEvents(events, "messages")
	// Expected:
	// 0: user "Initial prompt"
	// 1: assistant "I will check the repo." (collapsed from deltas)
	// 2: assistant "Found files." (or merged assistant)
	// 3: error "failed to compile"
	for _, item := range itemsMessages {
		if item.Kind == "tool" {
			t.Fatalf("expected no tool items in 'messages' view, found: %+v", item)
		}
	}

	// Verify streaming delta collapse
	if !strings.Contains(itemsMessages[1].Text, "I will check the repo.") {
		t.Errorf("expected collapsed assistant message, got %q", itemsMessages[1].Text)
	}

	itemsActivity := collapseAndFilterEvents(events, "activity")
	foundTool := false
	for _, item := range itemsActivity {
		if item.Kind == "tool" {
			foundTool = true
			if item.Tool != "bash" {
				t.Errorf("tool = %q, want 'bash'", item.Tool)
			}
			if item.Status != "completed" {
				t.Errorf("status = %q, want 'completed'", item.Status)
			}
			if item.Text != "file1.go\nfile2.go" {
				t.Errorf("text = %q, want 'file1.go\\nfile2.go'", item.Text)
			}
		}
	}
	if !foundTool {
		t.Fatalf("expected tool item in 'activity' view")
	}

	// Test ReadThread method with limit
	res, err := backend.ReadThread(context.Background(), task.ThreadID, "messages", 2, 2000)
	if err != nil {
		t.Fatalf("ReadThread failed: %v", err)
	}
	_ = res
}

func TestManagerIssueAndRevokeMCP(t *testing.T) {
	adapter := &testFakeAdapter{}
	driver := &testFakeDriver{kind: domain.DriverClaude, adapter: adapter}
	reg := provider.NewRegistry(driver)
	mgr := session.NewManager(reg)

	srv := mcp.NewServer(&appMCPBackend{})
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start MCP server: %v", err)
	}
	defer srv.Close(context.Background())

	mgr.SetMCPProvider(&appMCPProvider{server: srv})

	in := domain.SessionStartInput{
		ThreadID:   "thread-mcp-mgr-1",
		Cwd:        "/test/cwd",
		Permission: domain.PermissionDefault,
	}

	sess, err := mgr.Start(context.Background(), domain.DriverClaude, in)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if sess.ThreadID != "thread-mcp-mgr-1" {
		t.Errorf("sess.ThreadID = %q, want 'thread-mcp-mgr-1'", sess.ThreadID)
	}

	adapter.mu.Lock()
	if len(adapter.started) != 1 {
		t.Fatalf("expected 1 started session, got %d", len(adapter.started))
	}
	startedIn := adapter.started[0]
	adapter.mu.Unlock()

	if startedIn.MCP == nil {
		t.Fatalf("expected started session to have MCP populated")
	}
	if startedIn.MCP.ServerName != "orchestrator" {
		t.Errorf("ServerName = %q, want 'orchestrator'", startedIn.MCP.ServerName)
	}
	if startedIn.MCP.Endpoint != endpoint {
		t.Errorf("Endpoint = %q, want %q", startedIn.MCP.Endpoint, endpoint)
	}
	if !strings.HasPrefix(startedIn.MCP.AuthHeader, "Bearer ") {
		t.Errorf("AuthHeader = %q, want prefix 'Bearer '", startedIn.MCP.AuthHeader)
	}

	rawToken := strings.TrimPrefix(startedIn.MCP.AuthHeader, "Bearer ")
	scope, _, _, found := srv.Registry().Resolve(rawToken)
	if !found {
		t.Fatalf("expected token %q to be resolved in registry", rawToken)
	}
	if scope.ThreadID != "thread-mcp-mgr-1" {
		t.Errorf("scope.ThreadID = %q, want 'thread-mcp-mgr-1'", scope.ThreadID)
	}

	// Stop session -> should revoke credential
	if err := mgr.Stop(context.Background(), "thread-mcp-mgr-1"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	_, _, _, foundAfterStop := srv.Registry().Resolve(rawToken)
	if foundAfterStop {
		t.Fatalf("expected credential to be revoked after Stop")
	}
}

func TestMCPEndToEndHTTPIntegration(t *testing.T) {
	tempDir := t.TempDir()

	adapter := &testFakeAdapter{}
	driver := &testFakeDriver{kind: domain.DriverClaude, adapter: adapter}
	reg := provider.NewRegistry(driver)
	mgr := session.NewManager(reg)
	ws := session.NewWorkspaces()

	app := &App{
		registry:   reg,
		manager:    mgr,
		workspaces: ws,
	}
	backend := &appMCPBackend{app: app}
	srv := mcp.NewServer(backend)
	endpoint, err := srv.Start(context.Background())
	if err != nil {
		t.Fatalf("failed to start MCP server: %v", err)
	}
	defer srv.Close(context.Background())

	parentThreadID := "parent-thread-e2e"
	scope := mcp.CallerScope{
		ThreadID:   parentThreadID,
		Driver:     domain.DriverClaude,
		Cwd:        tempDir,
		Permission: domain.PermissionAcceptEdits,
	}
	cred, err := srv.Registry().Issue(endpoint, scope)
	if err != nil {
		t.Fatalf("failed to issue credential: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Call initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(initReq))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("initialize request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize returned status %d", resp.StatusCode)
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatalf("expected Mcp-Session-Id header from initialize")
	}

	// Send notifications/initialized
	notifReq := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req, err = http.NewRequest(http.MethodPost, endpoint, strings.NewReader(notifReq))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("notifications/initialized request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notifications/initialized returned status %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Helper for tools/call
	callTool := func(id int, name string, args map[string]any) (mcp.CallToolResult, error) {
		argsBytes, _ := json.Marshal(args)
		payload := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      name,
				"arguments": json.RawMessage(argsBytes),
			},
		}
		b, _ := json.Marshal(payload)
		httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+cred.Token)
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
		httpResp, err := client.Do(httpReq)
		if err != nil {
			return mcp.CallToolResult{}, err
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			return mcp.CallToolResult{}, fmt.Errorf("HTTP status %d", httpResp.StatusCode)
		}
		var r struct {
			Result mcp.CallToolResult `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(httpResp.Body).Decode(&r); err != nil {
			return mcp.CallToolResult{}, err
		}
		if r.Error != nil {
			return mcp.CallToolResult{}, fmt.Errorf("rpc error %d: %s", r.Error.Code, r.Error.Message)
		}
		return r.Result, nil
	}

	// 2. Call orchestrator_capabilities — verify caller info and ready providers are returned.
	capRes, err := callTool(2, "orchestrator_capabilities", map[string]any{})
	if err != nil {
		t.Fatalf("orchestrator_capabilities failed: %v", err)
	}
	if capRes.IsError {
		t.Fatalf("orchestrator_capabilities returned error: %+v", capRes)
	}
	if len(capRes.Content) == 0 {
		t.Fatalf("expected content in orchestrator_capabilities result")
	}
	var capabilities mcp.CapabilitiesResult
	if err := json.Unmarshal([]byte(capRes.Content[0].Text), &capabilities); err != nil {
		t.Fatalf("failed to decode orchestrator_capabilities: %v", err)
	}
	if capabilities.Caller.ThreadID != parentThreadID {
		t.Errorf("Caller.ThreadID = %q, want %q", capabilities.Caller.ThreadID, parentThreadID)
	}
	if capabilities.Caller.Driver != string(domain.DriverClaude) {
		t.Errorf("Caller.Driver = %q, want %q", capabilities.Caller.Driver, domain.DriverClaude)
	}
	if capabilities.Caller.Cwd != tempDir {
		t.Errorf("Caller.Cwd = %q, want %q", capabilities.Caller.Cwd, tempDir)
	}
	if capabilities.Caller.PermissionMode != domain.PermissionAcceptEdits {
		t.Errorf("Caller.PermissionMode = %q, want %q", capabilities.Caller.PermissionMode, domain.PermissionAcceptEdits)
	}
	if len(capabilities.Providers) == 0 {
		t.Fatalf("expected ready providers to be returned")
	}
	foundClaude := false
	for _, p := range capabilities.Providers {
		if p.Driver == string(domain.DriverClaude) {
			foundClaude = true
			if len(p.Models) == 0 || p.Models[0] != "fake-model-1" {
				t.Errorf("Provider models = %+v, want [fake-model-1]", p.Models)
			}
		}
	}
	if !foundClaude {
		t.Errorf("expected claude in ready providers list: %+v", capabilities.Providers)
	}

	// 3. Call list_threads and read_thread (view: "messages" and view: "activity")
	// against a thread with streamed message deltas + tool calls.
	wsItem := ws.Create("Test WS", "prompt", tempDir)
	streamThreadID := "thread-streamed-1"
	_, ok := ws.AddTask(wsItem.ID, domain.Task{
		ThreadID:   streamThreadID,
		Title:      "Stream Test Task",
		Driver:     domain.DriverClaude,
		Permission: domain.PermissionDefault,
		Cwd:        tempDir,
	})
	if !ok {
		t.Fatalf("failed to add task to workspace")
	}

	_, err = mgr.Start(context.Background(), domain.DriverClaude, domain.SessionStartInput{
		ThreadID: streamThreadID,
		Cwd:      tempDir,
	})
	if err != nil {
		t.Fatalf("failed to start thread session in manager: %v", err)
	}

	// Emit streamed message deltas + tool calls into manager via adapter emitter
	adapter.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: streamThreadID,
		Text:     "Initial prompt for work",
	})
	adapter.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventAgentMessage,
		ThreadID: streamThreadID,
		Text:     "Writing ",
		Delta:    true,
		ItemID:   "stream-msg-1",
	})
	adapter.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventAgentMessage,
		ThreadID: streamThreadID,
		Text:     "refactored code.",
		Delta:    true,
		ItemID:   "stream-msg-1",
	})
	adapter.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventToolCall,
		ThreadID: streamThreadID,
		Tool: &domain.ToolCall{
			ID:     "tool-call-1",
			Name:   "write",
			Target: "main.go",
			Status: domain.ToolInProgress,
			Input:  map[string]any{"path": "main.go"},
		},
	})
	adapter.emit.Emit(domain.RuntimeEvent{
		Kind:     domain.EventToolResult,
		ThreadID: streamThreadID,
		Tool: &domain.ToolCall{
			ID:     "tool-call-1",
			Name:   "write",
			Target: "main.go",
			Status: domain.ToolCompleted,
			Output: "wrote 42 bytes",
		},
	})

	// Call list_threads
	listRes, err := callTool(3, "list_threads", map[string]any{"limit": 10})
	if err != nil {
		t.Fatalf("list_threads failed: %v", err)
	}
	if listRes.IsError {
		t.Fatalf("list_threads returned error: %+v", listRes)
	}
	var threads []mcp.ThreadSummary
	if err := json.Unmarshal([]byte(listRes.Content[0].Text), &threads); err != nil {
		t.Fatalf("failed to decode list_threads: %v", err)
	}
	foundThread := false
	for _, th := range threads {
		if th.ThreadID == streamThreadID {
			foundThread = true
			if th.Title != "Stream Test Task" {
				t.Errorf("th.Title = %q, want 'Stream Test Task'", th.Title)
			}
			break
		}
	}
	if !foundThread {
		t.Fatalf("expected %s in list_threads result: %+v", streamThreadID, threads)
	}

	// Call read_thread view="messages"
	readMsgRes, err := callTool(4, "read_thread", map[string]any{
		"threadId": streamThreadID,
		"view":     "messages",
	})
	if err != nil {
		t.Fatalf("read_thread messages failed: %v", err)
	}
	if readMsgRes.IsError {
		t.Fatalf("read_thread messages returned error: %+v", readMsgRes)
	}
	var readMsgData mcp.ThreadReadResult
	if err := json.Unmarshal([]byte(readMsgRes.Content[0].Text), &readMsgData); err != nil {
		t.Fatalf("failed to decode read_thread messages: %v", err)
	}
	// Verify deltas collapse into clean messages and no tool items in "messages" view
	var collapsedAssistant string
	for _, item := range readMsgData.Items {
		if item.Kind == "tool" {
			t.Errorf("view=messages must not contain tool items: %+v", item)
		}
		if item.Kind == "assistant" {
			collapsedAssistant += item.Text
		}
	}
	if !strings.Contains(collapsedAssistant, "Writing refactored code.") {
		t.Errorf("expected collapsed assistant message 'Writing refactored code.', got %q", collapsedAssistant)
	}

	// Call read_thread view="activity"
	readActRes, err := callTool(5, "read_thread", map[string]any{
		"threadId": streamThreadID,
		"view":     "activity",
	})
	if err != nil {
		t.Fatalf("read_thread activity failed: %v", err)
	}
	if readActRes.IsError {
		t.Fatalf("read_thread activity returned error: %+v", readActRes)
	}
	var readActData mcp.ThreadReadResult
	if err := json.Unmarshal([]byte(readActRes.Content[0].Text), &readActData); err != nil {
		t.Fatalf("failed to decode read_thread activity: %v", err)
	}
	foundToolItem := false
	for _, item := range readActData.Items {
		if item.Kind == "tool" {
			foundToolItem = true
			if item.Tool != "write" {
				t.Errorf("item.Tool = %q, want 'write'", item.Tool)
			}
			if item.Status != "completed" {
				t.Errorf("item.Status = %q, want 'completed'", item.Status)
			}
			if !strings.Contains(item.Text, "wrote 42 bytes") {
				t.Errorf("item.Text = %q, want 'wrote 42 bytes'", item.Text)
			}
		}
	}
	if !foundToolItem {
		t.Fatalf("expected tool item in activity view: %+v", readActData.Items)
	}

	// 4. Verify permission fencing: calling delegate_task requesting bypassPermissions
	// from an acceptEdits parent returns isError: true.
	delRes, err := callTool(6, "delegate_task", map[string]any{
		"title":          "Privileged Task",
		"task":           "do dangerous work",
		"permissionMode": "bypassPermissions",
	})
	if err != nil {
		t.Fatalf("delegate_task call failed: %v", err)
	}
	if !delRes.IsError {
		t.Fatalf("expected delegate_task with bypassPermissions to return isError=true, got: %+v", delRes)
	}
	if len(delRes.Content) == 0 || !strings.Contains(delRes.Content[0].Text, "escalation denied") {
		t.Errorf("expected error message to mention 'escalation denied', got: %+v", delRes.Content)
	}

	// 5. Revoke the thread credential via RevokeThread and verify the next HTTP /mcp call returns 401.
	srv.Registry().RevokeThread(parentThreadID)

	pingReq, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"ping"}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	pingReq.Header.Set("Content-Type", "application/json")
	pingReq.Header.Set("Authorization", "Bearer "+cred.Token)
	pingReq.Header.Set("Mcp-Session-Id", sessionID)
	pingResp, err := client.Do(pingReq)
	if err != nil {
		t.Fatalf("ping request failed: %v", err)
	}
	defer pingResp.Body.Close()
	if pingResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized after RevokeThread, got %d", pingResp.StatusCode)
	}
}
