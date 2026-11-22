package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/jsonrpc"
	"composer/internal/provider"
)

type fakeServer struct {
	mu          sync.Mutex
	conn        *jsonrpc.Conn
	initParams  initializeParams
	newParams   sessionNewParams
	loadParams  sessionLoadParams
	loadErr     error
	configCalls []sessionSetConfigParams
	promptCalls []sessionPromptParams
	cancelCalls []sessionCancelParams

	onPrompt func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error)
}

func setupFakeServerPair(t *testing.T, s *session, srv *fakeServer) (*Adapter, *jsonrpc.Conn, *jsonrpc.Conn, *eventRecorder) {
	t.Helper()
	rec := &eventRecorder{}
	a := NewAdapter(domain.ProviderSettings{}, rec)

	rClient, wServer := io.Pipe()
	rServer, wClient := io.Pipe()

	serverHandler := func(ctx context.Context, method string, rawParams json.RawMessage) (any, error) {
		srv.mu.Lock()
		defer srv.mu.Unlock()

		switch method {
		case "initialize":
			_ = json.Unmarshal(rawParams, &srv.initParams)
			return initializeResult{ProtocolVersion: 1}, nil

		case "session/new":
			_ = json.Unmarshal(rawParams, &srv.newParams)
			return sessionNewResult{SessionID: "sess-new-123"}, nil

		case "session/load":
			_ = json.Unmarshal(rawParams, &srv.loadParams)
			if srv.loadErr != nil {
				return nil, srv.loadErr
			}
			return sessionNewResult{SessionID: srv.loadParams.SessionID}, nil

		case "session/set_config_option":
			var cfg sessionSetConfigParams
			_ = json.Unmarshal(rawParams, &cfg)
			srv.configCalls = append(srv.configCalls, cfg)
			return nil, nil

		case "session/cancel":
			var c sessionCancelParams
			_ = json.Unmarshal(rawParams, &c)
			srv.cancelCalls = append(srv.cancelCalls, c)
			return nil, nil

		case "session/prompt":
			var p sessionPromptParams
			_ = json.Unmarshal(rawParams, &p)
			srv.promptCalls = append(srv.promptCalls, p)
			if srv.onPrompt != nil {
				conn := srv.conn
				srv.mu.Unlock()
				res, err := srv.onPrompt(conn, p)
				srv.mu.Lock()
				return res, err
			}
			return sessionPromptResult{StopReason: "end_turn"}, nil

		default:
			return nil, nil
		}
	}

	serverConn := jsonrpc.NewConn(rServer, wServer, serverHandler)
	srv.conn = serverConn

	clientConn := jsonrpc.NewConn(rClient, wClient, a.makeHandler(s))
	s.conn = clientConn

	a.mu.Lock()
	a.sessions[s.threadID] = s
	a.mu.Unlock()

	t.Cleanup(func() {
		clientConn.Close(nil)
		serverConn.Close(nil)
		_ = wClient.Close()
		_ = wServer.Close()
		_ = rClient.Close()
		_ = rServer.Close()
	})

	return a, clientConn, serverConn, rec
}

