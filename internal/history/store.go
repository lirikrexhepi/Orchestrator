// Package history persists sessions so a workspace can be reopened after the
// app restarts.
//
// The unit of storage is the workspace, not the thread. Composer spawns several
// agents from one orchestrator request, and reading that request back means
// reading the orchestrator's side of it together with every agent it started —
// so all of them live in one directory and load as a set.
//
// Layout, one directory per workspace:
//
//	<root>/<workspaceID>/meta.json           workspace, tasks, resume ids
//	<root>/<workspaceID>/<threadID>.jsonl    one event per line
//
// JSONL for the transcripts because events only ever append: a line can be
// written as it is published without rewriting the file, a truncated tail from
// a crash costs one event rather than the session, and no migration is needed
// when the event shape changes.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"composer/internal/domain"
)

const (
	metaFile      = "meta.json"
	transcriptExt = ".jsonl"
	// Long transcripts are read back in full, so a ceiling keeps a runaway
	// stream from becoming unloadable. It applies to streamed deltas only:
	// user messages, tool calls and turn boundaries are always written, and
	// deltas are already merged per item by the recorder.
	maxEventsPerThread = 20000
)

// Meta is everything about a workspace except the transcripts themselves.
type Meta struct {
	Workspace domain.Workspace `json:"workspace"`
	Tasks     []domain.Task    `json:"tasks"`
	// CoordinatorThreadID is the orchestrator conversation that produced this
	// workspace. Stored explicitly because it is what distinguishes a Composer
	// session from a single agent's chat log.
	CoordinatorThreadID string `json:"coordinatorThreadId,omitempty"`
	// Resume maps a thread id to the provider's own session id, which is what a
	// CLI needs to continue a conversation it started in an earlier run.
	Resume map[string]string `json:"resume,omitempty"`
	// Version allows a later format change to be detected rather than guessed.
	Version int `json:"version"`
}

const currentVersion = 1

// Session is a workspace loaded back in full: its metadata plus every
// transcript, keyed by thread id.
type Session struct {
	Meta        Meta                             `json:"meta"`
	Transcripts map[string][]domain.RuntimeEvent `json:"transcripts"`
}

// Store owns the on-disk history tree.
//
// Writes are serialised through a single mutex. Event append is the hot path —
// it happens for every streamed token — so each append writes one line to an
// already-open file rather than reopening or rewriting anything.
type Store struct {
	root string

	mu       sync.Mutex
	open     map[string]*openFile
	counts   map[string]int
	disabled bool
}

type openFile struct {
	file   *os.File
	writer *bufio.Writer
}

// New opens (and creates) the history tree at root.
//
// A store that cannot create its directory is returned disabled rather than as
// an error: failing to persist history must never stop the app from running
// agents, which is the thing the user actually asked for.
func New(root string) *Store {
	store := &Store{
		root:   root,
		open:   make(map[string]*openFile),
		counts: make(map[string]int),
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		store.disabled = true
	}
	return store
}

// Root reports where history is being written, for diagnostics.
func (s *Store) Root() string { return s.root }

func (s *Store) workspaceDir(workspaceID string) string {
	return filepath.Join(s.root, safeName(workspaceID))
}

// SaveMeta writes a workspace's metadata, replacing any previous copy.
//
// Written whole and atomically: unlike the transcripts this is a small document
// that changes shape as tasks complete, and a half-written meta.json would make
// the whole workspace unreadable.
func (s *Store) SaveMeta(meta Meta) error {
	if s.disabled {
		return nil
	}
	if meta.Workspace.ID == "" {
		return errors.New("workspace id is required")
	}
	meta.Version = currentVersion

	dir := s.workspaceDir(meta.Workspace.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, metaFile), payload)
}

