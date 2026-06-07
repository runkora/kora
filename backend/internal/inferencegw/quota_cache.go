package inferencegw

import (
	"context"
	"sync"
	"time"
)

// QuotaResolver is the interface used by the Service on the hot path.
// QuotaCache implements it with a 60-second in-process TTL.
type QuotaResolver interface {
	Resolve(ctx context.Context, tenantID, principalID string) (budgetUSD float64, hasLimit bool, err error)
}

type quotaCacheEntry struct {
	budgetUSD float64
	hasLimit  bool
	expiresAt time.Time
}

// QuotaCache wraps QuotaRepository.Resolve with a per-principal in-process TTL
// cache. This avoids a DB round-trip on every inference request while keeping
// quota changes visible within one TTL window.
type QuotaCache struct {
	repo  QuotaRepository
	mu    sync.Mutex
	cache map[string]quotaCacheEntry
	ttl   time.Duration
}

// NewQuotaCache constructs a QuotaCache with the given repository and TTL.
func NewQuotaCache(repo QuotaRepository, ttl time.Duration) *QuotaCache {
	return &QuotaCache{
		repo:  repo,
		cache: make(map[string]quotaCacheEntry),
		ttl:   ttl,
	}
}

// Resolve returns the cached quota limit for the principal, refreshing from the
// repository when the entry is absent or expired.
func (c *QuotaCache) Resolve(ctx context.Context, tenantID, principalID string) (float64, bool, error) {
	key := tenantID + ":" + principalID

	c.mu.Lock()
	if e, ok := c.cache[key]; ok && time.Now().Before(e.expiresAt) {
		c.mu.Unlock()
		return e.budgetUSD, e.hasLimit, nil
	}
	c.mu.Unlock()

	budget, hasLimit, err := c.repo.Resolve(ctx, tenantID, principalID)
	if err != nil {
		return 0, false, err
	}

	c.mu.Lock()
	c.cache[key] = quotaCacheEntry{
		budgetUSD: budget,
		hasLimit:  hasLimit,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return budget, hasLimit, nil
}