func TestInitSessionHandshake(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:   "t-init",
		cwd:        `C:\project`,
		model:      "gemini-2.5-pro-high",
		permission: domain.PermissionPlan,
		skills: &domain.SkillPolicy{
			Mode:   domain.SkillsOnly,
			Skills: []domain.SkillRef{{Name: "coding-guidelines", Path: `C:\skills\coding`}},
		},
		tools:     make(map[string]bool),
		baselines: make(map[string]string),
		pending:   make(map[string]*pendingApproval),
	}

	a, _, _, _ := setupFakeServerPair(t, s, srv)

	in := domain.SessionStartInput{
		ThreadID:   "t-init",
		Cwd:        `C:\project`,
		Model:      "gemini-2.5-pro-high",
		Permission: domain.PermissionPlan,
		Skills:     s.skills,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.initSession(ctx, s, in); err != nil {
		t.Fatalf("initSession failed: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	// 1. initialize checked
	if srv.initParams.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", srv.initParams.ProtocolVersion)
	}
	if srv.initParams.ClientInfo.Name != "orchestrator" {
		t.Errorf("ClientInfo.Name = %q, want 'orchestrator'", srv.initParams.ClientInfo.Name)
	}

	// 2. session/new checked
	if srv.newParams.MCPServers == nil {
		t.Errorf("mcpServers must not be nil")
	}
	encodedNew, _ := json.Marshal(srv.newParams)
	if !strings.Contains(string(encodedNew), `"mcpServers":[]`) {
		t.Errorf("mcpServers did not serialize as []: %s", string(encodedNew))
	}

	meta := srv.newParams.Meta
	if meta == nil {
		t.Fatal("meta was nil")
	}
	agyMeta, ok := meta["antigravity"].(map[string]any)
	if !ok {
		t.Fatalf("meta[antigravity] missing or not map: %v", meta)
	}
	spMeta, ok := agyMeta["systemPrompt"].(map[string]any)
	if !ok {
		t.Fatalf("systemPrompt missing: %v", agyMeta)
	}
	appendPrompt, _ := spMeta["append"].(string)
	if !strings.Contains(appendPrompt, provider.RuntimeInstructions) {
		t.Errorf("appendPrompt missing RuntimeInstructions: %q", appendPrompt)
	}
	if !strings.Contains(appendPrompt, "coding-guidelines") {
		t.Errorf("appendPrompt missing skill: %q", appendPrompt)
	}

	// 3. session/set_config_option calls
	foundModel := false
	foundEffort := false
	foundMode := false
	for _, cfg := range srv.configCalls {
		if cfg.ConfigID == "model" && cfg.Value == "gemini-2.5-pro" {
			foundModel = true
		}
		if cfg.ConfigID == "thought_level" && cfg.Value == "high" {
			foundEffort = true
		}
		if cfg.ConfigID == "mode" && cfg.Value == "plan" {
			foundMode = true
		}
	}
	if !foundModel {
		t.Errorf("model config not applied, got calls: %+v", srv.configCalls)
	}
	if !foundEffort {
		t.Errorf("thought_level config not applied, got calls: %+v", srv.configCalls)
	}
	if !foundMode {
		t.Errorf("mode config not applied, got calls: %+v", srv.configCalls)
	}

	// 4. SessionID set
	if s.sessionID != "sess-new-123" {
		t.Errorf("s.sessionID = %q, want 'sess-new-123'", s.sessionID)
	}
}

func TestResumeSessionLoadAndFallback(t *testing.T) {
	// Case 1: Load succeeds
	{
		srv := &fakeServer{}
		s := &session{
			threadID:  "t-res-1",
			cwd:       `C:\project`,
			tools:     make(map[string]bool),
			baselines: make(map[string]string),
			pending:   make(map[string]*pendingApproval),
		}
		a, _, _, _ := setupFakeServerPair(t, s, srv)
		in := domain.SessionStartInput{ThreadID: "t-res-1", Resume: "sess-res-1"}

		if err := a.initSession(context.Background(), s, in); err != nil {
			t.Fatalf("initSession resume failed: %v", err)
		}
		if s.sessionID != "sess-res-1" {
			t.Errorf("sessionID = %q, want 'sess-res-1'", s.sessionID)
		}
	}

	// Case 2: Load fails -> returns explicit error instead of falling back to session/new
	{
		srv := &fakeServer{loadErr: errors.New("not found")}
		s := &session{
			threadID:  "t-res-2",
			cwd:       `C:\project`,
			tools:     make(map[string]bool),
			baselines: make(map[string]string),
			pending:   make(map[string]*pendingApproval),
		}
		a, _, _, _ := setupFakeServerPair(t, s, srv)
		in := domain.SessionStartInput{ThreadID: "t-res-2", Resume: "bad-sess"}

		if err := a.initSession(context.Background(), s, in); err == nil {
			t.Fatalf("expected initSession to fail when session/load fails")
		}
		srv.mu.Lock()
		calledNew := len(srv.newParams.MCPServers) > 0 || srv.newParams.Cwd != ""
		srv.mu.Unlock()
		if calledNew {
			t.Fatalf("must not fall back to session/new when session/load fails")
		}
	}
}

func TestRequestPermissionBlocksUntilApproval(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:   "t-perm",
		permission: domain.PermissionDefault,
		sessionID:  "sess-perm",
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	permApproved := make(chan string, 1)

	srv.onPrompt = func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error) {
		// Server sends permission request to client
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var res requestPermissionResult
		err := conn.Call(ctx, "session/request_permission", requestPermissionParams{
			SessionID: "sess-perm",
			ToolCall: acpToolCallPermission{
				ToolCallID: "call-delete-file",
				Title:      "delete_file",
				Kind:       "edit",
				RawInput:   map[string]any{"path": "secret.txt"},
			},
			Options: []acpPermissionOption{
				{OptionID: "opt-allow-once", Name: "Allow once", Kind: "allow_once"},
				{OptionID: "opt-allow-always", Name: "Always allow", Kind: "allow_always"},
				{OptionID: "opt-reject", Name: "Deny", Kind: "reject_once"},
			},
		}, &res)
		if err != nil {
			return sessionPromptResult{}, err
		}
		permApproved <- res.Outcome.OptionID
		return sessionPromptResult{StopReason: "end_turn"}, nil
	}

	a, _, _, rec := setupFakeServerPair(t, s, srv)

	turnCtx, cancelTurn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTurn()

	if err := a.SendTurn(turnCtx, domain.SendTurnInput{
		ThreadID: "t-perm",
		TurnID:   "turn-perm-1",
		Text:     "Please delete file",
	}); err != nil {
		t.Fatalf("SendTurn failed: %v", err)
	}

	// Verify approval request was emitted
	ev := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventApprovalRequest && e.Approval != nil && e.Approval.RequestID == "call-delete-file"
	})
	if ev.Approval.Title != "delete_file" {
		t.Errorf("Approval.Title = %q, want 'delete_file'", ev.Approval.Title)
	}

	// Respond with ApprovalAllowOnce
	if err := a.RespondToApproval(context.Background(), "t-perm", "call-delete-file", domain.ApprovalAllowOnce); err != nil {
		t.Fatalf("RespondToApproval failed: %v", err)
	}

	// Verify server received opt-allow-once without deadlock
	select {
	case opt := <-permApproved:
		if opt != "opt-allow-once" {
			t.Errorf("selected option = %q, want 'opt-allow-once'", opt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for permission reply from client")
	}

	// Verify turn completed
	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventTurnCompleted && e.StopReason == domain.StopEndTurn
	})
}

