package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"composer/internal/domain"
	"composer/internal/logger"
	"composer/internal/provider"
)

// Manager owns every live agent session across providers. Adapters are created
// per driver and shared by the threads that use them, so a single Codex
// app-server or OpenCode server backs many concurrent threads.
type Manager struct {
	registry *provider.Registry
	bus      *Bus

	mu       sync.RWMutex
	adapters map[adapterKey]provider.Adapter
	// retired adapters were replaced after a settings change but may still own
	// live sessions; they are stopped on shutdown.
	retired  []provider.Adapter
	threads  map[string]*record
	history  map[string][]domain.RuntimeEvent

	// recorder persists transcripts. Optional: a manager without one behaves
	// exactly as before, which keeps every existing test unchanged.
	recorder Recorder
	notes    TurnNotes
	handoffs map[string]string

	accountFor AccountResolver
	expandRefs RefExpander
}

type AccountResolver func(kind domain.DriverKind, cwd string) string

type RefExpander func(files []domain.FileRef) (string, []domain.FileRef)

func (m *Manager) SetRefExpander(expand RefExpander) {
	m.mu.Lock()
	m.expandRefs = expand
	m.mu.Unlock()
}

type adapterKey struct {
	driver  domain.DriverKind
	account string
}

func (m *Manager) SetAccountResolver(resolve AccountResolver) {
	m.mu.Lock()
	m.accountFor = resolve
	m.mu.Unlock()
}

func (m *Manager) ResolveAccount(kind domain.DriverKind, cwd, requested string) (string, error) {
	if requested != "" {
		if _, ok := m.registry.Account(kind, requested); !ok {
			return "", fmt.Errorf("%s account %q no longer exists", domain.DriverLabel(kind), requested)
		}
		return domain.NormalizeAccount(requested), nil
	}
	m.mu.RLock()
	resolve := m.accountFor
	m.mu.RUnlock()
	preferred := ""
	if resolve != nil {
		preferred = resolve(kind, cwd)
	}
	return m.registry.ResolveAccount(kind, preferred), nil
}

// Recorder receives events for durable storage. Implemented by the history
// store; kept as an interface so the manager does not depend on the file layer
// and tests can observe what would be written.
type Recorder interface {
	// Record persists one event for a thread. Called on the publishing
	// goroutine, so implementations must be cheap and must not block.
	Record(threadID string, event domain.RuntimeEvent)
	// NoteProviderSession records the id a CLI needs to resume this thread in a
	// later run.
	NoteProviderSession(threadID, providerSessionID string)
}

// SetRecorder attaches durable storage. Safe to call before any session starts.
func (m *Manager) SetRecorder(recorder Recorder) {
	m.mu.Lock()
	m.recorder = recorder
	m.mu.Unlock()
}

type record struct {
	session domain.Session
	adapter provider.Adapter
	// input is how the session was started, kept so a session whose process
	// died (interrupt fallback, crash) can be revived on the next send.
	input domain.SessionStartInput
	// lastAt is the timestamp of the thread's latest event.
	lastAt int64
}

const historyLimit = 2000

func NewManager(registry *provider.Registry) *Manager {
	return &Manager{
		registry: registry,
		bus:      NewBus(),
		adapters: make(map[adapterKey]provider.Adapter),
		threads:  make(map[string]*record),
		history:  make(map[string][]domain.RuntimeEvent),
		handoffs: make(map[string]string),
	}
}

func (m *Manager) Bus() *Bus { return m.bus }

// SessionPID reports the CLI process for a thread when its adapter tracks one.
func (m *Manager) SessionPID(threadID string) (int, bool) {
	m.mu.RLock()
	entry, ok := m.threads[threadID]
	m.mu.RUnlock()
	if !ok {
		return 0, false
	}
	reporter, ok := entry.adapter.(provider.ProcessReporter)
	if !ok {
		return 0, false
	}
	return reporter.SessionPID(threadID)
}

