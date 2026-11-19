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
	subscribers map[int]chan domain.RuntimeEvent
	nextID      int
}

func NewBus() *Bus {
	return &Bus{subscribers: make(map[int]chan domain.RuntimeEvent)}
}

func (b *Bus) Publish(event domain.RuntimeEvent) domain.RuntimeEvent {
	domain.NormalizeEvent(&event)
	event.Seq = b.seq.Add(1)
	if event.At == 0 {
		event.At = time.Now().UnixMilli()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
			continue
		default:
		}
		if event.Delta {
			continue
		}
		timer := time.NewTimer(controlSendTimeout)
		select {
		case ch <- event:
		case <-timer.C:
		}
		timer.Stop()
	}
	return event
}

func (b *Bus) Subscribe() (<-chan domain.RuntimeEvent, func()) {
	ch := make(chan domain.RuntimeEvent, subscriberBuffer)

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}
