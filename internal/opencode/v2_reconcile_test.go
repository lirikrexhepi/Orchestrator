package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
)

type reconcileTestEmitter struct {
	mu     sync.Mutex
	events []domain.RuntimeEvent
	ch     chan domain.RuntimeEvent
}

func newReconcileTestEmitter() *reconcileTestEmitter {
	return &reconcileTestEmitter{ch: make(chan domain.RuntimeEvent, 10)}
}

func (e *reconcileTestEmitter) Emit(event domain.RuntimeEvent) {
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
	select {
	case e.ch <- event:
	default:
	}
}

func (e *reconcileTestEmitter) last() domain.RuntimeEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.events) == 0 {
		return domain.RuntimeEvent{}
	}
	return e.events[len(e.events)-1]
}

func TestV2ReconcileThreadsCompleted(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session/ses-1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":     "ses-1",
					"status": "completed",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	emit := newReconcileTestEmitter()
	adapter := NewV2Adapter(domain.ProviderSettings{}, emit)
	api := &httpClient{base: ts.URL, client: ts.Client()}

	th := &v2Thread{
		threadID:  "thread-1",
		sessionID: "ses-1",
		turnID:    "turn-100",
		toolName:  map[string]string{"call-1": "read"},
		toolSeen:  map[string]bool{"call-1": true},
		itemText:  map[string]string{"item-1": "some text"},
	}
	adapter.threads["thread-1"] = th
	adapter.bySession["ses-1"] = th

	adapter.reconcileThreads(api)

	th.mu.Lock()
	turnID := th.turnID
	tools := len(th.toolName)
	seen := len(th.toolSeen)
	texts := len(th.itemText)
	th.mu.Unlock()

	if turnID != "" {
		t.Fatalf("expected turnID to be cleared, got %q", turnID)
	}
	if tools != 0 || seen != 0 || texts != 0 {
		t.Fatalf("expected tool and item state to be cleared, got tools=%d seen=%d texts=%d", tools, seen, texts)
	}

	last := emit.last()
	if last.Kind != domain.EventTurnCompleted {
		t.Fatalf("expected EventTurnCompleted, got %q", last.Kind)
	}
	if last.ThreadID != "thread-1" || last.TurnID != "turn-100" {
		t.Fatalf("unexpected event thread/turn: %+v", last)
	}
	if last.StopReason != domain.StopEndTurn {
		t.Fatalf("expected StopEndTurn, got %q", last.StopReason)
	}
}

func TestV2ReconcileThreadsFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session/ses-2" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":     "ses-2",
					"status": "failed",
					"error": map[string]any{
						"message": "Model execution limit reached",
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	emit := newReconcileTestEmitter()
	adapter := NewV2Adapter(domain.ProviderSettings{}, emit)
	api := &httpClient{base: ts.URL, client: ts.Client()}

	th := &v2Thread{
		threadID:  "thread-2",
		sessionID: "ses-2",
		turnID:    "turn-200",
		toolName:  map[string]string{"call-2": "bash"},
		toolSeen:  map[string]bool{"call-2": true},
		itemText:  map[string]string{"item-2": "error text"},
	}
	adapter.threads["thread-2"] = th
	adapter.bySession["ses-2"] = th

	adapter.reconcileThreads(api)

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
	if last.ThreadID != "thread-2" || last.TurnID != "turn-200" {
		t.Fatalf("unexpected event thread/turn: %+v", last)
	}
	if last.Error != "Model execution limit reached" {
		t.Fatalf("expected error message 'Model execution limit reached', got %q", last.Error)
	}
}

func TestV2ReconcileAfterSSEDrop(t *testing.T) {
	var sseAttempts int
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/event":
			mu.Lock()
			sseAttempts++
			attempt := sseAttempts
			mu.Unlock()

			if attempt == 1 {
				// Immediately drop first SSE connection
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				// Close connection by returning
				return
			}
			// On second attempt, hold connection open
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		case "/api/session/ses-drop":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":     "ses-drop",
					"status": "completed",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	emit := newReconcileTestEmitter()
	adapter := NewV2Adapter(domain.ProviderSettings{}, emit)
	api := &httpClient{base: ts.URL, client: ts.Client()}

	th := &v2Thread{
		threadID:  "thread-drop",
		sessionID: "ses-drop",
		turnID:    "turn-drop-1",
		toolName:  map[string]string{"call-d": "edit"},
		toolSeen:  map[string]bool{"call-d": true},
		itemText:  map[string]string{"item-d": "dropping..."},
	}
	adapter.threads["thread-drop"] = th
	adapter.bySession["ses-drop"] = th

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go adapter.streamEvents(ctx, api)

	select {
	case event := <-emit.ch:
		if event.Kind != domain.EventTurnCompleted {
			t.Fatalf("expected EventTurnCompleted, got %q", event.Kind)
		}
		if event.ThreadID != "thread-drop" || event.TurnID != "turn-drop-1" {
			t.Fatalf("unexpected event thread/turn: %+v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for turn to reconcile after SSE drop")
	}

	th.mu.Lock()
	turnID := th.turnID
	th.mu.Unlock()
	if turnID != "" {
		t.Fatalf("expected turnID to be cleared, got %q", turnID)
	}
}
