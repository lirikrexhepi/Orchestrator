package activity

import (
	"context"
	"sync"
	"time"

	"composer/internal/domain"
)

const dayLayout = "2006-01-02"

type Key struct {
	Day     string `json:"day"`
	Project string `json:"project"`
	Driver  string `json:"driver"`
	Model   string `json:"model"`
}

type Counts struct {
	Messages         int64 `json:"messages"`
	Sessions         int64 `json:"sessions"`
	ToolCalls        int64 `json:"toolCalls"`
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

func (c Counts) Tokens() int64 {
	return c.InputTokens + c.OutputTokens + c.CacheReadTokens + c.CacheWriteTokens
}

func (c *Counts) add(o Counts) {
	c.Messages += o.Messages
	c.Sessions += o.Sessions
	c.ToolCalls += o.ToolCalls
	c.InputTokens += o.InputTokens
	c.OutputTokens += o.OutputTokens
	c.CacheReadTokens += o.CacheReadTokens
	c.CacheWriteTokens += o.CacheWriteTokens
}

func (c Counts) empty() bool {
	return c == Counts{}
}

type Row struct {
	Key
	Counts
}

type Sink interface {
	AddActivity(ctx context.Context, rows []Row) error
}

type Thread struct {
	Project string
	Driver  string
	Model   string
}

type accumulator struct {
	pending map[Key]*Counts
	threads map[string]Thread
	turns   map[string]domain.Usage
	tools   map[string]map[string]struct{}
}

func newAccumulator() *accumulator {
	return &accumulator{
		pending: make(map[Key]*Counts),
		threads: make(map[string]Thread),
		turns:   make(map[string]domain.Usage),
		tools:   make(map[string]map[string]struct{}),
	}
}

func dayOf(at int64) string {
	if at <= 0 {
		return time.Now().Format(dayLayout)
	}
	return time.UnixMilli(at).Format(dayLayout)
}

func turnKey(event domain.RuntimeEvent) string {
	if event.TurnID != "" {
		return event.ThreadID + "\x00" + event.TurnID
	}
	return event.ThreadID
}

func positive(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func (a *accumulator) seed(threadID string, info Thread) {
	current := a.threads[threadID]
	if current.Project == "" {
		current.Project = info.Project
	}
	if current.Driver == "" {
		current.Driver = info.Driver
	}
	if current.Model == "" {
		current.Model = info.Model
	}
	a.threads[threadID] = current
}

func (a *accumulator) bucket(event domain.RuntimeEvent) *Counts {
	info := a.threads[event.ThreadID]
	if event.Driver != "" {
		info.Driver = string(event.Driver)
	}
	if event.Model != "" {
		info.Model = event.Model
	}
	a.threads[event.ThreadID] = info
	key := Key{Day: dayOf(event.At), Project: info.Project, Driver: info.Driver, Model: info.Model}
	counts, ok := a.pending[key]
	if !ok {
		counts = &Counts{}
		a.pending[key] = counts
	}
	return counts
}

func counted(kind domain.EventKind) bool {
	switch kind {
	case domain.EventSessionStarted, domain.EventSessionStopped, domain.EventUserMessage, domain.EventToolCall,
		domain.EventUsage, domain.EventAgentMessage, domain.EventTurnCompleted, domain.EventTurnFailed:
		return true
	}
	return false
}

func (a *accumulator) observe(event domain.RuntimeEvent) {
	switch event.Kind {
	case domain.EventSessionStarted:
		a.bucket(event).Sessions++
	case domain.EventUserMessage:
		a.bucket(event).Messages++
	case domain.EventToolCall:
		if event.Tool == nil {
			return
		}
		if event.Tool.ID != "" {
			seen, ok := a.tools[event.ThreadID]
			if !ok {
				seen = make(map[string]struct{})
				a.tools[event.ThreadID] = seen
			}
			if _, dup := seen[event.Tool.ID]; dup {
				return
			}
			seen[event.Tool.ID] = struct{}{}
		}
		a.bucket(event).ToolCalls++
	case domain.EventUsage, domain.EventAgentMessage:
		if event.Usage == nil {
			return
		}
		key := turnKey(event)
		previous := a.turns[key]
		current := *event.Usage
		a.turns[key] = current
		delta := Counts{
			InputTokens:      positive(current.InputTokens - previous.InputTokens),
			OutputTokens:     positive(current.OutputTokens - previous.OutputTokens),
			CacheReadTokens:  positive(current.CacheReadTokens - previous.CacheReadTokens),
			CacheWriteTokens: positive(current.CacheWriteTokens - previous.CacheWriteTokens),
		}
		if !delta.empty() {
			a.bucket(event).add(delta)
		}
	case domain.EventTurnCompleted, domain.EventTurnFailed:
		delete(a.turns, turnKey(event))
		delete(a.tools, event.ThreadID)
	case domain.EventSessionStopped:
		delete(a.tools, event.ThreadID)
	}
}

func (a *accumulator) drain() []Row {
	rows := make([]Row, 0, len(a.pending))
	for key, counts := range a.pending {
		if counts.empty() {
			continue
		}
		rows = append(rows, Row{Key: key, Counts: *counts})
	}
	a.pending = make(map[Key]*Counts)
	return rows
}

type Tracker struct {
	mu      sync.Mutex
	acc     *accumulator
	sink    Sink
	resolve func(threadID string) Thread
	since   int64
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	started bool
}

func NewTracker(sink Sink, resolve func(threadID string) Thread) *Tracker {
	return &Tracker{
		acc:     newAccumulator(),
		sink:    sink,
		resolve: resolve,
		since:   time.Now().UnixMilli(),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (t *Tracker) Since() int64 {
	return t.since
}

func (t *Tracker) Observe(event domain.RuntimeEvent) {
	if t == nil || event.ThreadID == "" {
		return
	}
	if event.At > 0 && event.At < t.since {
		return
	}
	if !counted(event.Kind) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	info, known := t.acc.threads[event.ThreadID]
	retry := info.Project == "" && (event.Kind == domain.EventSessionStarted || event.Kind == domain.EventUserMessage)
	if t.resolve != nil && (!known || retry) {
		t.acc.seed(event.ThreadID, t.resolve(event.ThreadID))
	}
	t.acc.observe(event)
}

func (t *Tracker) Flush(ctx context.Context) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	rows := t.acc.drain()
	t.mu.Unlock()
	if len(rows) == 0 {
		return nil
	}
	if err := t.sink.AddActivity(ctx, rows); err != nil {
		t.mu.Lock()
		for _, row := range rows {
			counts, ok := t.acc.pending[row.Key]
			if !ok {
				counts = &Counts{}
				t.acc.pending[row.Key] = counts
			}
			counts.add(row.Counts)
		}
		t.mu.Unlock()
		return err
	}
	return nil
}

func (t *Tracker) Run(interval time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return
	}
	t.started = true
	t.mu.Unlock()
	go func() {
		defer close(t.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = t.Flush(context.Background())
			case <-t.stop:
				return
			}
		}
	}()
}

func (t *Tracker) Close() error {
	if t == nil {
		return nil
	}
	t.once.Do(func() { close(t.stop) })
	t.mu.Lock()
	started := t.started
	t.mu.Unlock()
	if started {
		select {
		case <-t.done:
		case <-time.After(2 * time.Second):
		}
	}
	return t.Flush(context.Background())
}
