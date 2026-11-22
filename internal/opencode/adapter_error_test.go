package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"composer/internal/domain"
)

type testEmitter struct {
	mu     sync.Mutex
	events []domain.RuntimeEvent
}

func (e *testEmitter) Emit(event domain.RuntimeEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *testEmitter) last() domain.RuntimeEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.events) == 0 {
		return domain.RuntimeEvent{}
	}
	return e.events[len(e.events)-1]
}

func TestParseOpenCodeLogLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantLevel   string
		wantSession string
		wantErrMsg  string
	}{
		{
			name:        "Free usage limit error",
			line:        `timestamp=2026-10-27T18:14:27.069Z level=ERROR run=31c83129 message="stream error" providerID=opencode modelID=beam-free session.id=ses_123 small=false agent=build mode=primary error.error="AI_APICallError: Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go"`,
			wantLevel:   "ERROR",
			wantSession: "ses_123",
			wantErrMsg:  "AI_APICallError: Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go",
		},
		{
			name:        "Stream error without quoted error",
			line:        `timestamp=2026-10-27T18:14:27.069Z level=ERROR message="stream error" session.id=ses_456`,
			wantLevel:   "ERROR",
			wantSession: "ses_456",
			wantErrMsg:  "OpenCode stream error",
		},
		{
			name:        "Generic error with message",
			line:        `timestamp=2026-10-27T18:14:27.069Z level=ERROR message="Upstream gateway timeout" session.id=ses_789`,
			wantLevel:   "ERROR",
			wantSession: "ses_789",
			wantErrMsg:  "Upstream gateway timeout",
		},
		{
			name:        "Bare error field",
			line:        `timestamp=2026-10-27T18:14:27.069Z level=ERROR error.error=ConnectionRefused`,
			wantLevel:   "ERROR",
			wantSession: "",
			wantErrMsg:  "ConnectionRefused",
		},
		{
			name:        "Info log",
			line:        `timestamp=2026-10-27T18:14:27.069Z level=INFO message="stream"`,
			wantLevel:   "INFO",
			wantSession: "",
			wantErrMsg:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, sessionID, errMsg := parseOpenCodeLogLine(tt.line)
			if level != tt.wantLevel {
				t.Errorf("level = %q, want %q", level, tt.wantLevel)
			}
			if sessionID != tt.wantSession {
				t.Errorf("sessionID = %q, want %q", sessionID, tt.wantSession)
			}
			if errMsg != tt.wantErrMsg {
				t.Errorf("errMsg = %q, want %q", errMsg, tt.wantErrMsg)
			}
		})
	}
}

func TestOnServerErrorFailsStuckTurn(t *testing.T) {
	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{}, emit)

	th := &thread{
		threadID:  "thread-1",
		sessionID: "ses-1",
		turnID:    "turn-100",
	}
	adapter.threads["thread-1"] = th
	adapter.bySession["ses-1"] = th

	adapter.onServerError("ses-1", "AI_APICallError: Free usage limit reached")

	th.mu.Lock()
	turnID := th.turnID
	th.mu.Unlock()

	if turnID != "" {
		t.Fatalf("expected turnID to be cleared, got %q", turnID)
	}

	last := emit.last()
	if last.Kind != domain.EventTurnFailed {
		t.Fatalf("expected EventTurnFailed, got %q", last.Kind)
	}
	if last.ThreadID != "thread-1" || last.TurnID != "turn-100" {
		t.Fatalf("unexpected thread/turn in event: %+v", last)
	}
	if last.Error != "AI_APICallError: Free usage limit reached" {
		t.Fatalf("unexpected error message: %q", last.Error)
	}
}

func TestOnMessageUpdatedFailsOnMessageError(t *testing.T) {
	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{}, emit)

	th := &thread{
		threadID:  "thread-2",
		sessionID: "ses-2",
		turnID:    "turn-200",
	}
	adapter.threads["thread-2"] = th
	adapter.bySession["ses-2"] = th

	payload, _ := json.Marshal(map[string]any{
		"info": map[string]any{
			"id":        "msg-1",
			"sessionID": "ses-2",
			"role":      "assistant",
			"error": map[string]any{
				"name": "APIError",
				"data": map[string]any{
					"message": "Quota exceeded",
				},
			},
		},
	})

	adapter.onMessageUpdated(payload)

	th.mu.Lock()
	turnID := th.turnID
	th.mu.Unlock()

	if turnID != "" {
		t.Fatalf("expected turnID to be cleared, got %q", turnID)
	}

	last := emit.last()
	if last.Kind != domain.EventTurnFailed {
		t.Fatalf("expected EventTurnFailed, got %q", last.Kind)
	}
	if last.Error != "Quota exceeded" {
		t.Fatalf("expected 'Quota exceeded', got %q", last.Error)
	}
}

