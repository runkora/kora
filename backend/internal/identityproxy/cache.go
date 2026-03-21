// Package identityproxy implements the ext_proc identity sidecar.
// It caches a platform JWT, exchanges it via mTLS against the identity service,
// and injects it as an Authorization header on every outbound request from the
// agent workload — without any involvement from the agent itself (ADR-0021).
package identityproxy

import (
	"sync"
	"time"
)

// TokenCache holds the single platform JWT shared across all goroutines and all
// Envoy worker threads that send ext_proc requests to this binary.
//
// Reads are lock-free at the RLock level; writes (exchange / refresh) briefly
// acquire the full write lock. Under normal operation writes happen at most once
// per SVID rotation cycle (every ~7.5 minutes for a 15-minute SVID TTL).
type TokenCache struct {
	mu     sync.RWMutex
	token  string
	expiry time.Time
}

// NewTokenCache constructs an empty TokenCache ready for use.
func NewTokenCache() *TokenCache {
	return &TokenCache{}
}

// Set stores a newly obtained platform token and its expiry time.
// Called by the Exchanger after a successful /identity/exchange call.
func (c *TokenCache) Set(token string, expiry time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.expiry = expiry
}

// Get returns the cached token, its expiry time, and whether the cache
// currently holds a valid (non-empty, non-expired) token.
func (c *TokenCache) Get() (token string, expiry time.Time, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token == "" || time.Now().After(c.expiry) {
		return "", time.Time{}, false
	}
	return c.token, c.expiry, true
}

// Invalidate clears the cached token. Called immediately on SVID rotation to
// force a re-exchange before the next outbound request is served.
func (c *TokenCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.expiry = time.Time{}
}

// IsExpiringSoon reports whether the cached token will expire within threshold.
// Used by the background refresh goroutine to decide whether to re-exchange
// proactively (the ADR specifies expiry − 60 seconds as the threshold).
func (c *TokenCache) IsExpiringSoon(threshold time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token == "" {
		return true // treat empty cache as expiring
	}
	return time.Now().Add(threshold).After(c.expiry)
}