func TestRequestPermissionBypassMode(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:   "t-bypass",
		permission: domain.PermissionBypass,
		sessionID:  "sess-bypass",
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	permApproved := make(chan string, 1)

	srv.onPrompt = func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var res requestPermissionResult
		err := conn.Call(ctx, "session/request_permission", requestPermissionParams{
			SessionID: "sess-bypass",
			ToolCall: acpToolCallPermission{
				ToolCallID: "call-cmd",
				Title:      "run_command",
			},
			Options: []acpPermissionOption{
				{OptionID: "opt-once", Name: "Once", Kind: "allow_once"},
				{OptionID: "opt-always", Name: "Always", Kind: "allow_always"},
			},
		}, &res)
		if err != nil {
			return sessionPromptResult{}, err
		}
		permApproved <- res.Outcome.OptionID
		return sessionPromptResult{StopReason: "end_turn"}, nil
	}

	a, _, _, rec := setupFakeServerPair(t, s, srv)

	if err := a.SendTurn(context.Background(), domain.SendTurnInput{
		ThreadID: "t-bypass",
		TurnID:   "turn-bypass-1",
		Text:     "run command",
	}); err != nil {
		t.Fatalf("SendTurn failed: %v", err)
	}

	select {
	case opt := <-permApproved:
		if opt != "opt-always" {
			t.Errorf("bypass selected %q, want 'opt-always'", opt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for bypass auto-approval")
	}

	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventTurnCompleted
	})
}

func TestUpdateModelInPlace(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:  "t-mod",
		sessionID: "sess-mod",
		model:     "gemini-2.0-flash",
		tools:     make(map[string]bool),
		baselines: make(map[string]string),
		pending:   make(map[string]*pendingApproval),
	}

	a, _, _, _ := setupFakeServerPair(t, s, srv)

	ok := a.UpdateModel("t-mod", "gemini-2.5-pro", domain.ModelOptions{domain.OptionEffort: "high"})
	if !ok {
		t.Fatal("UpdateModel returned false")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	foundModel := false
	foundEffort := false
	for _, cfg := range srv.configCalls {
		if cfg.ConfigID == "model" && cfg.Value == "gemini-2.5-pro" {
			foundModel = true
		}
		if cfg.ConfigID == "thought_level" && cfg.Value == "high" {
			foundEffort = true
		}
	}
	if !foundModel || !foundEffort {
		t.Errorf("UpdateModel calls missing expected params: %+v", srv.configCalls)
	}
}

