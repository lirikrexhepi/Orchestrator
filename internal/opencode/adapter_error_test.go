package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
