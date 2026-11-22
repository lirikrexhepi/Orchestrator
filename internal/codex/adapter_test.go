package codex

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"composer/internal/domain"
	"composer/internal/jsonrpc"
)

type codexEmitter struct {
	mu     sync.Mutex
	events []domain.RuntimeEvent
}

func (e *codexEmitter) Emit(ev domain.RuntimeEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *codexEmitter) snapshot() []domain.RuntimeEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]domain.RuntimeEvent, len(e.events))
	copy(out, e.events)
	return out
}

func TestStreamedItemDeduplicationAndItemID(t *testing.T) {
	emit := &codexEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	th := &thread{
		threadID:      "t1",
		codexID:       "codex-1",
		turnID:        "turn-1",
		streamedItems: make(map[string]bool),
	}
	a.threads["t1"] = th
	a.byCodex["codex-1"] = th

	// Streamed agent message delta followed by completed item must not duplicate.
	a.onAgentDelta(json.RawMessage(`{"threadId":"codex-1","turnId":"turn-1","itemId":"item-msg-1","delta":"hello "}`))
	a.onItem(json.RawMessage(`{"threadId":"codex-1","turnId":"turn-1","item":{"id":"item-msg-1","type":"agentMessage","text":"hello world"}}`), true)

	// Streamed reasoning delta followed by completed item must not duplicate.
	a.onReasoningDelta(json.RawMessage(`{"threadId":"codex-1","turnId":"turn-1","itemId":"item-rsn-1","delta":"thinking..."}`))
	a.onItem(json.RawMessage(`{"threadId":"codex-1","turnId":"turn-1","item":{"id":"item-rsn-1","type":"reasoning","summary":["thinking..."]}}`), true)

	// Unstreamed completed item should still be emitted with ItemID.
	a.onItem(json.RawMessage(`{"threadId":"codex-1","turnId":"turn-1","item":{"id":"item-msg-2","type":"agentMessage","text":"unstreamed"}}`), true)

	events := emit.snapshot()
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	if events[0].Kind != domain.EventAgentMessage || events[0].ItemID != "item-msg-1" || !events[0].Delta || events[0].Text != "hello " {
		t.Fatalf("event[0] = %+v", events[0])
	}
	if events[1].Kind != domain.EventAgentThought || events[1].ItemID != "item-rsn-1" || !events[1].Delta || events[1].Text != "thinking..." {
		t.Fatalf("event[1] = %+v", events[1])
	}
	if events[2].Kind != domain.EventAgentMessage || events[2].ItemID != "item-msg-2" || events[2].Delta || events[2].Text != "unstreamed" {
		t.Fatalf("event[2] = %+v", events[2])
	}
}

func TestTokenUsageNotificationAndLegacyFallback(t *testing.T) {
	emit := &codexEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	th := &thread{threadID: "t1", codexID: "codex-1", turnID: "turn-1"}
	a.threads["t1"] = th
	a.byCodex["codex-1"] = th

	// Modern tokenUsage.last + modelContextWindow payload.
	a.onUsage(json.RawMessage(`{
		"threadId":"codex-1",
		"turnId":"turn-1",
		"tokenUsage":{
			"last":{"totalTokens":45000,"inputTokens":40000,"outputTokens":5000,"cachedInputTokens":12000},
			"total":{"totalTokens":90000,"inputTokens":80000,"outputTokens":10000},
			"modelContextWindow":200000
		}
	}`))

	// Legacy usage fallback payload.
	a.onUsage(json.RawMessage(`{
		"threadId":"codex-1",
		"usage":{"inputTokens":1000,"outputTokens":200,"cachedInputTokens":100,"contextWindow":128000}
	}`))

	events := emit.snapshot()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	u0 := events[0].Usage
	if u0 == nil || u0.ContextTokens != 45000 || u0.ContextWindow != 200000 || u0.InputTokens != 40000 || u0.CacheReadTokens != 12000 {
		t.Fatalf("modern usage = %+v", u0)
	}
	u1 := events[1].Usage
	if u1 == nil || u1.ContextTokens != 1200 || u1.ContextWindow != 128000 {
		t.Fatalf("legacy usage = %+v", u1)
	}
}