func TestBaselineFallbackAndDiff(t *testing.T) {
	srv := &fakeServer{}
	dir := t.TempDir()
	filePath := filepath.Join(dir, "code.txt")
	if err := os.WriteFile(filePath, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &session{
		threadID:   "t-diff",
		sessionID:  "sess-diff",
		permission: domain.PermissionBypass,
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	a, _, serverConn, rec := setupFakeServerPair(t, s, srv)
	s.turnID = "turn-diff"

	// 1. First sighting: capture baseline
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-diff",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call",
			ToolCallID:    "call-edit-1",
			Name:          "edit_file",
			Status:        "in_progress",
			Args:          map[string]any{"path": filePath},
		},
	})
	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolCall && e.Tool != nil && e.Tool.ID == "call-edit-1"
	})

	// Modify file on disk
	if err := os.WriteFile(filePath, []byte("line1\nmodified line\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Terminal sighting: completed, without native diffs -> triggers s.editDiff fallback
	_ = serverConn.Notify("session/update", sessionUpdateParams{
		SessionID: "sess-diff",
		Update: acpSessionUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    "call-edit-1",
			Name:          "edit_file",
			Status:        "completed",
			Args:          map[string]any{"path": filePath},
		},
	})
	ev := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventToolResult && e.Tool != nil && e.Tool.ID == "call-edit-1" && len(e.Tool.Diffs) > 0
	})
	diff := ev.Tool.Diffs[0]
	if !strings.Contains(diff.NewText, "modified line") {
		t.Errorf("fallback diff does not show change: %+v", diff)
	}
	_ = a
}

func TestInterruptTurnCancelsPrompt(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:   "t-intr",
		sessionID:  "sess-intr",
		permission: domain.PermissionBypass,
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	promptBlocked := make(chan struct{})
	srv.onPrompt = func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error) {
		close(promptBlocked)
		// Wait until cancelled
		for {
			srv.mu.Lock()
			cancelled := len(srv.cancelCalls) > 0
			srv.mu.Unlock()
			if cancelled {
				return sessionPromptResult{StopReason: "cancelled"}, nil
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	a, _, _, rec := setupFakeServerPair(t, s, srv)

	if err := a.SendTurn(context.Background(), domain.SendTurnInput{
		ThreadID: "t-intr",
		TurnID:   "turn-intr-1",
		Text:     "Long running prompt",
	}); err != nil {
		t.Fatalf("SendTurn failed: %v", err)
	}

	<-promptBlocked

	if err := a.InterruptTurn(context.Background(), "t-intr"); err != nil {
		t.Fatalf("InterruptTurn failed: %v", err)
	}

	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventTurnCompleted && e.StopReason == domain.StopCancelled
	})
}

