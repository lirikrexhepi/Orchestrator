package antigravity

import (
	"context"
	"net/http"
	"sync"
	"time"

	"composer/internal/domain"
)

const (
	quotaTTL     = 45 * time.Second
	quotaTimeout = 10 * time.Second
	quotaBudget  = 25 * time.Second
)

// QuotaSource serves the newest Antigravity quota figures it has and refreshes
// asynchronously in the background so UI callers never block on network I/O.
type QuotaSource struct {
	accountID string
	fetch     func(ctx context.Context, client *http.Client) ([]domain.RateLimit, int64, error)
	ttl       time.Duration
	client    *http.Client

	mu        sync.Mutex
	limits    []domain.RateLimit
	fetchedAt int64
	err       error
	checkedAt time.Time
	inflight  bool
	onUpdate  func()
}

// NewQuotaSource creates a QuotaSource for accountID (default profile when "" or "default").
func NewQuotaSource(accountID string) *QuotaSource {
	normID := domain.NormalizeAccount(accountID)
	return &QuotaSource{
		accountID: normID,
		fetch: func(ctx context.Context, client *http.Client) ([]domain.RateLimit, int64, error) {
			return FetchQuota(ctx, normID, client)
		},
		ttl:    quotaTTL,
		client: newQuotaClient(&http.Client{Timeout: quotaTimeout}),
	}
}

// OnUpdate registers a callback invoked outside the mutex whenever a refresh completes.
func (s *QuotaSource) OnUpdate(notify func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdate = notify
}

// Snapshot returns cached quota limits, fetch timestamp, and any error without blocking.
func (s *QuotaSource) Snapshot() ([]domain.RateLimit, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.inflight && time.Since(s.checkedAt) >= s.ttl {
		s.inflight = true
		go s.refresh()
	}

	return s.limits, s.fetchedAt, s.err
}

// Invalidate clears the freshness window so the next Snapshot triggers a background refresh.
func (s *QuotaSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt = time.Time{}
	clearTokenCache()
}

func (s *QuotaSource) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), quotaBudget)
	defer cancel()

	limits, fetchedAt, err := s.fetch(ctx, s.client)
	s.store(limits, fetchedAt, err)
}

func (s *QuotaSource) store(limits []domain.RateLimit, fetchedAt int64, err error) {
	s.mu.Lock()
	s.inflight = false
	s.checkedAt = time.Now()
	s.err = err
	if len(limits) > 0 {
		s.limits, s.fetchedAt = limits, fetchedAt
	}
	notify := s.onUpdate
	s.mu.Unlock()

	if notify != nil {
		notify()
	}
}