func TestInterruptTurnCancelsAndCompletes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/ses-3/abort" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	emit := &testEmitter{}
	adapter := NewAdapter(domain.ProviderSettings{}, emit)

	th := &thread{
		threadID:  "thread-3",
		sessionID: "ses-3",
		turnID:    "turn-300",
		api:       &httpClient{base: ts.URL, client: ts.Client()},
	}
	adapter.threads["thread-3"] = th
	adapter.bySession["ses-3"] = th

	err := adapter.InterruptTurn(context.Background(), "thread-3")
	if err != nil {
		t.Fatalf("InterruptTurn failed: %v", err)
	}

	th.mu.Lock()
	turnID := th.turnID
	th.mu.Unlock()

	if turnID != "" {
		t.Fatalf("expected turnID to be cleared, got %q", turnID)
	}

	last := emit.last()
	if last.Kind != domain.EventTurnCompleted {
		t.Fatalf("expected EventTurnCompleted, got %q", last.Kind)
	}
	if last.StopReason != domain.StopCancelled {
		t.Fatalf("expected StopCancelled, got %q", last.StopReason)
	}
}

func TestV1AndV2RequireCwdAndFailResumeExplicitly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/missing-1" || r.URL.Path == "/api/session/missing-1" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/session" || r.URL.Path == "/api/session" {
			t.Errorf("must not create a blank session when resume fails")
			http.Error(w, "should not be called", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	v1 := NewAdapter(domain.ProviderSettings{ServerURL: ts.URL}, &testEmitter{})
	if _, err := v1.StartSession(context.Background(), domain.SessionStartInput{ThreadID: "t1"}); err == nil {
		t.Fatal("v1 expected error when Cwd is empty")
	}
	if _, err := v1.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "t1", Cwd: t.TempDir(), Resume: "missing-1",
	}); err == nil {
		t.Fatal("v1 expected error when resume fails")
	}

	v2 := NewV2Adapter(domain.ProviderSettings{ServerURL: ts.URL}, &testEmitter{})
	if _, err := v2.StartSession(context.Background(), domain.SessionStartInput{ThreadID: "t2"}); err == nil {
		t.Fatal("v2 expected error when Cwd is empty")
	}
	if _, err := v2.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "t2", Cwd: t.TempDir(), Resume: "missing-1",
	}); err == nil {
		t.Fatal("v2 expected error when resume fails")
	}
}

func TestV2MCPRegistration(t *testing.T) {
	var mcpCalls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/mcp":
			mcpCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/api/session":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "ses-v2-mcp"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	v2 := NewV2Adapter(domain.ProviderSettings{ServerURL: ts.URL}, &testEmitter{})
	_, err := v2.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "t-v2-mcp",
		Cwd:      t.TempDir(),
		MCP: &domain.MCPConfig{
			ServerName: "orchestrator",
			Endpoint:   "http://127.0.0.1:45678/mcp",
			AuthHeader: "Bearer tok",
		},
	})
	if err != nil {
		t.Fatalf("v2 StartSession failed: %v", err)
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcpCalls = %d, want 1", mcpCalls.Load())
	}
}

func TestOnPartUpdatedDoesNotDropPrefixAssistantReply(t *testing.T) {
	emit := &testEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	th := &thread{
		threadID:     "t-prefix",
		sessionID:    "ses-prefix",
		turnID:       "turn-1",
		messageRoles: map[string]string{},
		partText:     map[string]string{},
		partTypes:    map[string]string{},
	}
	a.threads["t-prefix"] = th
	a.bySession["ses-prefix"] = th

	// Assistant reply that happens to share a prefix with user prompt ("yes") must NOT be dropped.
	raw, _ := json.Marshal(PartUpdatedProperties{
		Part: Part{ID: "p1", SessionID: "ses-prefix", MessageID: "m-assist", Type: "text", Text: "yes, I will do that."},
	})
	a.onPartUpdated(raw)
	if got := emit.last(); got.Kind != domain.EventAgentMessage || got.Text != "yes, I will do that." {
		t.Fatalf("expected assistant text to be emitted, got %+v", got)
	}

	// User message with role=="user" must be skipped.
	th.mu.Lock()
	th.messageRoles["m-user"] = "user"
	th.mu.Unlock()
	rawUser, _ := json.Marshal(PartUpdatedProperties{
		Part: Part{ID: "p2", SessionID: "ses-prefix", MessageID: "m-user", Type: "text", Text: "yes"},
	})
	a.onPartUpdated(rawUser)
	emit.mu.Lock()
	count := len(emit.events)
	emit.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected user part to be skipped (count=1), got %d", count)
	}
}

