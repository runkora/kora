package inferencegw

import (
	"context"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// roleCacheTTL bounds how stale a resolved role set may be on the inference hot
// path. Role membership itself only changes at login (see
// docs/adr/2026-06-15-role-budget-attribution-for-non-interactive-credentials.md),
// so a short cache is purely a load shield on the role lookup — it never makes
// membership any more stale than the login-cadence bound already does.
const roleCacheTTL = 90 * time.Second

// roleCache is a small TTL cache mapping (tenant, principal) → resolved roles.
// It shields the per-request role lookup that backs role-budget attribution and
// enforcement for credentials that carry no roles claim (PATs). Concurrency-safe.
type roleCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]roleCacheEntry
}

type roleCacheEntry struct {
	roles   []string
	expires time.Time
}

func newRoleCache(ttl time.Duration) *roleCache {
	return &roleCache{ttl: ttl, entries: make(map[string]roleCacheEntry)}
}

func roleCacheKey(tenantID, principalID string) string {
	return tenantID + "|" + principalID
}

// get returns the cached roles for the key when present and unexpired. The
// second return value distinguishes a live hit (including a cached empty set,
// which is a meaningful "this principal holds no roles" result) from a miss.
func (c *roleCache) get(key string, now time.Time) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || now.After(e.expires) {
		return nil, false
	}
	return e.roles, true
}

func (c *roleCache) set(key string, roles []string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = roleCacheEntry{roles: roles, expires: now.Add(c.ttl)}
}

// resolveRolesCached returns the principal's current roles, resolved from the
// user_profiles snapshot (the single source of truth — credentials never carry
// roles) and cached for roleCacheTTL. Fails open: any lookup error resolves to
// no roles so that role-resolution problems never block inference. An empty
// result is cached too, so unbudgeted principals don't re-query every request.
func (s *Service) resolveRolesCached(ctx context.Context, tenantID, principalID string) []string {
	if s.usage == nil || principalID == "" {
		return nil
	}
	now := time.Now()
	key := roleCacheKey(tenantID, principalID)
	if s.roleCache != nil {
		if roles, ok := s.roleCache.get(key, now); ok {
			return roles
		}
	}
	roles, err := s.usage.GetPrincipalRoles(ctx, tenantID, principalID)
	if err != nil {
		s.logger.Warn(ctx, "budget.role_resolve_failed",
			logging.Field{Key: "reason", Value: "principal role lookup failed"},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "principal_id", Value: principalID},
		)
		return nil
	}
	if s.roleCache != nil {
		s.roleCache.set(key, roles, now)
	}
	return roles
}