// LoadMeta reads one workspace's metadata, recovering from .tmp or transcripts if damaged.
func (s *Store) LoadMeta(workspaceID string) (Meta, error) {
	var meta Meta
	metaPath := filepath.Join(s.workspaceDir(workspaceID), metaFile)
	tmpPath := metaPath + ".tmp"

	payload, err := os.ReadFile(metaPath)
	if err != nil || len(payload) == 0 {
		// Try reading temp file if main file is missing or empty
		if tmpBytes, tmpErr := os.ReadFile(tmpPath); tmpErr == nil && len(tmpBytes) > 0 {
			payload = tmpBytes
			err = nil
		}
	}

	valid := false
	if err == nil {
		payload = bytes.TrimPrefix(payload, []byte("\xef\xbb\xbf"))
		payload = bytes.Trim(payload, "\x00\r\n\t ")
		if len(payload) > 0 && json.Unmarshal(payload, &meta) == nil && meta.Workspace.ID != "" {
			valid = true
		}
	}

	if !valid {
		// Attempt auto-healing from transcripts so chats never disappear on crashes
		healed, hErr := s.healMetaFromTranscripts(workspaceID)
		if hErr == nil {
			return healed, nil
		}
		if err != nil {
			return meta, err
		}
		return meta, errors.New("invalid or empty metadata")
	}

	metaModified := false
	if meta.Workspace.ID == "" {
		meta.Workspace.ID = workspaceID
		metaModified = true
	}

	// Resolve actual repository path if Cwd points to a worktree or is empty
	if meta.Workspace.Cwd != "" && strings.Contains(strings.ToLower(meta.Workspace.Cwd), "worktrees") {
		if repo := resolveRepoFromWorktree(meta.Workspace.Cwd); repo != "" {
			meta.Workspace.Cwd = repo
			metaModified = true
		}
	}
	if meta.Workspace.Cwd == "" {
		for _, t := range meta.Tasks {
			if t.Worktree != nil && t.Worktree.Path != "" {
				if repo := resolveRepoFromWorktree(t.Worktree.Path); repo != "" {
					meta.Workspace.Cwd = repo
					metaModified = true
					break
				}
				meta.Workspace.Cwd = t.Worktree.Path
				metaModified = true
				break
			}
		}
		if meta.Workspace.Cwd == "" {
			if wd, gErr := os.Getwd(); gErr == nil {
				meta.Workspace.Cwd = wd
				metaModified = true
			}
		}
	}

	// Ensure title is present and meaningful
	if meta.Workspace.Title == "" || meta.Workspace.Title == "Session" {
		if meta.Workspace.Prompt != "" {
			meta.Workspace.Title = shortenText(meta.Workspace.Prompt, 40)
			metaModified = true
		} else if len(meta.Tasks) > 0 {
			for _, t := range meta.Tasks {
				if t.Title != "" && t.Title != "Agent" && t.Title != "Session" {
					meta.Workspace.Title = t.Title
					metaModified = true
					break
				} else if t.Prompt != "" {
					meta.Workspace.Title = shortenText(t.Prompt, 40)
					metaModified = true
					break
				}
			}
		}
	}

	// Ensure UpdatedAt is accurate for chronological sorting
	if meta.Workspace.UpdatedAt == 0 {
		for _, t := range meta.Tasks {
			if t.UpdatedAt > meta.Workspace.UpdatedAt {
				meta.Workspace.UpdatedAt = t.UpdatedAt
				metaModified = true
			}
			if t.CreatedAt > meta.Workspace.UpdatedAt {
				meta.Workspace.UpdatedAt = t.CreatedAt
				metaModified = true
			}
		}
		if meta.Workspace.UpdatedAt == 0 && meta.Workspace.CreatedAt > 0 {
			meta.Workspace.UpdatedAt = meta.Workspace.CreatedAt
			metaModified = true
		}
	}

	// Enrich tasks with multi-model / multi-driver history if not already recorded
	dir := s.workspaceDir(workspaceID)
	for i := range meta.Tasks {
		t := &meta.Tasks[i]
		if len(t.Drivers) <= 1 {
			existingD := t.Drivers
			if len(existingD) == 0 && t.Driver != "" {
				existingD = []domain.DriverKind{t.Driver}
			}
			existingM := t.Models
			if len(existingM) == 0 && t.Model != "" {
				existingM = []string{t.Model}
			}
			tPath := filepath.Join(dir, safeName(t.ThreadID)+transcriptExt)
			d, m := scanTranscriptDriversAndModels(tPath, existingD, existingM)
			if len(d) > len(t.Drivers) || len(m) > len(t.Models) {
				t.Drivers = d
				t.Models = m
				metaModified = true
			}
		}
	}
	if metaModified {
		_ = s.SaveMeta(meta)
	}

	return meta, nil
}

