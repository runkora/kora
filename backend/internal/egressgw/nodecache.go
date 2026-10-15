package egressgw

import (
	"sync"
	"time"
)

// Node id owner cache bounds, from
// docs/adr/2026-10-15-same-host-connection-selection.md.
const (
	nodeOwnerTTL     = 10 * time.Minute
	nodeOwnerEntries = 10_000
)

// ownerCache maps a node id to its owner login per tenant and host, with a TTL
// and a size bound.
type ownerCache struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]ownerEntry
}

type ownerEntry struct {
	owner   string
	expires time.Time
}

func newOwnerCache(now func() time.Time) *ownerCache {
	return &ownerCache{now: now, entries: map[string]ownerEntry{}}
}

func (c *ownerCache) get(tenantID, host, id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[tenantID+"|"+host+"|"+id]
	if !ok || c.now().After(e.expires) {
		return "", false
	}
	return e.owner, true
}

// put stores an owner. At the size bound it drops expired entries, then any
// entries needed to make room.
func (c *ownerCache) put(tenantID, host, id, owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= nodeOwnerEntries {
		now := c.now()
		for k, e := range c.entries {
			if now.After(e.expires) || len(c.entries) >= nodeOwnerEntries {
				delete(c.entries, k)
			}
		}
	}
	c.entries[tenantID+"|"+host+"|"+id] = ownerEntry{owner: owner, expires: c.now().Add(nodeOwnerTTL)}
}
