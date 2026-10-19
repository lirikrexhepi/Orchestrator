package provider

import (
	"context"

	"composer/internal/domain"
)

// Adapter translates one provider CLI's native protocol into canonical runtime
// events. Implementations own their child processes and must release them when
// StopSession or StopAll is called.
type Adapter interface {
	Driver() domain.DriverKind
	Capabilities() Capabilities

	StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error)
	SendTurn(ctx context.Context, in domain.SendTurnInput) error
	InterruptTurn(ctx context.Context, threadID string) error
	RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error
	RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error
	StopSession(ctx context.Context, threadID string) error
	StopAll(ctx context.Context) error
	HasSession(threadID string) bool
}

// ProcessReporter is implemented by adapters that own an OS process per thread.
// Optional: it exists so tooling can attribute spawned child processes (dev
// servers and the like) to the agent that started them, and an adapter without
// a per-thread process simply does not implement it.
type ProcessReporter interface {
	SessionPID(threadID string) (int, bool)
}

// SessionReporter is implemented by adapters that can report a thread's current
// session, including the provider's own session id.
//
// Optional, and read after the fact rather than at start: several CLIs only
// reveal the id once their stream opens, so the value captured when a session is
// created is frequently empty. Without it a thread cannot be resumed in a later
// run.
type SessionReporter interface {
	Session(threadID string) (domain.Session, bool)
}

type Capabilities struct {
	SessionModelSwitch bool `json:"sessionModelSwitch"`
	Resume             bool `json:"resume"`
	Approvals          bool `json:"approvals"`
	Plans              bool `json:"plans"`
}

type ModelUpdater interface {
	UpdateModel(threadID, model string, options domain.ModelOptions) bool
}

type TurnRemover interface {
	RemoveTurn(sessionID, text string, occurrence int) error
}

type SessionMover interface {
	MoveSession(sessionID, cwd string) error
}

// Emitter is how adapters publish canonical events. The session manager
// supplies one that stamps sequence numbers and fans out to subscribers.
type Emitter interface {
	Emit(domain.RuntimeEvent)
}

type EmitterFunc func(domain.RuntimeEvent)

func (f EmitterFunc) Emit(e domain.RuntimeEvent) { f(e) }

// Driver produces adapters and reports CLI availability for one provider kind.
type Driver interface {
	Kind() domain.DriverKind
	DisplayName() string
	DefaultSettings() domain.ProviderSettings
	Probe(ctx context.Context, settings domain.ProviderSettings) domain.ProviderSnapshot
	NewAdapter(settings domain.ProviderSettings, emit Emitter) (Adapter, error)
}
