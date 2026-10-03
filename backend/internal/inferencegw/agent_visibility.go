package inferencegw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
)

// Agent usage is private to whoever may see the agent, so every agent rollup
// first asks orbit-agents which agents the caller can see and filters the usage
// rows to them. The slugs never come from the browser.

// visibilityTTL bounds how long an answer is reused. A grant revoked or an
// agent created inside this window is seen late by at most this long. The page
// makes a handful of calls in one burst on load and on each period change, and
// five seconds covers a burst without letting a revocation outlive a coffee.
const visibilityTTL = 5 * time.Second

// maxVisibilityEntries caps the cache so a stream of distinct tokens cannot grow it.
const maxVisibilityEntries = 1024

// agentVisibilitySource answers which agents a bearer may see.
type agentVisibilitySource interface {
	VisibleAgents(ctx context.Context, bearer string) (agentsclient.Visibility, error)
}

// visibilityCache reuses an answer per tenant and token for visibilityTTL and
// shares one in-flight call between concurrent askers. The key is the token
// itself, hashed, rather than the person: a narrowly scoped PAT sees fewer
// agents than the same person's session, and a hash of the token cannot mix
// the two up.
type visibilityCache struct {
	src agentVisibilitySource
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]visibilityEntry
	flight  singleflight.Group
}

type visibilityEntry struct {
	value   agentsclient.Visibility
	expires time.Time
}

func newVisibilityCache(src agentVisibilitySource, ttl time.Duration) *visibilityCache {
	return &visibilityCache{src: src, ttl: ttl, now: time.Now, entries: map[string]visibilityEntry{}}
}

func visibilityKey(tenantID, bearer string) string {
	sum := sha256.Sum256([]byte(bearer))
	return tenantID + ":" + hex.EncodeToString(sum[:])
}

// Visible returns the agents the bearer's principal may see.
func (c *visibilityCache) Visible(ctx context.Context, tenantID, bearer string) (agentsclient.Visibility, error) {
	key := visibilityKey(tenantID, bearer)
	if v, ok := c.lookup(key); ok {
		return v, nil
	}
	res, err, _ := c.flight.Do(key, func() (any, error) {
		// A caller whose context ends must not cancel the call the others share.
		v, err := c.src.VisibleAgents(context.WithoutCancel(ctx), bearer)
		if err != nil {
			return nil, fmt.Errorf("resolve visible agents: %w", err)
		}
		c.store(key, v)
		return v, nil
	})
	if err != nil {
		return agentsclient.Visibility{}, err
	}
	v, _ := res.(agentsclient.Visibility)
	return v, nil
}

func (c *visibilityCache) lookup(key string) (agentsclient.Visibility, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		return agentsclient.Visibility{}, false
	}
	return e.value, true
}

func (c *visibilityCache) store(key string, v agentsclient.Visibility) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxVisibilityEntries {
		now := c.now()
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= maxVisibilityEntries {
			clear(c.entries)
		}
	}
	c.entries[key] = visibilityEntry{value: v, expires: c.now().Add(c.ttl)}
}