// Append records one event on a thread's transcript.
//
// Buffered rather than synced per line: an event is cheap to lose and expensive
// to fsync, and the flush on Close covers the ordinary exit. A crash costs at
// most the tail of a transcript, never the workspace.
func (s *Store) Append(workspaceID, threadID string, event domain.RuntimeEvent) error {
	if s.disabled || workspaceID == "" || threadID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := workspaceID + "/" + threadID
	if event.Delta && s.counts[key] >= maxEventsPerThread {
		return nil
	}

	handle, err := s.handleLocked(workspaceID, threadID)
	if err != nil {
		return err
	}

	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := handle.writer.Write(append(line, '\n')); err != nil {
		return err
	}
	s.counts[key]++
	return nil
}

func (s *Store) handleLocked(workspaceID, threadID string) (*openFile, error) {
	key := workspaceID + "/" + threadID
	if handle, ok := s.open[key]; ok {
		return handle, nil
	}

	dir := s.workspaceDir(workspaceID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, safeName(threadID)+transcriptExt)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	handle := &openFile{file: file, writer: bufio.NewWriterSize(file, 32*1024)}
	s.open[key] = handle
	return handle, nil
}

// Flush pushes buffered events for a workspace to disk. Called when a turn ends
// so a session that is merely idle is already durable.
func (s *Store) Flush(workspaceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := workspaceID + "/"
	var firstErr error
	for key, handle := range s.open {
		if workspaceID != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		if err := handle.writer.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
		if handle.file != nil {
			_ = handle.file.Sync()
		}
	}
	return firstErr
}

// Close flushes and releases every open transcript.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for key, handle := range s.open {
		if err := handle.writer.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
		if handle.file != nil {
			_ = handle.file.Sync()
		}
		if err := handle.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(s.open, key)
	}
	return firstErr
}

// RootThreadID extracts the base thread ID before any "-cont-" suffix.
func RootThreadID(threadID string) string {
	if idx := strings.Index(threadID, "-cont-"); idx != -1 {
		return threadID[:idx]
	}
	return threadID
}

// MergeContinuations merges continuation tasks into their original thread tasks.
// Returns true if any continuations were merged, and a map of contThreadID -> rootThreadID.
func MergeContinuations(meta *Meta) (bool, map[string]string) {
	if meta == nil || len(meta.Tasks) == 0 {
		return false, nil
	}

	contMap := make(map[string]string) // contThreadID -> rootThreadID
	var hasCont bool
	for _, t := range meta.Tasks {
		root := RootThreadID(t.ThreadID)
		if root != t.ThreadID {
			hasCont = true
			contMap[t.ThreadID] = root
		}
	}
	if !hasCont {
		return false, nil
	}

	// Group tasks by root thread ID, preserving order
	type threadGroup struct {
		rootID string
		tasks  []domain.Task
	}
	groupOrder := make([]string, 0, len(meta.Tasks))
	groups := make(map[string]*threadGroup)

	for _, t := range meta.Tasks {
		root := RootThreadID(t.ThreadID)
		g, ok := groups[root]
		if !ok {
			g = &threadGroup{rootID: root}
			groups[root] = g
			groupOrder = append(groupOrder, root)
		}
		g.tasks = append(g.tasks, t)
	}

	mergedTasks := make([]domain.Task, 0, len(groupOrder))
	for _, rootID := range groupOrder {
		g := groups[rootID]
		if len(g.tasks) == 1 {
			mergedTasks = append(mergedTasks, g.tasks[0])
			continue
		}

		// Multiple tasks for this root thread: merge into the primary (first/root) task
		primary := g.tasks[0]
		primary.ThreadID = rootID

		// Latest task gives the current driver, model, options, state, and updated timestamp
		latest := g.tasks[len(g.tasks)-1]
		if latest.Driver != "" {
			primary.Driver = latest.Driver
		}
		if latest.Model != "" {
			primary.Model = latest.Model
		}
		if latest.Options != nil {
			primary.Options = latest.Options
		}
		if latest.State != "" {
			primary.State = latest.State
		}
		if latest.UpdatedAt > primary.UpdatedAt {
			primary.UpdatedAt = latest.UpdatedAt
		}
		if latest.Worktree != nil {
			primary.Worktree = latest.Worktree
		}

		// Collect all drivers and models across the group
		var allDrivers []domain.DriverKind
		var allModels []string
		for _, t := range g.tasks {
			if len(t.Drivers) > 0 {
				for _, d := range t.Drivers {
					if d != "" && !containsDriver(allDrivers, d) {
						allDrivers = append(allDrivers, d)
					}
				}
			} else if t.Driver != "" && !containsDriver(allDrivers, t.Driver) {
				allDrivers = append(allDrivers, t.Driver)
			}

			if len(t.Models) > 0 {
				for _, m := range t.Models {
					if m != "" && !containsString(allModels, m) {
						allModels = append(allModels, m)
					}
				}
			} else if t.Model != "" && !containsString(allModels, t.Model) {
				allModels = append(allModels, t.Model)
			}
		}
		if len(allDrivers) > 0 {
			primary.Drivers = allDrivers
		}
		if len(allModels) > 0 {
			primary.Models = allModels
		}

		// Update resume map
		if meta.Resume != nil {
			for _, t := range g.tasks {
				if rID, ok := meta.Resume[t.ThreadID]; ok && rID != "" {
					meta.Resume[rootID] = rID
					if t.ThreadID != rootID {
						delete(meta.Resume, t.ThreadID)
					}
				}
			}
		}

		mergedTasks = append(mergedTasks, primary)
	}

	meta.Tasks = mergedTasks
	return true, contMap
}