func TestInitSessionMCPServers(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID: "t-mcp",
		cwd:      `C:\project`,
	}
	a, _, _, _ := setupFakeServerPair(t, s, srv)

	in := domain.SessionStartInput{
		ThreadID: "t-mcp",
		Cwd:      `C:\project`,
		MCP: &domain.MCPConfig{
			ServerName: "orchestrator",
			Endpoint:   "http://127.0.0.1:4000/mcp",
			AuthHeader: "Bearer secret-token-123",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.initSession(ctx, s, in); err != nil {
		t.Fatalf("initSession failed: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if len(srv.newParams.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(srv.newParams.MCPServers))
	}

	mcpMap, ok := srv.newParams.MCPServers[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", srv.newParams.MCPServers[0])
	}
	if mcpMap["type"] != "http" {
		t.Errorf("type = %v, want 'http'", mcpMap["type"])
	}
	if mcpMap["name"] != "orchestrator" {
		t.Errorf("name = %v, want 'orchestrator'", mcpMap["name"])
	}
	if mcpMap["url"] != "http://127.0.0.1:4000/mcp" {
		t.Errorf("url = %v, want 'http://127.0.0.1:4000/mcp'", mcpMap["url"])
	}

	headers, ok := mcpMap["headers"].([]any)
	if !ok {
		if hSlice, ok2 := mcpMap["headers"].([]map[string]string); ok2 {
			if len(hSlice) != 1 || hSlice[0]["name"] != "Authorization" || hSlice[0]["value"] != "Bearer secret-token-123" {
				t.Errorf("headers = %v", hSlice)
			}
		} else {
			t.Fatalf("unexpected headers type %T: %v", mcpMap["headers"], mcpMap["headers"])
		}
	} else {
		if len(headers) != 1 {
			t.Fatalf("expected 1 header, got %d", len(headers))
		}
		hMap, ok := headers[0].(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any for header, got %T", headers[0])
		}
		if hMap["name"] != "Authorization" || hMap["value"] != "Bearer secret-token-123" {
			t.Errorf("header = %v", hMap)
		}
	}
}

func TestStartSessionRequiresCwd(t *testing.T) {
	a := NewAdapter(domain.ProviderSettings{BinaryPath: "cmd"}, &eventRecorder{})
	if _, err := a.StartSession(context.Background(), domain.SessionStartInput{ThreadID: "t-nocwd"}); err == nil {
		t.Fatal("expected error when Cwd is empty")
	}
}

func TestAuthenticateAndDynamicThoughtAndModes(t *testing.T) {
	var authCalls []string
	var configCalls []sessionSetConfigParams
	var mu sync.Mutex

	rClient, wServer := io.Pipe()
	rServer, wClient := io.Pipe()

	serverConn := jsonrpc.NewConn(rServer, wServer, func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "initialize":
			return initializeResult{
				ProtocolVersion: 1,
				AuthMethods:     []acpAuthMethod{{ID: "oauth-personal", Name: "Google"}},
			}, nil
		case "authenticate":
			var p authenticateParams
			_ = json.Unmarshal(raw, &p)
			authCalls = append(authCalls, p.MethodID)
			return nil, nil
		case "session/new":
			return sessionNewResult{
				SessionID: "sess-dyn",
				ConfigOptions: []acpConfigOption{
					{ID: "model", Category: "model"},
					{ID: "reasoning_effort", Category: "thought_level"},
				},
				Modes: json.RawMessage(`{"currentModeId":"default","availableModes":[{"id":"default"},{"id":"plan"}]}`),
			}, nil
		case "session/set_config_option":
			var cfg sessionSetConfigParams
			_ = json.Unmarshal(raw, &cfg)
			configCalls = append(configCalls, cfg)
			return nil, nil
		}
		return nil, nil
	})
	defer serverConn.Close(nil)

	rec := &eventRecorder{}
	a := NewAdapter(domain.ProviderSettings{}, rec)
	s := &session{
		threadID:   "t-dyn",
		account:    "work",
		authMethod: "oauth-personal",
		cwd:        `C:\project`,
		model:      "gemini-2.5-pro-high",
		permission: domain.PermissionAcceptEdits,
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}
	clientConn := jsonrpc.NewConn(rClient, wClient, a.makeHandler(s))
	defer clientConn.Close(nil)
	s.conn = clientConn
	a.mu.Lock()
	a.sessions[s.threadID] = s
	a.mu.Unlock()

	if err := a.initSession(context.Background(), s, domain.SessionStartInput{
		ThreadID:   "t-dyn",
		Cwd:        `C:\project`,
		Model:      "gemini-2.5-pro-high",
		Permission: domain.PermissionAcceptEdits,
	}); err != nil {
		t.Fatalf("initSession failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authCalls) != 1 || authCalls[0] != "oauth-personal" {
		t.Fatalf("authCalls = %v, want [oauth-personal]", authCalls)
	}
	var gotThoughtID, gotMode string
	for _, cfg := range configCalls {
		if cfg.ConfigID == "reasoning_effort" && cfg.Value == "high" {
			gotThoughtID = cfg.ConfigID
		}
		if cfg.ConfigID == "mode" {
			gotMode = cfg.Value
		}
	}
	if gotThoughtID != "reasoning_effort" {
		t.Fatalf("expected dynamic thought_level config ID 'reasoning_effort', got %+v", configCalls)
	}
	if gotMode != "default" {
		t.Fatalf("expected AcceptEdits to map to 'default' (avoiding 'accept-edits'), got %q", gotMode)
	}
}

func TestQuestionPermissionNeverAutoApprovedUnderBypass(t *testing.T) {
	srv := &fakeServer{}
	s := &session{
		threadID:   "t-q",
		account:    "work",
		permission: domain.PermissionBypass,
		sessionID:  "sess-q",
		tools:      make(map[string]bool),
		baselines:  make(map[string]string),
		pending:    make(map[string]*pendingApproval),
	}

	questionReply := make(chan string, 1)
	srv.onPrompt = func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var res requestPermissionResult
		err := conn.Call(ctx, "session/request_permission", requestPermissionParams{
			SessionID: "sess-q",
			ToolCall: acpToolCallPermission{
				ToolCallID: "interaction_123",
				Title:      "ask_question",
				Kind:       "interaction",
				RawInput:   map[string]any{"question": "Pick color", "options": []any{"Red", "Blue"}},
			},
			Options: []acpPermissionOption{
				{OptionID: "opt-red", Name: "Red", Kind: "allow_once"},
				{OptionID: "opt-blue", Name: "Blue", Kind: "allow_once"},
			},
		}, &res)
		if err != nil {
			return sessionPromptResult{}, err
		}
		questionReply <- res.Outcome.OptionID
		return sessionPromptResult{StopReason: "end_turn"}, nil
	}

	a, _, _, rec := setupFakeServerPair(t, s, srv)
	if err := a.SendTurn(context.Background(), domain.SendTurnInput{
		ThreadID: "t-q",
		TurnID:   "turn-q1",
		Text:     "Ask me",
	}); err != nil {
		t.Fatal(err)
	}

	ev := rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventQuestionAsked
	})
	if ev.Question == nil || ev.Question.RequestID != "interaction_123" || ev.Account != "work" {
		t.Fatalf("unexpected question event: %+v", ev)
	}

	if err := a.RespondToQuestion(context.Background(), "t-q", "interaction_123", []string{"Blue"}); err != nil {
		t.Fatal(err)
	}
	select {
	case opt := <-questionReply:
		if opt != "opt-blue" {
			t.Fatalf("selected option = %q, want opt-blue", opt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for question reply")
	}

	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventTurnCompleted && e.TurnID == "turn-q1"
	})
}