func TestApprovalPayloadsAndCasing(t *testing.T) {
	if got := approvalPolicy(domain.PermissionAcceptEdits); got != "on-failure" {
		t.Fatalf("approvalPolicy(AcceptEdits) = %q, want on-failure", got)
	}
	if got := approvalPolicy(domain.PermissionDefault); got != "on-request" {
		t.Fatalf("approvalPolicy(Default) = %q, want on-request", got)
	}

	emit := &codexEmitter{}
	a := NewAdapter(domain.ProviderSettings{}, emit)
	th := &thread{threadID: "t1", codexID: "codex-1", turnID: "turn-1"}
	a.threads["t1"] = th
	a.byCodex["codex-1"] = th

	// 1. Command execution approval uses accept/acceptForSession/decline/cancel
	cmdDone := make(chan any, 1)
	go func() {
		res, _ := a.onApprovalRequest(context.Background(), "item/commandExecution/requestApproval", json.RawMessage(`{
			"threadId":"codex-1","turnId":"turn-1","itemId":"cmd-1","command":"ls"
		}`))
		cmdDone <- res
	}()
	time.Sleep(20 * time.Millisecond)
	if err := a.RespondToApproval(context.Background(), "t1", "cmd-1", domain.ApprovalAllowAlways); err != nil {
		t.Fatal(err)
	}
	res1 := (<-cmdDone).(ApprovalResponse)
	if res1.Decision != "acceptForSession" {
		t.Fatalf("command approval decision = %q, want acceptForSession", res1.Decision)
	}

	// 2. Permissions approval uses {permissions, scope}
	permDone := make(chan any, 1)
	go func() {
		res, _ := a.onApprovalRequest(context.Background(), "item/permissions/requestApproval", json.RawMessage(`{
			"threadId":"codex-1","turnId":"turn-1","itemId":"perm-1","permissions":{"network":{"enabled":true}}
		}`))
		permDone <- res
	}()
	time.Sleep(20 * time.Millisecond)
	if err := a.RespondToApproval(context.Background(), "t1", "perm-1", domain.ApprovalAllowOnce); err != nil {
		t.Fatal(err)
	}
	res2 := (<-permDone).(PermissionsApprovalResponse)
	if res2.Scope != "turn" || res2.Permissions["network"] == nil {
		t.Fatalf("permissions approval response = %+v", res2)
	}
}

func TestUpdateModelAllowsEffortOnlyAndBuildTurnInput(t *testing.T) {
	a := NewAdapter(domain.ProviderSettings{}, &codexEmitter{})
	th := &thread{threadID: "t1", model: "gpt-6.1-sol", effort: "medium"}
	a.threads["t1"] = th

	if !a.UpdateModel("t1", "", domain.ModelOptions{domain.OptionEffort: "high"}) {
		t.Fatal("UpdateModel returned false")
	}
	if th.model != "gpt-6.1-sol" || th.effort != "high" {
		t.Fatalf("model=%q effort=%q, want gpt-6.1-sol / high", th.model, th.effort)
	}

	inputs := buildTurnInput("check files", []domain.FileRef{
		{Path: "/tmp/a.png", MIME: "image/png"},
		{Path: "/tmp/main.go", MIME: "text/x-go"},
	})
	if len(inputs) != 2 {
		t.Fatalf("got %d inputs, want 2: %+v", len(inputs), inputs)
	}
	if inputs[0].Type != "text" || inputs[0].Text != "check files\n@/tmp/main.go" {
		t.Fatalf("inputs[0] = %+v", inputs[0])
	}
	if inputs[1].Type != "localImage" || inputs[1].Path != "/tmp/a.png" {
		t.Fatalf("inputs[1] = %+v", inputs[1])
	}
}

func TestStartSessionRequiresCwdAndResumeFailsExplicitly(t *testing.T) {
	a := NewAdapter(domain.ProviderSettings{}, &codexEmitter{})

	// Missing cwd returns error before spawning.
	if _, err := a.StartSession(context.Background(), domain.SessionStartInput{ThreadID: "t1"}); err == nil {
		t.Fatal("expected error when Cwd is empty")
	}

	// Resume failure returns error instead of falling back to thread/start.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	defer clientR.Close()
	defer serverW.Close()
	defer serverR.Close()
	defer clientW.Close()

	startedThreadStart := false
	serverConn := jsonrpc.NewConn(serverR, serverW, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "thread/resume":
			return nil, context.DeadlineExceeded
		case "thread/start":
			startedThreadStart = true
			return ThreadStartResponse{Thread: Thread{ID: "new-id"}}, nil
		}
		return nil, nil
	})
	defer serverConn.Close(nil)

	clientConn := jsonrpc.NewConn(clientR, clientW, a.handleInbound)
	defer clientConn.Close(nil)
	a.conn = clientConn

	_, err := a.StartSession(context.Background(), domain.SessionStartInput{
		ThreadID: "t-res",
		Cwd:      t.TempDir(),
		Resume:   "missing-id",
	})
	if err == nil {
		t.Fatal("expected StartSession to fail when thread/resume fails")
	}
	if startedThreadStart {
		t.Fatal("must not fall back to thread/start when thread/resume fails")
	}
}

func TestAbortPendingOnStopAll(t *testing.T) {
	a := NewAdapter(domain.ProviderSettings{}, &codexEmitter{})
	ch := make(chan string, 1)
	a.pending["req-1"] = ch
	if err := a.StopAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-ch:
		if dec != DecisionAbort {
			t.Fatalf("decision = %q, want %q", dec, DecisionAbort)
		}
	case <-time.After(time.Second):
		t.Fatal("pending channel was not drained on StopAll")
	}
}
