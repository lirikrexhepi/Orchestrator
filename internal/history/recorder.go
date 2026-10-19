package history

import (
	"sync"

	"composer/internal/domain"
)

// Recorder routes live events into the right workspace on disk.
//
// The manager publishes events per thread and knows nothing about workspaces,
// so this holds the thread → workspace mapping that turns a stream of
// per-thread events into a session that can be reopened as a unit.
type Recorder struct {
	store *Store

	mu sync.RWMutex
	// workspaceOf maps a thread (agent or orchestrator) to its workspace.
	workspaceOf map[string]string
	// metas is the authoritative in-memory copy, rewritten whenever it changes.
	metas map[string]*Meta
	// dirty marks workspaces whose meta needs flushing.
	dirty map[string]bool

	onProviderSession func(threadID, providerSessionID string)
	onChanged         func()

	// streamMu guards streaming, the per-thread delta being accumulated before
	// it is written. Deltas are persisted merged, one line per streamed item,
	// so transcripts stay small and replay does not re-assemble every token.
	streamMu  sync.Mutex
	streaming map[string]*pendingDelta
}

type pendingDelta struct {
	workspaceID string
	event       domain.RuntimeEvent
}

func NewRecorder(store *Store) *Recorder {
	return &Recorder{
		store:       store,
		workspaceOf: make(map[string]string),
		metas:       make(map[string]*Meta),
		dirty:       make(map[string]bool),
		streaming:   make(map[string]*pendingDelta),
	}
}

// OnChanged registers a callback that fires whenever sessions or tasks change.
func (r *Recorder) OnChanged(cb func()) {
	r.mu.Lock()
	r.onChanged = cb
	r.mu.Unlock()
}