// List reports every stored workspace, newest first.
//
// Reads only meta.json per workspace: the panel shows titles and timestamps, and
// loading transcripts here would mean reading every event ever recorded to draw
// a list.
func (s *Store) List() ([]Meta, error) {
	if s.disabled {
		return nil, nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]Meta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.LoadMeta(entry.Name())
		if err != nil {
			// A directory without readable metadata is a partial write from a
			// crash; skipping it keeps the rest of history usable.
			continue
		}
		MergeContinuations(&meta)
		out = append(out, meta)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Workspace.UpdatedAt > out[j].Workspace.UpdatedAt
	})
	return out, nil
}

// Load reads a whole workspace back: metadata plus every transcript in it.
func (s *Store) Load(workspaceID string) (Session, error) {
	var loaded Session
	// Buffered writes would otherwise be missing from a session reopened in the
	// same run that produced it.
	_ = s.Flush(workspaceID)

	meta, err := s.LoadMeta(workspaceID)
	if err != nil {
		return loaded, err
	}
	merged, contMap := MergeContinuations(&meta)
	loaded.Meta = meta
	loaded.Transcripts = make(map[string][]domain.RuntimeEvent)

	dir := s.workspaceDir(workspaceID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return loaded, err
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, transcriptExt) {
			continue
		}
		events, err := readTranscript(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		threadID := strings.TrimSuffix(name, transcriptExt)
		loaded.Transcripts[threadID] = events
	}

	// Merge continuation transcripts into root thread transcripts
	var filesToClean []string
	if contMap == nil {
		contMap = make(map[string]string)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, transcriptExt) {
			continue
		}
		tID := strings.TrimSuffix(name, transcriptExt)
		if idx := strings.Index(tID, "-cont-"); idx != -1 {
			rootID := tID[:idx]
			if _, exists := contMap[tID]; !exists {
				contMap[tID] = rootID
			}
		}
	}

	for contID, rootID := range contMap {
		contEvents, hasContEvents := loaded.Transcripts[contID]
		if hasContEvents {
			rootEvents := loaded.Transcripts[rootID]
			loaded.Transcripts[rootID] = append(rootEvents, contEvents...)
			delete(loaded.Transcripts, contID)

			contPath := filepath.Join(dir, safeName(contID)+transcriptExt)
			rootPath := filepath.Join(dir, safeName(rootID)+transcriptExt)
			if contBytes, rErr := os.ReadFile(contPath); rErr == nil && len(contBytes) > 0 {
				if f, aErr := os.OpenFile(rootPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); aErr == nil {
					_, _ = f.Write(contBytes)
					_ = f.Close()
				}
			}
			filesToClean = append(filesToClean, contPath)
		}
	}

	if merged || len(filesToClean) > 0 {
		for _, path := range filesToClean {
			_ = os.Remove(path)
		}
		_ = s.SaveMeta(loaded.Meta)
	}

	return loaded, nil
}

