package session

import (
	"sync"
	"sync/atomic"
	"time"

	"composer/internal/domain"
)

const subscriberBuffer = 4096

// controlSendTimeout bounds how long a slow subscriber may hold up a
// non-delta event. Deltas are droppable; turn/tool/approval events are not.
const controlSendTimeout = 2 * time.Second

// Bus fans runtime events out to subscribers. A slow subscriber may lose
// streamed deltas but never waits-out a control event (turn, tool, approval)
// unless it stalls for longer than controlSendTimeout.
type Bus struct {
	seq atomic.Uint64

	mu          sync.RWMutex
	subscribers map[int]*subscriber
	nextID      int
}

type subscriber struct {
	mu       sync.RWMutex
	ch       chan domain.RuntimeEvent
	done     chan struct{}
	doneOnce sync.Once
	closed   bool
}

func (s *subscriber) send(event domain.RuntimeEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- event:
		return
	default:
	}
	if event.Delta {
		return
	}
	timer := time.NewTimer(controlSendTimeout)
	select {
	case s.ch <- event:
	case <-s.done:
	case <-timer.C:
	}
	timer.Stop()
}

func (s *subscriber) close() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	s.mu.Unlock()
}

func NewBus() *Bus {
	return &Bus{subscribers: make(map[int]*subscriber)}
}

// Prepare normalizes an event and assigns a monotonic Seq and timestamp if unset.
func (b *Bus) Prepare(event domain.RuntimeEvent) domain.RuntimeEvent {
	domain.NormalizeEvent(&event)
	if event.Seq == 0 {
		event.Seq = b.seq.Add(1)
	}
	if event.At == 0 {
		event.At = time.Now().UnixMilli()
	}
	return event
}

func (b *Bus) Publish(event domain.RuntimeEvent) domain.RuntimeEvent {
	event = b.Prepare(event)

	b.mu.RLock()
	subs := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		sub.send(event)
	}
	return event
}

func (b *Bus) Subscribe() (<-chan domain.RuntimeEvent, func()) {
	sub := &subscriber{
		ch:   make(chan domain.RuntimeEvent, subscriberBuffer),
		done: make(chan struct{}),
	}

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = sub
	b.mu.Unlock()

	return sub.ch, func() {
		b.mu.Lock()
		existing, ok := b.subscribers[id]
		if ok {
			delete(b.subscribers, id)
		}
		b.mu.Unlock()
		if ok {
			existing.close()
		}
	}
}
