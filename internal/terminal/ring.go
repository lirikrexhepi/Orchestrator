package terminal

import "sync"

const defaultScrollbackBytes = 256 * 1024

// RingBuffer is a thread-safe fixed-capacity byte ring buffer.
type RingBuffer struct {
	mu       sync.RWMutex
	buf      []byte
	start    int
	size     int
	capacity int
}

// NewRingBuffer allocates a ring buffer with the given capacity, defaulting to defaultScrollbackBytes.
func NewRingBuffer(capacity ...int) *RingBuffer {
	capVal := defaultScrollbackBytes
	if len(capacity) > 0 && capacity[0] > 0 {
		capVal = capacity[0]
	}
	return &RingBuffer{
		buf:      make([]byte, capVal),
		capacity: capVal,
	}
}

// Write writes bytes into the ring buffer, overwriting the oldest bytes when full.
func (r *RingBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	n := len(p)
	if n >= r.capacity {
		copy(r.buf, p[n-r.capacity:])
		r.start = 0
		r.size = r.capacity
		return n, nil
	}

	if r.size+n > r.capacity {
		overflow := (r.size + n) - r.capacity
		r.start = (r.start + overflow) % r.capacity
		r.size = r.capacity - n
	}

	writePos := (r.start + r.size) % r.capacity
	if writePos+n <= r.capacity {
		copy(r.buf[writePos:], p)
	} else {
		first := r.capacity - writePos
		copy(r.buf[writePos:], p[:first])
		copy(r.buf[0:], p[first:])
	}
	r.size += n

	return n, nil
}

// Snapshot returns a contiguous copy of the current buffered bytes in chronological order.
func (r *RingBuffer) Snapshot() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.size == 0 {
		return nil
	}

	out := make([]byte, r.size)
	if r.start+r.size <= r.capacity {
		copy(out, r.buf[r.start:r.start+r.size])
	} else {
		first := r.capacity - r.start
		copy(out[:first], r.buf[r.start:])
		copy(out[first:], r.buf[:r.size-first])
	}
	return out
}

// Cap returns the maximum capacity in bytes.
func (r *RingBuffer) Cap() int {
	return r.capacity
}

// Len returns the current number of valid bytes in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}