func TestFirstTurnContextLimitAndMultiStepUsageAndQuestionID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/providers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"providers": []any{
					map[string]any{
						"id": "anthropic",
						"models": map[string]any{
							"claude-sonnet": map[string]any{"limit": map[string]any{"context": 200000}},
						},
					},
				},
			})
		case "/question/q-1/reply":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := &testEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	api := &httpClient{base: ts.URL, client: ts.Client()}
	th := &thread{
		threadID:         "t-usage",
		sessionID:        "ses-usage",
		turnID:           "turn-1",
		api:              api,
		serverKey:        "srv-1",
		stepUsage:        make(map[string]domain.Usage),
		messageRoles:     make(map[string]string),
		partText:         make(map[string]string),
		partTypes:        make(map[string]string),
	}
	a.threads["t-usage"] = th
	a.bySession["ses-usage"] = th
	a.pendingQuestions["q-1"] = "ses-usage"

	msg1, _ := json.Marshal(map[string]any{
		"info": map[string]any{
			"id": "m1", "sessionID": "ses-usage", "role": "assistant",
			"providerID": "anthropic", "modelID": "claude-sonnet",
			"tokens": map[string]any{"input": 1000, "output": 200},
			"cost":   0.01,
		},
	})
	a.onMessageUpdated(msg1)
	first := emit.last()
	if first.Usage == nil || first.Usage.ContextWindow != 200000 || first.Usage.InputTokens != 1000 {
		t.Fatalf("first step usage = %+v", first.Usage)
	}

	// Second step in same turn must accumulate instead of overwriting step 1.
	msg2, _ := json.Marshal(map[string]any{
		"info": map[string]any{
			"id": "m2", "sessionID": "ses-usage", "role": "assistant",
			"providerID": "anthropic", "modelID": "claude-sonnet",
			"tokens": map[string]any{"input": 800, "output": 300},
			"cost":   0.02,
		},
	})
	a.onMessageUpdated(msg2)
	second := emit.last()
	if second.Usage == nil || second.Usage.InputTokens != 1800 || second.Usage.OutputTokens != 500 || second.Usage.ContextTokens != 1100 {
		t.Fatalf("second step accumulated usage = %+v", second.Usage)
	}

	if err := a.RespondToQuestion(context.Background(), "t-usage", "q-1", []string{"yes"}); err != nil {
		t.Fatal(err)
	}
	qEv := emit.last()
	if qEv.Kind != domain.EventQuestionAnswered || qEv.Question == nil || qEv.Question.RequestID != "q-1" {
		t.Fatalf("question answered event = %+v", qEv)
	}

	// Test onServerExit removes thread, cancels stream, cleans pending, emits EventSessionStopped
	streamCancelled := false
	a.mu.Lock()
	a.servers["srv-1"] = &serverEntry{streamed: func() { streamCancelled = true }}
	a.pending["p-1"] = "ses-usage"
	a.mu.Unlock()
	a.onServerExit("srv-1", "boom")
	if !streamCancelled {
		t.Fatal("onServerExit did not cancel stream context")
	}
	if a.HasSession("t-usage") {
		t.Fatal("onServerExit did not remove dead thread")
	}
	if last := emit.last(); last.Kind != domain.EventSessionStopped {
		t.Fatalf("expected EventSessionStopped, got %+v", last)
	}
}

func TestFileURIAndGoAuthPath(t *testing.T) {
	uri := fileURI(`C:\Users\PC\repo\main.go`)
	if !strings.HasPrefix(uri, "file:///C:/Users/PC/repo/main.go") {
		t.Fatalf("fileURI = %q", uri)
	}

	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	got, err := goAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "opencode", "auth.json")
	if got != want {
		t.Fatalf("goAuthPath = %q, want %q", got, want)
	}
	_ = os.RemoveAll(xdg)
}
