package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

var (
	ErrSessionNotFound = errors.New("terminal session not found")
	ErrSessionClosed   = errors.New("terminal session closed")
)

// SessionInfo describes an active or recent terminal session.
type SessionInfo struct {
	ID        string `json:"id"`
	ThreadID  string `json:"threadId"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Shell     string `json:"shell"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	Running   bool   `json:"running"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Event represents an output chunk or process exit event.
type Event struct {
	Type     string `json:"type"` // "output" or "exit"
	Data     []byte `json:"data,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
}

type session struct {
	info   SessionInfo
	pty    PTY
	ring   *RingBuffer
	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.RWMutex
	closed      bool
	subscribers map[int]chan Event
	nextSubID   int
}

func (s *session) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	for _, ch := range s.subscribers {
		select {
		case ch <- ev:
		default:
			// Buffer full: drop oldest event to avoid blocking reader goroutine
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}

// Manager coordinates server-owned PTY sessions and their subscribers.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

// NewManager creates an empty terminal manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*session),
	}
}

func clamp(val, min, max, def int) int {
	if val <= 0 {
		return def
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func generateID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("term_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Open starts a new PTY session with the given parameters.
func (m *Manager) Open(threadID, cwd, title string, cols, rows int) (SessionInfo, error) {
	cols = clamp(cols, 20, 400, 100)
	rows = clamp(rows, 5, 200, 28)

	resolvedCwd := cwd
	if resolvedCwd == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			resolvedCwd = home
		}
	} else if fi, err := os.Stat(resolvedCwd); err != nil || !fi.IsDir() {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			resolvedCwd = home
		}
	}

	pty, shellLabel, err := startPTY(resolvedCwd, "", cols, rows, nil)
	if err != nil {
		return SessionInfo{}, err
	}

	if title == "" {
		title = shellLabel
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &session{
		info: SessionInfo{
			ID:        generateID(),
			ThreadID:  threadID,
			Title:     title,
			Cwd:       resolvedCwd,
			Shell:     shellLabel,
			Cols:      cols,
			Rows:      rows,
			Running:   true,
			CreatedAt: time.Now().UnixMilli(),
		},
		pty:         pty,
		ring:        NewRingBuffer(),
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[int]chan Event),
	}

	m.mu.Lock()
	m.sessions[sess.info.ID] = sess
	m.mu.Unlock()

	// Reader goroutine pumping bytes into the 256 KB ring buffer and broadcasting to WS subscribers
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.pty.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				_, _ = sess.ring.Write(chunk)
				sess.broadcast(Event{Type: "output", Data: chunk})
			}
			if err != nil {
				break
			}
		}
	}()

	// Waiter goroutine recording exit code and broadcasting exit frame
	go func() {
		code, _ := sess.pty.Wait(sess.ctx)

		sess.mu.Lock()
		sess.info.Running = false
		exitCode := code
		sess.info.ExitCode = &exitCode
		sess.mu.Unlock()

		sess.broadcast(Event{Type: "exit", ExitCode: exitCode})
	}()

	return sess.info, nil
}

// Get returns the metadata for a single session if it exists.
func (m *Manager) Get(id string) (SessionInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[id]
	if !ok {
		return SessionInfo{}, false
	}

	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return sess.info, true
}

// List returns all sessions belonging to the specified thread.
// When threadID is empty, it returns an empty slice so sessions never leak across threads.
func (m *Manager) List(threadID string) []SessionInfo {
	if threadID == "" {
		return []SessionInfo{}
	}
	return m.listFiltered(func(info SessionInfo) bool {
		return info.ThreadID == threadID
	})
}

// ListAll returns all active or recent terminal sessions across all threads.
func (m *Manager) ListAll() []SessionInfo {
	return m.listFiltered(nil)
}

func (m *Manager) listFiltered(match func(SessionInfo) bool) []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []SessionInfo
	for _, sess := range m.sessions {
		sess.mu.RLock()
		info := sess.info
		sess.mu.RUnlock()

		if match == nil || match(info) {
			result = append(result, info)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt < result[j].CreatedAt
	})
	if result == nil {
		return []SessionInfo{}
	}
	return result
}

// Write delivers input bytes to the PTY process.
func (m *Manager) Write(id string, data []byte) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	sess.mu.RLock()
	running := sess.info.Running && !sess.closed
	sess.mu.RUnlock()

	if !running {
		return ErrSessionClosed
	}

	_, err := sess.pty.Write(data)
	return err
}

// Resize updates the terminal dimensions on the PTY.
func (m *Manager) Resize(id string, cols, rows int) error {
	cols = clamp(cols, 20, 400, 100)
	rows = clamp(rows, 5, 200, 28)

	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	sess.mu.Lock()
	if sess.info.Cols == cols && sess.info.Rows == rows {
		sess.mu.Unlock()
		return nil
	}
	sess.info.Cols = cols
	sess.info.Rows = rows
	running := sess.info.Running && !sess.closed
	sess.mu.Unlock()

	if !running {
		return nil
	}

	return sess.pty.Resize(cols, rows)
}

// Close kills the PTY, unregisters the session, and cleanly closes subscriber channels.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	sess.cancel()
	_ = sess.pty.Close()

	sess.mu.Lock()
	sess.closed = true
	sess.info.Running = false
	for subID, ch := range sess.subscribers {
		delete(sess.subscribers, subID)
		close(ch)
	}
	sess.mu.Unlock()

	return nil
}

// CloseThread closes all sessions belonging to the specified thread.
func (m *Manager) CloseThread(threadID string) {
	if threadID == "" {
		return
	}

	m.mu.RLock()
	var targets []string
	for id, sess := range m.sessions {
		if sess.info.ThreadID == threadID {
			targets = append(targets, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range targets {
		_ = m.Close(id)
	}
}

// CloseAll closes every active terminal session cleanly.
func (m *Manager) CloseAll() {
	m.mu.RLock()
	var targets []string
	for id := range m.sessions {
		targets = append(targets, id)
	}
	m.mu.RUnlock()

	for _, id := range targets {
		_ = m.Close(id)
	}
}

// Subscribe attaches a buffered channel to receive events for the session and snapshots past output.
func (m *Manager) Subscribe(id string) ([]byte, <-chan Event, func(), SessionInfo, error) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return nil, nil, nil, SessionInfo{}, ErrSessionNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.closed {
		return nil, nil, nil, SessionInfo{}, ErrSessionClosed
	}

	snapshot := sess.ring.Snapshot()
	info := sess.info

	subCh := make(chan Event, 64)
	subID := sess.nextSubID
	sess.nextSubID++
	sess.subscribers[subID] = subCh

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			if _, exists := sess.subscribers[subID]; exists {
				delete(sess.subscribers, subID)
				close(subCh)
			}
		})
	}

	return snapshot, subCh, cancel, info, nil
}