func (r *Recorder) notifyChanged() {
	r.mu.RLock()
	cb := r.onChanged
	r.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// OpenWorkspace registers a workspace and the orchestrator thread that produced
// it, so both are recorded together from the first event.
func (r *Recorder) OpenWorkspace(workspace domain.Workspace, coordinatorThreadID string) {
	r.mu.Lock()
	meta := r.metaFor(workspace.ID)
	if meta.Workspace.Cwd != "" {
		workspace.Cwd = meta.Workspace.Cwd
	}
	meta.Workspace = workspace
	if coordinatorThreadID != "" {
		meta.CoordinatorThreadID = coordinatorThreadID
		r.workspaceOf[coordinatorThreadID] = workspace.ID
	}
	snapshot := *meta
	r.mu.Unlock()

	_ = r.store.SaveMeta(snapshot)
	r.notifyChanged()
}

func (r *Recorder) metaFor(workspaceID string) *Meta {
	if meta, ok := r.metas[workspaceID]; ok {
		return meta
	}
	meta := &Meta{Resume: make(map[string]string)}
	if stored, err := r.store.LoadMeta(workspaceID); err == nil && stored.Workspace.ID == workspaceID {
		*meta = stored
		if meta.Resume == nil {
			meta.Resume = make(map[string]string)
		}
	}
	meta.Workspace.ID = workspaceID
	r.metas[workspaceID] = meta
	return meta
}

func keepKnown(existing, next domain.Task) domain.Task {
	if next.Account == "" {
		next.Account = existing.Account
	}
	if next.Model == "" {
		next.Model = existing.Model
	}
	if len(next.Models) == 0 {
		next.Models = existing.Models
	}
	if len(next.Drivers) == 0 {
		next.Drivers = existing.Drivers
	}
	if next.Options == nil {
		next.Options = existing.Options
	}
	if next.Permission == "" {
		next.Permission = existing.Permission
	}
	if next.Worktree == nil {
		next.Worktree = existing.Worktree
	}
	if next.Cwd == "" {
		next.Cwd = existing.Cwd
	}
	if existing.TitleSource != "" && next.TitleSource == "" {
		next.Title = existing.Title
		next.TitleSource = existing.TitleSource
		next.TitleTurn = existing.TitleTurn
	}
	if existing.UserTurns > next.UserTurns {
		next.UserTurns = existing.UserTurns
	}
	return next
}

// TrackTask binds an agent thread to its workspace and records the task.
func (r *Recorder) TrackTask(task domain.Task) {
	if task.WorkspaceID == "" || task.ThreadID == "" {
		return
	}

	r.mu.Lock()
	meta := r.metaFor(task.WorkspaceID)
	r.workspaceOf[task.ThreadID] = task.WorkspaceID

	replaced := false
	for i, existing := range meta.Tasks {
		if existing.ThreadID == task.ThreadID {
			meta.Tasks[i] = keepKnown(existing, task)
			replaced = true
			break
		}
	}
	if !replaced {
		meta.Tasks = append(meta.Tasks, task)
	}
	snapshot := *meta
	r.mu.Unlock()

	_ = r.store.SaveMeta(snapshot)
	r.notifyChanged()
}

// Record persists one event against whichever workspace owns its thread.
//
// Events on unknown threads are dropped rather than stored loose: a thread with
// no workspace has nothing to be reopened as, and writing it would create
// directories that List could never describe.
func (r *Recorder) Record(threadID string, event domain.RuntimeEvent) {
	r.mu.RLock()
	workspaceID, ok := r.workspaceOf[threadID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	if isMergeableDelta(event) {
		r.streamMu.Lock()
		pending := r.streaming[threadID]
		if pending != nil && pending.workspaceID == workspaceID && continues(pending.event, event) {
			pending.event.Text += event.Text
			r.streamMu.Unlock()
			return
		}
		r.streaming[threadID] = &pendingDelta{workspaceID: workspaceID, event: event}
		r.streamMu.Unlock()
		if pending != nil {
			_ = r.store.Append(pending.workspaceID, threadID, pending.event)
		}
		return
	}

	r.flushStream(threadID)
	_ = r.store.Append(workspaceID, threadID, event)

	// A finished turn is a natural durability point, and cheap: the buffer is
	// usually near-empty by then. Turn starts and session starts are flushed
	// too so a crash mid-turn loses only streamed tokens, never the prompt
	// that started the turn or the resume id that makes it resumable.
	switch event.Kind {
	case domain.EventTurnCompleted, domain.EventTurnFailed, domain.EventSessionStopped,
		domain.EventUserMessage, domain.EventSessionStarted, domain.EventNotice:
		_ = r.store.Flush(workspaceID)
		if event.Kind == domain.EventTurnCompleted || event.Kind == domain.EventTurnFailed || event.Kind == domain.EventSessionStopped {
			r.flushMeta(workspaceID)
		}
	}
}

func isMergeableDelta(event domain.RuntimeEvent) bool {
	return event.Delta && (event.Kind == domain.EventAgentMessage || event.Kind == domain.EventAgentThought)
}

func continues(prev, next domain.RuntimeEvent) bool {
	return prev.Kind == next.Kind && prev.TurnID == next.TurnID && prev.ItemID == next.ItemID
}

// flushStream writes the thread's accumulated delta, if any.
func (r *Recorder) flushStream(threadID string) {
	r.streamMu.Lock()
	pending := r.streaming[threadID]
	delete(r.streaming, threadID)
	r.streamMu.Unlock()
	if pending != nil {
		_ = r.store.Append(pending.workspaceID, threadID, pending.event)
	}
}

func (r *Recorder) flushAllStreams() {
	r.streamMu.Lock()
	threads := make([]string, 0, len(r.streaming))
	for threadID := range r.streaming {
		threads = append(threads, threadID)
	}
	r.streamMu.Unlock()
	for _, threadID := range threads {
		r.flushStream(threadID)
	}
}

// OnProviderSession registers a callback invoked whenever a CLI session ID is recorded.
func (r *Recorder) OnProviderSession(cb func(threadID, providerSessionID string)) {
	r.mu.Lock()
	r.onProviderSession = cb
	r.mu.Unlock()
}

// NoteProviderSession stores the id needed to resume a thread in a later run.
func (r *Recorder) NoteProviderSession(threadID, providerSessionID string) {
	if providerSessionID == "" {
		return
	}

	r.mu.Lock()
	cb := r.onProviderSession
	workspaceID, ok := r.workspaceOf[threadID]
	if !ok {
		r.mu.Unlock()
		if cb != nil {
			cb(threadID, providerSessionID)
		}
		return
	}
	meta, ok := r.metas[workspaceID]
	if !ok {
		r.mu.Unlock()
		if cb != nil {
			cb(threadID, providerSessionID)
		}
		return
	}
	if meta.Resume == nil {
		meta.Resume = make(map[string]string)
	}
	if meta.Resume[threadID] == providerSessionID {
		r.mu.Unlock()
		return
	}
	meta.Resume[threadID] = providerSessionID
	r.dirty[workspaceID] = true
	snapshot := *meta
	r.mu.Unlock()

	if err := r.store.SaveMeta(snapshot); err != nil {
		r.mu.Lock()
		r.dirty[workspaceID] = true
		r.mu.Unlock()
	} else {
		r.mu.Lock()
		if r.metas[workspaceID] == meta {
			delete(r.dirty, workspaceID)
		}
		r.mu.Unlock()
	}

	if cb != nil {
		cb(threadID, providerSessionID)
	}
}

func (r *Recorder) RemoveTurn(threadID, turnID string) error {
	r.flushStream(threadID)
	r.mu.RLock()
	workspaceID, ok := r.workspaceOf[threadID]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return r.store.RemoveTurn(workspaceID, threadID, turnID)
}

func (r *Recorder) ForgetProviderSession(threadID string) {
	r.mu.Lock()
	workspaceID, ok := r.workspaceOf[threadID]
	meta := r.metas[workspaceID]
	if !ok || meta == nil || meta.Resume[threadID] == "" {
		r.mu.Unlock()
		return
	}
	delete(meta.Resume, threadID)
	snapshot := *meta
	r.mu.Unlock()
	_ = r.store.SaveMeta(snapshot)
}

// UpdateTaskState keeps the stored task list in step with the live one, so a
// reopened session shows what finished rather than what merely started.
func (r *Recorder) UpdateTaskState(threadID string, state domain.TaskState, summary string) {
	r.mu.Lock()
	workspaceID, ok := r.workspaceOf[threadID]
	if !ok {
		r.mu.Unlock()
		return
	}
	meta, ok := r.metas[workspaceID]
	if !ok {
		r.mu.Unlock()
		return
	}
	for i := range meta.Tasks {
		if meta.Tasks[i].ThreadID != threadID {
			continue
		}
		if state != "" {
			meta.Tasks[i].State = state
		}
		if summary != "" {
			meta.Tasks[i].Summary = summary
		}
		r.dirty[workspaceID] = true
		break
	}
	r.mu.Unlock()
}

func (r *Recorder) touchTask(workspaceID, threadID string, at int64) {
	if at <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	meta, ok := r.metas[workspaceID]
	if !ok {
		return
	}
	for i := range meta.Tasks {
		if meta.Tasks[i].ThreadID == threadID && at > meta.Tasks[i].UpdatedAt {
			meta.Tasks[i].UpdatedAt = at
			r.dirty[workspaceID] = true
		}
	}
	if at > meta.Workspace.UpdatedAt {
		meta.Workspace.UpdatedAt = at
		r.dirty[workspaceID] = true
	}
}

// Touch updates a workspace's last-activity stamp so history sorts by use.
func (r *Recorder) Touch(workspaceID string, at int64) {
	r.mu.Lock()
	if meta, ok := r.metas[workspaceID]; ok && at > meta.Workspace.UpdatedAt {
		meta.Workspace.UpdatedAt = at
		r.dirty[workspaceID] = true
	}
	r.mu.Unlock()
}

// Restore rehydrates in-memory routing from a stored workspace so a restart
// keeps recording to the same workspace instead of dropping events as unknown
// threads. SQLite remains only the metadata index; JSONL remains canonical.
func (r *Recorder) Restore(meta Meta) {
	if meta.Workspace.ID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := meta
	if stored.Resume == nil {
		stored.Resume = make(map[string]string)
	}
	r.metas[meta.Workspace.ID] = &stored
	if meta.CoordinatorThreadID != "" {
		r.workspaceOf[meta.CoordinatorThreadID] = meta.Workspace.ID
	}
	for _, task := range meta.Tasks {
		if task.ThreadID != "" {
			r.workspaceOf[task.ThreadID] = meta.Workspace.ID
		}
	}
	for threadID := range meta.Resume {
		if _, ok := r.workspaceOf[threadID]; !ok {
			r.workspaceOf[threadID] = meta.Workspace.ID
		}
	}
}

// ResumeID reports the provider session id stored for a thread, which is what
// a CLI needs to continue that thread's conversation.
func (r *Recorder) ResumeID(threadID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	workspaceID, ok := r.workspaceOf[threadID]
	if !ok {
		return ""
	}
	if meta, ok := r.metas[workspaceID]; ok && meta.Resume != nil {
		return meta.Resume[threadID]
	}
	return ""
}

// WorkspaceOf reports which workspace a thread belongs to.
func (r *Recorder) WorkspaceOf(threadID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.workspaceOf[threadID]
	return id, ok
}

func (r *Recorder) WorkspaceCwd(threadID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.workspaceOf[threadID]
	if !ok {
		return ""
	}
	if meta, ok := r.metas[id]; ok {
		return meta.Workspace.Cwd
	}
	return ""
}

func (r *Recorder) flushMeta(workspaceID string) {
	r.mu.Lock()
	if !r.dirty[workspaceID] {
		r.mu.Unlock()
		return
	}
	meta, ok := r.metas[workspaceID]
	if !ok {
		delete(r.dirty, workspaceID)
		r.mu.Unlock()
		return
	}
	snapshot := *meta
	r.mu.Unlock()

	if err := r.store.SaveMeta(snapshot); err != nil {
		return
	}
	r.mu.Lock()
	if current, ok := r.metas[workspaceID]; ok && current == meta {
		delete(r.dirty, workspaceID)
	}
	r.mu.Unlock()
}

// FlushAll makes every open workspace durable without closing the store, so a
// session can be left behind mid-run with nothing still buffered.
func (r *Recorder) FlushAll() error {
	r.flushAllStreams()
	r.mu.Lock()
	pending := make([]Meta, 0, len(r.dirty))
	ids := make([]string, 0, len(r.dirty))
	for workspaceID := range r.dirty {
		if meta, ok := r.metas[workspaceID]; ok {
			pending = append(pending, *meta)
			ids = append(ids, workspaceID)
		}
	}
	r.mu.Unlock()

	for i, meta := range pending {
		if err := r.store.SaveMeta(meta); err != nil {
			continue
		}
		r.mu.Lock()
		delete(r.dirty, ids[i])
		r.mu.Unlock()
	}
	return r.store.Flush("")
}

// Close flushes every pending meta and transcript.
func (r *Recorder) Close() error {
	r.flushAllStreams()
	r.mu.Lock()
	pending := make([]Meta, 0, len(r.dirty))
	ids := make([]string, 0, len(r.dirty))
	for workspaceID := range r.dirty {
		if meta, ok := r.metas[workspaceID]; ok {
			pending = append(pending, *meta)
			ids = append(ids, workspaceID)
		}
	}
	r.mu.Unlock()

	for i, meta := range pending {
		if err := r.store.SaveMeta(meta); err != nil {
			continue
		}
		r.mu.Lock()
		delete(r.dirty, ids[i])
		r.mu.Unlock()
	}
	return r.store.Close()
}

// RecordCoordinator stores the orchestrator conversation that produced a
// workspace, and binds the thread so any later turns land in the same place.
func (r *Recorder) RecordCoordinator(workspaceID, threadID string, events []domain.RuntimeEvent) {
	if workspaceID == "" || threadID == "" {
		return
	}

	r.mu.Lock()
	meta, ok := r.metas[workspaceID]
	if !ok {
		meta = &Meta{Resume: make(map[string]string)}
		meta.Workspace.ID = workspaceID
		r.metas[workspaceID] = meta
	}
	meta.CoordinatorThreadID = threadID
	r.workspaceOf[threadID] = workspaceID
	snapshot := *meta
	r.mu.Unlock()

	for _, event := range events {
		_ = r.store.Append(workspaceID, threadID, event)
	}
	_ = r.store.Flush(workspaceID)
	_ = r.store.SaveMeta(snapshot)
	r.notifyChanged()
}

// Forget drops a workspace from memory and disk.
func (r *Recorder) Forget(workspaceID string) error {
	r.mu.Lock()
	for threadID, id := range r.workspaceOf {
		if id == workspaceID {
			delete(r.workspaceOf, threadID)
		}
	}
	delete(r.metas, workspaceID)
	delete(r.dirty, workspaceID)
	r.mu.Unlock()

	err := r.store.Delete(workspaceID)
	r.notifyChanged()
	return err
}

// ForgetCliTask drops an individual task from memory and disk.
func (r *Recorder) ForgetCliTask(workspaceID, threadID string) error {
	rootID := RootThreadID(threadID)
	r.mu.Lock()
	delete(r.workspaceOf, threadID)
	delete(r.workspaceOf, rootID)
	if meta, ok := r.metas[workspaceID]; ok {
		var remaining []domain.Task
		for _, t := range meta.Tasks {
			if t.ThreadID != threadID && t.ThreadID != rootID && RootThreadID(t.ThreadID) != rootID {
				remaining = append(remaining, t)
			}
		}
		meta.Tasks = remaining
		if meta.Resume != nil {
			delete(meta.Resume, threadID)
			delete(meta.Resume, rootID)
		}
		if len(meta.Tasks) == 0 {
			delete(r.metas, workspaceID)
			delete(r.dirty, workspaceID)
		}
	}
	r.mu.Unlock()

	err := r.store.DeleteTask(workspaceID, threadID)
	r.notifyChanged()
	return err
}

func (r *Recorder) SetChatArchived(workspaceID, threadID string, archived bool) error {
	if workspaceID == "" {
		return nil
	}
	rootID := RootThreadID(threadID)
	apply := func(meta *Meta) bool {
		changed := false
		matched := false
		for i := range meta.Tasks {
			t := &meta.Tasks[i]
			if threadID != "" && t.ThreadID != threadID && t.ThreadID != rootID && RootThreadID(t.ThreadID) != rootID {
				continue
			}
			matched = true
			if t.Archived != archived {
				t.Archived = archived
				changed = true
			}
		}
		if (threadID == "" || !matched) && meta.Workspace.Archived != archived {
			meta.Workspace.Archived = archived
			changed = true
		}
		return changed
	}

	r.mu.Lock()
	if meta, ok := r.metas[workspaceID]; ok {
		if apply(meta) {
			r.dirty[workspaceID] = true
		}
		r.mu.Unlock()
		r.flushMeta(workspaceID)
		r.notifyChanged()
		return nil
	}
	r.mu.Unlock()

	meta, err := r.store.LoadMeta(workspaceID)
	if err != nil {
		return err
	}
	if apply(&meta) {
		if err := r.store.SaveMeta(meta); err != nil {
			return err
		}
	}
	r.notifyChanged()
	return nil
}

func (r *Recorder) MoveWorkspace(workspaceID, cwd string) error {
	r.mu.Lock()
	meta := r.metaFor(workspaceID)
	meta.Workspace.Cwd = cwd
	for i := range meta.Tasks {
		if meta.Tasks[i].Worktree == nil {
			meta.Tasks[i].Cwd = cwd
		}
	}
	snapshot := *meta
	r.mu.Unlock()
	if err := r.store.SaveMeta(snapshot); err != nil {
		return err
	}
	r.notifyChanged()
	return nil
}