// DeleteTask removes an individual task and its transcript. If it was the only task,
// the entire workspace is deleted.
func (s *Store) DeleteTask(workspaceID, threadID string) error {
	if s.disabled || workspaceID == "" || threadID == "" {
		return nil
	}

	meta, err := s.LoadMeta(workspaceID)
	if err != nil {
		return err
	}
	MergeContinuations(&meta)

	if len(meta.Tasks) <= 1 {
		return s.Delete(workspaceID)
	}

	rootID := RootThreadID(threadID)
	var remaining []domain.Task
	found := false
	for _, t := range meta.Tasks {
		if t.ThreadID == threadID || t.ThreadID == rootID || RootThreadID(t.ThreadID) == rootID {
			found = true
			continue
		}
		remaining = append(remaining, t)
	}
	if !found {
		return nil
	}

	meta.Tasks = remaining
	if meta.Resume != nil {
		delete(meta.Resume, threadID)
		delete(meta.Resume, rootID)
	}

	s.mu.Lock()
	key := workspaceID + "/" + threadID
	rootKey := workspaceID + "/" + rootID
	for _, k := range []string{key, rootKey} {
		if h, ok := s.open[k]; ok {
			_ = h.writer.Flush()
			_ = h.file.Close()
			delete(s.open, k)
			delete(s.counts, k)
		}
	}
	s.mu.Unlock()

	dir := s.workspaceDir(workspaceID)
	_ = os.Remove(filepath.Join(dir, safeName(threadID)+transcriptExt))
	if rootID != threadID {
		_ = os.Remove(filepath.Join(dir, safeName(rootID)+transcriptExt))
	}

	return s.SaveMeta(meta)
}

func (s *Store) RemoveTurn(workspaceID, threadID, turnID string) error {
	if s.disabled || workspaceID == "" || threadID == "" || turnID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := workspaceID + "/" + threadID
	if handle, ok := s.open[key]; ok {
		_ = handle.writer.Flush()
		_ = handle.file.Close()
		delete(s.open, key)
	}

	path := filepath.Join(s.workspaceDir(workspaceID), safeName(threadID)+transcriptExt)
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines [][]byte
	var events []domain.RuntimeEvent
	for _, line := range bytes.Split(payload, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event domain.RuntimeEvent
		_ = json.Unmarshal(line, &event)
		lines = append(lines, line)
		events = append(events, event)
	}
	drop := domain.ExchangeMask(events, turnID)
	var out bytes.Buffer
	count := 0
	for i, line := range lines {
		if drop[i] {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
		count++
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.counts[key] = count
	return nil
}

// Delete removes a workspace and everything recorded under it.
func (s *Store) Delete(workspaceID string) error {
	if s.disabled || workspaceID == "" {
		return nil
	}

	s.mu.Lock()
	prefix := workspaceID + "/"
	for key, handle := range s.open {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		_ = handle.writer.Flush()
		_ = handle.file.Close()
		delete(s.open, key)
		delete(s.counts, key)
	}
	s.mu.Unlock()

	return os.RemoveAll(s.workspaceDir(workspaceID))
}

// readTranscript parses a JSONL transcript, tolerating a truncated final line.
//
// A crash mid-write leaves a partial line; dropping it recovers everything
// before it rather than failing the whole read.
func readTranscript(path string) ([]domain.RuntimeEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	events := make([]domain.RuntimeEvent, 0, 256)
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.Trim(string(line), "\r\n\t \x00")
			if trimmed != "" {
				var event domain.RuntimeEvent
				if json.Unmarshal([]byte(trimmed), &event) == nil {
					events = append(events, event)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return events, err
		}
	}
	return events, nil
}

// healMetaFromTranscripts reconstructs a valid Meta object from any .jsonl transcripts
// present in the workspace directory. This guarantees that even if meta.json was wiped,
// zero-filled, or corrupted by an abrupt crash/kill, the session remains visible and recoverable.
func (s *Store) healMetaFromTranscripts(workspaceID string) (Meta, error) {
	var meta Meta
	dir := s.workspaceDir(workspaceID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return meta, err
	}

	meta.Workspace.ID = workspaceID
	meta.Version = currentVersion
	meta.Resume = make(map[string]string)

	var allEvents []domain.RuntimeEvent
	taskMap := make(map[string]*domain.Task)
	var taskOrder []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, transcriptExt) {
			continue
		}
		tID := strings.TrimSuffix(name, transcriptExt)
		events, err := readTranscript(filepath.Join(dir, name))
		if err != nil || len(events) == 0 {
			continue
		}
		allEvents = append(allEvents, events...)

		if strings.HasPrefix(tID, "coordinator-") {
			meta.CoordinatorThreadID = tID
			continue
		}

		task := &domain.Task{
			ID:          "task-" + safeName(tID),
			WorkspaceID: workspaceID,
			ThreadID:    tID,
			State:       domain.TaskComplete,
		}
		for _, ev := range events {
			if ev.At > 0 {
				if task.CreatedAt == 0 || ev.At < task.CreatedAt {
					task.CreatedAt = ev.At
				}
				if ev.At > task.UpdatedAt {
					task.UpdatedAt = ev.At
				}
			}
			if ev.Kind == domain.EventUserMessage && task.Prompt == "" && ev.Text != "" {
				task.Prompt = ev.Text
				task.Title = shortenText(ev.Text, 35)
			}
			if ev.Driver != "" {
				dKind := domain.DriverKind(strings.ToLower(string(ev.Driver)))
				if task.Driver == "" {
					task.Driver = dKind
				}
				if !containsDriver(task.Drivers, dKind) {
					task.Drivers = append(task.Drivers, dKind)
				}
			}
			if ev.Kind == domain.EventTurnFailed {
				task.State = domain.TaskFailed
			}
		}
		if task.Title == "" {
			task.Title = "Agent"
		}
		taskMap[tID] = task
		taskOrder = append(taskOrder, tID)
	}

	if len(allEvents) == 0 {
		return meta, errors.New("no transcript events found to heal from")
	}

	for _, tID := range taskOrder {
		meta.Tasks = append(meta.Tasks, *taskMap[tID])
	}

	for _, ev := range allEvents {
		if ev.At > 0 {
			if meta.Workspace.CreatedAt == 0 || ev.At < meta.Workspace.CreatedAt {
				meta.Workspace.CreatedAt = ev.At
			}
			if ev.At > meta.Workspace.UpdatedAt {
				meta.Workspace.UpdatedAt = ev.At
			}
		}
		if ev.Kind == domain.EventUserMessage && meta.Workspace.Prompt == "" && ev.Text != "" {
			meta.Workspace.Prompt = ev.Text
			meta.Workspace.Title = shortenText(ev.Text, 35)
		}
	}

	if meta.Workspace.Title == "" {
		if len(meta.Tasks) > 0 && meta.Tasks[0].Title != "" {
			meta.Workspace.Title = meta.Tasks[0].Title
		} else {
			meta.Workspace.Title = "Session"
		}
	}

	if meta.Workspace.Cwd == "" {
		for _, t := range meta.Tasks {
			if t.Worktree != nil && t.Worktree.Path != "" {
				if repo := resolveRepoFromWorktree(t.Worktree.Path); repo != "" {
					meta.Workspace.Cwd = repo
					break
				}
			}
		}
		if meta.Workspace.Cwd == "" {
			if wd, err := os.Getwd(); err == nil {
				meta.Workspace.Cwd = wd
			}
		}
	}

	_ = s.SaveMeta(meta)
	return meta, nil
}

