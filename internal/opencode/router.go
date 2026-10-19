package opencode

import (
	"context"
	"sync"

	"composer/internal/domain"
	"composer/internal/provider"
)

type sessionAdapter interface {
	provider.Adapter
	provider.ModelUpdater
	provider.SessionReporter
}

type Router struct {
	settings domain.ProviderSettings
	legacy   *Adapter
	next     *V2Adapter

	mu    sync.RWMutex
	owner map[string]sessionAdapter
}

func NewRouter(settings domain.ProviderSettings, emit provider.Emitter) *Router {
	return &Router{
		settings: settings,
		legacy:   NewAdapter(settings, emit),
		next:     NewV2Adapter(settings, emit),
		owner:    make(map[string]sessionAdapter),
	}
}

func (r *Router) Driver() domain.DriverKind { return domain.DriverOpenCode }

func (r *Router) Capabilities() provider.Capabilities {
	return provider.Capabilities{SessionModelSwitch: true, Resume: true, Approvals: true}
}

func (r *Router) route(threadID string) sessionAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if adapter, ok := r.owner[threadID]; ok {
		return adapter
	}
	return r.legacy
}

func (r *Router) StartSession(ctx context.Context, in domain.SessionStartInput) (domain.Session, error) {
	var adapter sessionAdapter = r.legacy
	if IsV2(ctx, r.settings) {
		adapter = r.next
	}
	session, err := adapter.StartSession(ctx, in)
	if err != nil {
		return session, err
	}
	r.mu.Lock()
	r.owner[in.ThreadID] = adapter
	r.mu.Unlock()
	return session, nil
}

func (r *Router) SendTurn(ctx context.Context, in domain.SendTurnInput) error {
	return r.route(in.ThreadID).SendTurn(ctx, in)
}

func (r *Router) InterruptTurn(ctx context.Context, threadID string) error {
	return r.route(threadID).InterruptTurn(ctx, threadID)
}

func (r *Router) RespondToApproval(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	return r.route(threadID).RespondToApproval(ctx, threadID, requestID, decision)
}

func (r *Router) RespondToQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	return r.route(threadID).RespondToQuestion(ctx, threadID, requestID, answers)
}

func (r *Router) StopSession(ctx context.Context, threadID string) error {
	adapter := r.route(threadID)
	r.mu.Lock()
	delete(r.owner, threadID)
	r.mu.Unlock()
	return adapter.StopSession(ctx, threadID)
}

func (r *Router) StopAll(ctx context.Context) error {
	r.mu.Lock()
	r.owner = make(map[string]sessionAdapter)
	r.mu.Unlock()
	legacyErr := r.legacy.StopAll(ctx)
	if err := r.next.StopAll(ctx); err != nil {
		return err
	}
	return legacyErr
}

func (r *Router) HasSession(threadID string) bool {
	return r.route(threadID).HasSession(threadID)
}

func (r *Router) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	return r.route(threadID).UpdateModel(threadID, model, options)
}

func (r *Router) Session(threadID string) (domain.Session, bool) {
	return r.route(threadID).Session(threadID)
}
