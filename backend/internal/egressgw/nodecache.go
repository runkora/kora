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

// ownerCache maps a node id to its owner login per tenant, host and the
// connection whose token resolved it, with a TTL and a size bound.
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

func (c *ownerCache) get(tenantID, host, connection, id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[ownerKey(tenantID, host, connection, id)]
	if !ok || c.now().After(e.expires) {
		return "", false
	}
	return e.owner, true
}

// put stores an owner, or "" for an id the connection cannot see. At the size
// bound it drops expired entries, then any entries needed to make room.
func (c *ownerCache) put(tenantID, host, connection, id, owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= nodeOwnerEntries {
		now := c.now()
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
		for k := range c.entries {
			if len(c.entries) < nodeOwnerEntries {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[ownerKey(tenantID, host, connection, id)] = ownerEntry{owner: owner, expires: c.now().Add(nodeOwnerTTL)}
}

func ownerKey(tenantID, host, connection, id string) string {
	return tenantID + "|" + host + "|" + connection + "|" + id
}