func shortenText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}
	if idx := strings.IndexAny(s, "\r\n"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// resolveRepoFromWorktree inspects a worktree directory's .git file or path to find
// the main repository root where the task originated.
func resolveRepoFromWorktree(wtPath string) string {
	if wtPath == "" {
		return ""
	}
	gitFile := filepath.Join(wtPath, ".git")
	data, err := os.ReadFile(gitFile)
	if err == nil {
		content := strings.TrimSpace(string(data))
		// Format: gitdir: C:/Users/PC/Projects/hobby/orchestrator/.git/worktrees/branch-name
		if idx := strings.Index(content, "gitdir:"); idx != -1 {
			gitDir := strings.TrimSpace(content[idx+len("gitdir:"):])
			norm := strings.ReplaceAll(gitDir, "\\", "/")
			if wtIdx := strings.Index(norm, "/.git/worktrees"); wtIdx != -1 {
				return filepath.Clean(norm[:wtIdx])
			}
			if gitIdx := strings.LastIndex(norm, "/.git"); gitIdx != -1 {
				return filepath.Clean(norm[:gitIdx])
			}
		}
	}
	return ""
}

// writeAtomic replaces a file via a temp file and rename, forcing a disk sync
// so that NTFS never commits a zero-length or null-filled directory entry on power loss or crash.
func writeAtomic(path string, payload []byte) error {
	temp := path + ".tmp"
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}

	var renameErr error
	for attempt := 0; attempt < 5; attempt++ {
		renameErr = os.Rename(temp, path)
		if renameErr == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = os.Remove(path)
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return renameErr
	}
	return nil
}

