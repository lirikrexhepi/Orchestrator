package claude

import (
	"encoding/json"
	"testing"

	"composer/internal/domain"
)

// Captured verbatim from `claude --print --output-format stream-json --verbose`
// on CLI 2.1.220. This is the only frame that carries subscription quota.
const liveRateLimitFrame = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1786832400,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled_until","isUsingOverage":false},"uuid":"4164b31d-155e-41a6-8f4b-0939ffc0cd96","session_id":"d7d76585-e50f-4766-a235-ec52aeb192ed"}`

func TestParsesLiveRateLimitFrame(t *testing.T) {
	var envelope Envelope
	if err := json.Unmarshal([]byte(liveRateLimitFrame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Type != "rate_limit_event" {
		t.Fatalf("type = %q, want rate_limit_event", envelope.Type)
	}
	if envelope.RateLimitInfo == nil {
		t.Fatal("rate_limit_info did not decode")
	}
	if envelope.RateLimitInfo.RateLimitType != "five_hour" {
		t.Errorf("window = %q, want five_hour", envelope.RateLimitInfo.RateLimitType)
	}
	if envelope.RateLimitInfo.ResetsAt != 1786832400 {
		t.Errorf("resetsAt = %d, want 1786832400", envelope.RateLimitInfo.ResetsAt)
	}
	if envelope.RateLimitInfo.Status != "allowed" {
		t.Errorf("status = %q, want allowed", envelope.RateLimitInfo.Status)
	}
	// This frame carries no percentage; that must stay nil rather than becoming 0,
	// so the UI can distinguish "unreported" from "none used".
	if envelope.RateLimitInfo.UsedPercent != nil {
		t.Errorf("usedPercent = %v, want nil for a frame without the field", *envelope.RateLimitInfo.UsedPercent)
	}
}

func TestRateLimitFrameWithPercentage(t *testing.T) {
	const frame = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","rateLimitType":"seven_day","usedPercent":69,"resetsAt":1786832400}}`

	var envelope Envelope
	if err := json.Unmarshal([]byte(frame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.RateLimitInfo.UsedPercent == nil {
		t.Fatal("usedPercent did not decode")
	}
	if *envelope.RateLimitInfo.UsedPercent != 69 {
		t.Errorf("usedPercent = %d, want 69", *envelope.RateLimitInfo.UsedPercent)
	}
}

func TestRateLimitEmitsDomainEvent(t *testing.T) {
	var captured []domain.RuntimeEvent
	adapter := &Adapter{emit: emitterFunc(func(event domain.RuntimeEvent) {
		captured = append(captured, event)
	})}

	var envelope Envelope
	if err := json.Unmarshal([]byte(liveRateLimitFrame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	adapter.handleRateLimit(&session{threadID: "t1"}, &envelope)

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	event := captured[0]
	if event.Kind != domain.EventRateLimit {
		t.Errorf("kind = %q, want %q", event.Kind, domain.EventRateLimit)
	}
	if len(event.RateLimits) != 1 || event.RateLimits[0].Window != "five_hour" {
		t.Fatalf("rate limits = %+v", event.RateLimits)
	}
	if event.Driver != domain.DriverClaude {
		t.Errorf("driver = %q, want claude", event.Driver)
	}
}

func TestRateLimitConvertsRemainingToUsed(t *testing.T) {
	const frame = `{"type":"rate_limit_event","rate_limit_info":{"rateLimitType":"five_hour","remainingPercent":85}}`

	var captured []domain.RuntimeEvent
	adapter := &Adapter{emit: emitterFunc(func(event domain.RuntimeEvent) {
		captured = append(captured, event)
	})}

	var envelope Envelope
	if err := json.Unmarshal([]byte(frame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	adapter.handleRateLimit(&session{threadID: "t1"}, &envelope)

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	used := captured[0].RateLimits[0].UsedPercent
	if used == nil || *used != 15 {
		t.Errorf("usedPercent = %v, want 15 (100 - 85 remaining)", used)
	}
}

func TestRateLimitConvertsUtilizationToUsed(t *testing.T) {
	const frame = `{"type":"rate_limit_event","rate_limit_info":{"rateLimitType":"five_hour","utilization":0.426}}`

	var captured []domain.RuntimeEvent
	adapter := &Adapter{emit: emitterFunc(func(event domain.RuntimeEvent) {
		captured = append(captured, event)
	})}

	var envelope Envelope
	if err := json.Unmarshal([]byte(frame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	adapter.handleRateLimit(&session{threadID: "t1"}, &envelope)

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	used := captured[0].RateLimits[0].UsedPercent
	if used == nil || *used != 43 {
		t.Errorf("usedPercent = %v, want 43 (math.Round(0.426*100))", used)
	}
}

func TestRateLimitIgnoresFrameWithoutWindow(t *testing.T) {
	const frame = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`

	var captured []domain.RuntimeEvent
	adapter := &Adapter{emit: emitterFunc(func(event domain.RuntimeEvent) {
		captured = append(captured, event)
	})}

	var envelope Envelope
	if err := json.Unmarshal([]byte(frame), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	adapter.handleRateLimit(&session{threadID: "t1"}, &envelope)

	if len(captured) != 0 {
		t.Errorf("a window-less frame must not emit, got %+v", captured)
	}
}

type emitterFunc func(domain.RuntimeEvent)

func (f emitterFunc) Emit(event domain.RuntimeEvent) { f(event) }