func TestConcurrentTurnSettlementDoesNotClobberOrDuplicate(t *testing.T) {
	srv := &fakeServer{}
	turn1Entered := make(chan struct{})
	turn1Release := make(chan struct{})

	srv.onPrompt = func(conn *jsonrpc.Conn, params sessionPromptParams) (sessionPromptResult, error) {
		var text string
		if len(params.Prompt) > 0 {
			if m, ok := params.Prompt[0].(map[string]any); ok {
				text, _ = m["text"].(string)
			}
		}
		if text == "first" {
			close(turn1Entered)
			<-turn1Release
			return sessionPromptResult{StopReason: "cancelled"}, nil
		}
		return sessionPromptResult{StopReason: "end_turn"}, nil
	}

	s := &session{
		threadID:  "t-conc",
		account:   "work",
		sessionID: "sess-conc",
		tools:     make(map[string]bool),
		baselines: make(map[string]string),
		pending:   make(map[string]*pendingApproval),
	}
	a, _, _, rec := setupFakeServerPair(t, s, srv)

	if err := a.SendTurn(context.Background(), domain.SendTurnInput{
		ThreadID: "t-conc",
		TurnID:   "turn-1",
		Text:     "first",
	}); err != nil {
		t.Fatal(err)
	}
	<-turn1Entered

	// Release turn-1 shortly after cancel arrives so turn-2 proceeds cleanly.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(turn1Release)
	}()

	if err := a.SendTurn(context.Background(), domain.SendTurnInput{
		ThreadID: "t-conc",
		TurnID:   "turn-2",
		Text:     "second",
	}); err != nil {
		t.Fatal(err)
	}

	rec.wait(t, func(e domain.RuntimeEvent) bool {
		return e.Kind == domain.EventTurnCompleted && e.TurnID == "turn-2"
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	var turn1Terminals, turn2Terminals int
	for _, e := range rec.events {
		if e.Kind == domain.EventTurnCompleted || e.Kind == domain.EventTurnFailed {
			if e.TurnID == "turn-1" {
				turn1Terminals++
			} else if e.TurnID == "turn-2" {
				turn2Terminals++
			}
		}
	}
	if turn1Terminals != 1 || turn2Terminals != 1 {
		t.Fatalf("expected 1 terminal event per turn, got turn1=%d turn2=%d", turn1Terminals, turn2Terminals)
	}
}