func (m *Manager) adapterFor(kind domain.DriverKind, account string) (provider.Adapter, error) {
	key := adapterKey{driver: kind, account: domain.NormalizeAccount(account)}
	m.mu.RLock()
	adapter, ok := m.adapters[key]
	m.mu.RUnlock()
	if ok {
		return adapter, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if adapter, ok := m.adapters[key]; ok {
		return adapter, nil
	}

	adapter, err := m.registry.NewAdapter(kind, key.account, provider.EmitterFunc(m.record))
	if err != nil {
		return nil, err
	}
	m.adapters[key] = adapter
	return adapter, nil
}

// record stamps the event, appends it to the thread transcript, and publishes.
func (m *Manager) record(event domain.RuntimeEvent) {
	// Adapters report their own events and do not all know their driver kind, so
	// the manager fills it in from the thread's session. Consumers that group by
	// CLI (usage totals, diagnostics) depend on this being present.
	if (event.Driver == "" || event.Account == "" || event.Model == "") && event.ThreadID != "" {
		m.mu.RLock()
		if entry, ok := m.threads[event.ThreadID]; ok {
			if event.Driver == "" {
				event.Driver = entry.session.Driver
			}
			if event.Model == "" {
				event.Model = entry.session.Model
			}
			if event.Account == "" && event.Driver == entry.session.Driver {
				event.Account = entry.session.Account
			}
		}
		m.mu.RUnlock()
	}

	if event.Kind == domain.EventAgentMessage || event.Kind == domain.EventAgentThought {
		logger.Debugf("Manager", "Stream chunk: thread=%s kind=%s len=%d", event.ThreadID, event.Kind, len(event.Text))
	} else {
		logger.Infof("Manager", "Event: thread=%s driver=%s kind=%s seq=%d", event.ThreadID, event.Driver, event.Kind, event.Seq)
	}

	published := m.bus.Publish(event)

	if published.ThreadID == "" {
		return
	}
	m.mu.Lock()
	if entry, ok := m.threads[published.ThreadID]; ok {
		entry.lastAt = published.At
	}
	transcript := m.history[published.ThreadID]
	// Consecutive deltas of the same item are stored as one event, so the
	// bounded transcript covers whole turns instead of a few hundred tokens.
	if n := len(transcript); n > 0 && published.Delta && sameStream(transcript[n-1], published) {
		merged := transcript[n-1]
		merged.Text += published.Text
		// The merged event carries the newest seq, so a snapshot's highest
		// seq marks exactly what it already contains.
		merged.Seq = published.Seq
		transcript[n-1] = merged
	} else {
		transcript = append(transcript, published)
	}
	if len(transcript) > historyLimit {
		transcript = transcript[len(transcript)-historyLimit:]
	}
	m.history[published.ThreadID] = transcript
	recorder := m.recorder
	m.mu.Unlock()

	// Outside the lock: persistence must never widen the critical section that
	// every streamed token passes through.
	if recorder != nil {
		recorder.Record(published.ThreadID, published)

		// A turn boundary is the cheap moment to catch a resume id that only
		// became known once the stream opened.
		switch published.Kind {
		case domain.EventSessionStarted, domain.EventTurnCompleted, domain.EventTurnFailed:
			go m.refreshProviderSession(published.ThreadID)
		}
	}
}

// sameStream reports whether next continues prev's streamed item.
func sameStream(prev, next domain.RuntimeEvent) bool {
	return prev.Delta && prev.Kind == next.Kind && prev.ThreadID == next.ThreadID &&
		prev.TurnID == next.TurnID && prev.ItemID == next.ItemID &&
		(next.Kind == domain.EventAgentMessage || next.Kind == domain.EventAgentThought)
}

// LastActivity reports when the thread last produced an event.
func (m *Manager) LastActivity(threadID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.threads[threadID]; ok {
		return entry.lastAt
	}
	return 0
}

// ResetAdapter drops the cached adapter for a driver so the next session is
// built with the current settings. Sessions already running keep their adapter.
func (m *Manager) ResetAdapter(kind domain.DriverKind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, adapter := range m.adapters {
		if key.driver != kind {
			continue
		}
		m.retired = append(m.retired, adapter)
		delete(m.adapters, key)
	}
}

func (m *Manager) Start(ctx context.Context, kind domain.DriverKind, in domain.SessionStartInput) (domain.Session, error) {
	if in.ThreadID == "" {
		return domain.Session{}, errors.New("threadId is required")
	}

	m.mu.RLock()
	_, exists := m.threads[in.ThreadID]
	m.mu.RUnlock()
	if exists {
		return domain.Session{}, fmt.Errorf("thread %s already has an active session", in.ThreadID)
	}

	account, err := m.ResolveAccount(kind, in.Cwd, in.Account)
	if err != nil {
		return domain.Session{}, err
	}
	in.Account = account

	adapter, err := m.adapterFor(kind, account)
	if err != nil {
		return domain.Session{}, err
	}

	// Registered before the adapter starts: StartSession emits session.started
	// synchronously on some drivers, and record() stamps the driver from this
	// map. Registering afterwards would leave those first events unattributed.
	m.mu.Lock()
	m.threads[in.ThreadID] = &record{
		session: domain.Session{ThreadID: in.ThreadID, Driver: kind, Account: account, Cwd: in.Cwd, Model: in.Model},
		adapter: adapter,
		input:   in,
	}
	m.mu.Unlock()

	session, err := adapter.StartSession(ctx, in)
	if err != nil {
		m.mu.Lock()
		delete(m.threads, in.ThreadID)
		m.mu.Unlock()
		return domain.Session{}, err
	}

	if session.Driver == "" {
		session.Driver = kind
	}
	if session.Model == "" {
		session.Model = in.Model
	}
	session.Account = account

	m.mu.Lock()
	m.threads[in.ThreadID] = &record{session: session, adapter: adapter, input: in, lastAt: time.Now().UnixMilli()}
	recorder := m.recorder
	m.mu.Unlock()

	// The id a CLI needs to resume later. Some drivers know it here; others
	// (Claude) learn it once the stream opens and are picked up by refreshSession.
	if recorder != nil && session.ProviderSessionID != "" {
		recorder.NoteProviderSession(in.ThreadID, session.ProviderSessionID)
	}

	return session, nil
}

// refreshProviderSession re-reads a thread's session from its adapter and stores
// any resume id that appeared after start.
//
// Claude reports its session id asynchronously, so the value captured at start
// is often empty; without this a Claude thread could never be resumed.
func (m *Manager) refreshProviderSession(threadID string) {
	m.mu.RLock()
	entry, ok := m.threads[threadID]
	recorder := m.recorder
	m.mu.RUnlock()
	if !ok || recorder == nil {
		return
	}

	reporter, ok := entry.adapter.(provider.SessionReporter)
	if !ok {
		return
	}
	session, ok := reporter.Session(threadID)
	if !ok || session.ProviderSessionID == "" {
		return
	}

	m.mu.Lock()
	if current, live := m.threads[threadID]; live {
		current.session.ProviderSessionID = session.ProviderSessionID
	}
	m.mu.Unlock()

	recorder.NoteProviderSession(threadID, session.ProviderSessionID)
}

func (m *Manager) Send(ctx context.Context, in domain.SendTurnInput) error {
	m.mu.RLock()
	expand := m.expandRefs
	m.mu.RUnlock()
	if expand != nil && len(in.Files) > 0 {
		if context, rest := expand(in.Files); context != "" {
			in.Text = context + "\n\n" + in.Text
			in.Files = rest
		}
	}
	logger.Infof("Manager", "Send: thread=%s turn=%s len=%d files=%d", in.ThreadID, in.TurnID, len(in.Text), len(in.Files))
	entry, err := m.lookup(in.ThreadID)
	if err != nil {
		logger.Errorf("Manager", "Send failed (lookup): %v", err)
		return err
	}
	// The CLI process can be gone while the thread is still registered: an
	// interrupt that had to kill it, or a crash. Revive it with its resume id
	// instead of failing every later message.
	if !entry.adapter.HasSession(in.ThreadID) {
		if err := m.revive(ctx, in.ThreadID, entry); err != nil {
			logger.Errorf("Manager", "Send failed (revive): %v", err)
			return err
		}
		if entry, err = m.lookup(in.ThreadID); err != nil {
			return err
		}
	}
	if handoff := m.takeHandoff(in.ThreadID); handoff != "" {
		in.Text = handoff + "\n\n--- Current request ---\n\n" + in.Text
	}
	if note := m.notes.take(in.ThreadID); note != "" {
		in.Text += "\n\n" + note
	}
	return entry.adapter.SendTurn(ctx, in)
}

func (m *Manager) revive(ctx context.Context, threadID string, entry *record) error {
	in := entry.input
	in.ThreadID = threadID
	if resume := entry.session.ProviderSessionID; resume != "" {
		in.Resume = resume
	}
	if in.Model == "" {
		in.Model = entry.session.Model
	}
	logger.Infof("Manager", "Reviving thread %s (driver=%s resume=%s)", threadID, entry.session.Driver, in.Resume)
	m.mu.Lock()
	if current, ok := m.threads[threadID]; ok && current == entry {
		delete(m.threads, threadID)
	}
	m.mu.Unlock()
	_, err := m.StartResumed(ctx, entry.session.Driver, in, nil)
	return err
}

func (m *Manager) StartResumed(ctx context.Context, kind domain.DriverKind, in domain.SessionStartInput, transcript []domain.RuntimeEvent) (bool, error) {
	notice := "Started a new session; the conversation so far was passed to it"
	if in.Resume != "" {
		notice = "Previous session could not be resumed; the conversation so far was passed to a new one"
		_, err := m.Start(ctx, kind, in)
		if err == nil {
			return true, nil
		}
		logger.Warnf("Manager", "Resume of thread %s (session %s) failed: %v; starting fresh with its transcript", in.ThreadID, in.Resume, err)
		in.Resume = ""
		in.Fork = false
	}
	if _, err := m.Start(ctx, kind, in); err != nil {
		return false, err
	}
	if len(transcript) == 0 {
		transcript = m.History(in.ThreadID)
	}
	if hasConversation(transcript) {
		m.QueueHandoff(in.ThreadID, BuildHandoffPrompt(kind, kind, transcript, in.Cwd))
		m.RecordNotice(in.ThreadID, notice, "")
	}
	return false, nil
}

func hasConversation(events []domain.RuntimeEvent) bool {
	for _, event := range events {
		if event.Kind == domain.EventUserMessage || event.Kind == domain.EventAgentMessage {
			return true
		}
	}
	return false
}

func (m *Manager) QueueHandoff(threadID, text string) {
	if threadID == "" || text == "" {
		return
	}
	m.mu.Lock()
	if m.handoffs == nil {
		m.handoffs = make(map[string]string)
	}
	m.handoffs[threadID] = text
	m.mu.Unlock()
}

func (m *Manager) takeHandoff(threadID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	text := m.handoffs[threadID]
	delete(m.handoffs, threadID)
	return text
}

// RecordUserMessage injects a user.message event into the thread's history so
// that CoordinatorHistory() replays include user prompts. Without this, user
// messages only exist client-side and vanish on any component remount.
func (m *Manager) RecordUserMessage(threadID, turnID, text string, files ...domain.FileRef) {
	m.record(domain.RuntimeEvent{
		Kind:     domain.EventUserMessage,
		ThreadID: threadID,
		TurnID:   turnID,
		Text:     text,
		Files:    files,
		At:       time.Now().UnixMilli(),
	})
}

// RecordNotice injects a persistent divider into the thread's transcript, so a
// provider/model switch survives history reopen. Recorded through the same path
// as user messages, hence durable in JSONL and replayed on load.
func (m *Manager) RecordNotice(threadID, text, icon string) {
	if text == "" {
		return
	}
	m.record(domain.RuntimeEvent{
		Kind:     domain.EventNotice,
		ThreadID: threadID,
		Text:     text,
		Icon:     icon,
		At:       time.Now().UnixMilli(),
	})
}

// ThreadSession reports the live session for a thread, if any.
func (m *Manager) ThreadSession(threadID string) (domain.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.threads[threadID]
	if !ok {
		return domain.Session{}, false
	}
	return entry.session, true
}

// ProviderSessionID returns the CLI-native resume id for a thread, if known.
func (m *Manager) ProviderSessionID(threadID string) string {
	m.mu.RLock()
	entry, ok := m.threads[threadID]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	reporter, ok := entry.adapter.(provider.SessionReporter)
	if !ok {
		return ""
	}
	session, ok := reporter.Session(threadID)
	if !ok {
		return ""
	}
	return session.ProviderSessionID
}

// UpdateModel switches the model/options a live thread will use from now on.
// Adapters that apply it in place report true; others (long-lived procs with
// fixed launch args) report false so the caller restarts the thread instead.
func (m *Manager) UpdateModel(threadID, model string, options domain.ModelOptions) bool {
	m.mu.RLock()
	entry, ok := m.threads[threadID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	updater, ok := entry.adapter.(provider.ModelUpdater)
	if !ok {
		return false
	}
	applied := updater.UpdateModel(threadID, model, options)
	if applied {
		m.mu.Lock()
		if current, live := m.threads[threadID]; live {
			if model != "" {
				current.session.Model = model
				current.input.Model = model
			}
			if options != nil {
				current.input.Options = options
			}
		}
		m.mu.Unlock()
	}
	return applied
}


func (m *Manager) Interrupt(ctx context.Context, threadID string) error {
	entry, err := m.lookup(threadID)
	if err != nil {
		return err
	}
	return entry.adapter.InterruptTurn(ctx, threadID)
}

func (m *Manager) Respond(ctx context.Context, threadID, requestID string, decision domain.ApprovalDecision) error {
	entry, err := m.lookup(threadID)
	if err != nil {
		return err
	}
	return entry.adapter.RespondToApproval(ctx, threadID, requestID, domain.NormalizeDecision(decision))
}

func (m *Manager) RespondQuestion(ctx context.Context, threadID, requestID string, answers []string) error {
	entry, err := m.lookup(threadID)
	if err != nil {
		return err
	}
	return entry.adapter.RespondToQuestion(ctx, threadID, requestID, answers)
}

var ErrTurnRemovalUnsupported = errors.New("provider cannot edit its own session")

func (m *Manager) RemoveProviderTurn(kind domain.DriverKind, account, sessionID, text string, occurrence int) error {
	adapter, err := m.adapterFor(kind, account)
	if err != nil {
		return err
	}
	remover, ok := adapter.(provider.TurnRemover)
	if !ok {
		return ErrTurnRemovalUnsupported
	}
	return remover.RemoveTurn(sessionID, text, occurrence)
}

func (m *Manager) MoveProviderSession(kind domain.DriverKind, account, sessionID, cwd string) error {
	adapter, err := m.adapterFor(kind, account)
	if err != nil {
		return err
	}
	mover, ok := adapter.(provider.SessionMover)
	if !ok {
		return nil
	}
	return mover.MoveSession(sessionID, cwd)
}

func (m *Manager) ForgetTurn(threadID, turnID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transcript := m.history[threadID]
	drop := domain.ExchangeMask(transcript, turnID)
	kept := transcript[:0:0]
	for i, event := range transcript {
		if !drop[i] {
			kept = append(kept, event)
		}
	}
	m.history[threadID] = kept
}

func (m *Manager) TurnInProgress(threadID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, live := m.threads[threadID]; !live {
		return false
	}
	transcript := m.history[threadID]
	for i := len(transcript) - 1; i >= 0; i-- {
		switch transcript[i].Kind {
		case domain.EventTurnStarted:
			return true
		case domain.EventTurnCompleted, domain.EventTurnFailed, domain.EventSessionStopped:
			return false
		}
	}
	return false
}

func (m *Manager) Stop(ctx context.Context, threadID string) error {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	delete(m.threads, threadID)
	m.mu.Unlock()
	if !ok {
		logger.Infof("Manager", "Stop: thread=%s found=false", threadID)
		return nil
	}
	logger.Infof("Manager", "Stop: thread=%s found=true driver=%s cwd=%q", threadID, entry.session.Driver, entry.session.Cwd)
	return entry.adapter.StopSession(ctx, threadID)
}

// StopAll tears down every adapter. Called on app shutdown so no agent CLI
// outlives the window.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	adapters := make([]provider.Adapter, 0, len(m.adapters)+len(m.retired))
	for _, adapter := range m.adapters {
		adapters = append(adapters, adapter)
	}
	adapters = append(adapters, m.retired...)
	m.retired = nil
	m.adapters = make(map[adapterKey]provider.Adapter)
	m.threads = make(map[string]*record)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, adapter := range adapters {
		wg.Add(1)
		go func(adapter provider.Adapter) {
			defer wg.Done()
			_ = adapter.StopAll(ctx)
		}(adapter)
	}
	wg.Wait()
}

func (m *Manager) Sessions() []domain.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Session, 0, len(m.threads))
	for _, entry := range m.threads {
		out = append(out, entry.session)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func (m *Manager) History(threadID string) []domain.RuntimeEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	transcript := m.history[threadID]
	out := make([]domain.RuntimeEvent, len(transcript))
	copy(out, transcript)
	return out
}

// HistorySnapshot returns the thread's transcript and the highest event seq
// it contains, so a client can apply only live events that come after it.
func (m *Manager) HistorySnapshot(threadID string) ([]domain.RuntimeEvent, uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	transcript := m.history[threadID]
	out := make([]domain.RuntimeEvent, len(transcript))
	copy(out, transcript)
	var last uint64
	for _, event := range out {
		if event.Seq > last {
			last = event.Seq
		}
	}
	return out, last
}

var ErrNoSession = errors.New("no active session")

func (m *Manager) lookup(threadID string) (*record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.threads[threadID]
	if !ok {
		return nil, fmt.Errorf("%w for thread %s", ErrNoSession, threadID)
	}
	return entry, nil
}