// safeName keeps generated ids from escaping the history tree or colliding with
// path syntax. Ids are app-generated, but they end up as filenames, and a thread
// id is not worth trusting with a path separator.
func safeName(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" || name == "." || name == ".." {
		return "_"
	}
	if len(name) > 96 {
		name = name[:96]
	}
	return name
}

func containsDriver(list []domain.DriverKind, d domain.DriverKind) bool {
	for _, item := range list {
		if strings.EqualFold(string(item), string(d)) {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

type transcriptScan struct {
	size    int64
	modTime time.Time
	offset  int64
	drivers []domain.DriverKind
	models  []string
}

var (
	transcriptScansMu sync.Mutex
	transcriptScans   = map[string]transcriptScan{}
)

var (
	driverKey    = []byte(`"driver":"`)
	noticeKind   = []byte(`"kind":"notice"`)
	switchedFrom = []byte("Switched from ")
)

func scanTranscriptDriversAndModels(path string, existingDrivers []domain.DriverKind, existingModels []string) ([]domain.DriverKind, []string) {
	info, err := os.Stat(path)
	if err != nil {
		return existingDrivers, existingModels
	}

	transcriptScansMu.Lock()
	cached, ok := transcriptScans[path]
	transcriptScansMu.Unlock()

	if !ok || info.Size() < cached.offset {
		cached = transcriptScan{}
	}
	if !ok || info.Size() != cached.size || !info.ModTime().Equal(cached.modTime) {
		scanned, err := scanTranscriptFrom(path, cached)
		if err != nil {
			return existingDrivers, existingModels
		}
		scanned.size = info.Size()
		scanned.modTime = info.ModTime()
		cached = scanned
		transcriptScansMu.Lock()
		transcriptScans[path] = cached
		transcriptScansMu.Unlock()
	}

	drivers := make([]domain.DriverKind, len(existingDrivers), len(existingDrivers)+len(cached.drivers))
	copy(drivers, existingDrivers)
	for _, d := range cached.drivers {
		if !containsDriver(drivers, d) {
			drivers = append(drivers, d)
		}
	}
	models := make([]string, len(existingModels), len(existingModels)+len(cached.models))
	copy(models, existingModels)
	for _, m := range cached.models {
		if !containsString(models, m) {
			models = append(models, m)
		}
	}
	return drivers, models
}

func scanTranscriptFrom(path string, prev transcriptScan) (transcriptScan, error) {
	file, err := os.Open(path)
	if err != nil {
		return prev, err
	}
	defer file.Close()

	next := transcriptScan{
		offset:  prev.offset,
		drivers: append([]domain.DriverKind(nil), prev.drivers...),
		models:  append([]string(nil), prev.models...),
	}
	if next.offset > 0 {
		if _, err := file.Seek(next.offset, io.SeekStart); err != nil {
			return prev, err
		}
	}

	addDriver := func(d domain.DriverKind) {
		if d != "" && !containsDriver(next.drivers, d) {
			next.drivers = append(next.drivers, d)
		}
	}

	reader := bufio.NewReaderSize(file, 256*1024)
	for {
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			rest := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				line, err = reader.ReadSlice('\n')
				rest = append(rest, line...)
			}
			line = rest
		}
		if err != nil {
			break
		}
		next.offset += int64(len(line))

		head := line
		if len(head) > 512 {
			head = head[:512]
		}
		if i := bytes.Index(head, driverKey); i >= 0 {
			value := line[i+len(driverKey):]
			if j := bytes.IndexByte(value, '"'); j > 0 {
				addDriver(domain.DriverKind(strings.ToLower(string(value[:j]))))
			}
		}
		if !bytes.Contains(line, noticeKind) || !bytes.Contains(line, switchedFrom) {
			continue
		}
		var partial struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(line, &partial) != nil || !strings.Contains(partial.Text, "Switched from ") {
			continue
		}
		text := strings.TrimPrefix(partial.Text, "Switched from ")
		parts := strings.Split(text, " to ")
		if len(parts) != 2 {
			continue
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			subParts := strings.SplitN(part, ": ", 2)
			if len(subParts) == 2 {
				addDriver(domain.DriverKind(strings.ToLower(strings.TrimSpace(subParts[0]))))
				if mName := strings.TrimSpace(subParts[1]); mName != "" && !containsString(next.models, mName) {
					next.models = append(next.models, mName)
				}
			} else {
				addDriver(domain.DriverKind(strings.ToLower(part)))
			}
		}
	}
	return next, nil
}
