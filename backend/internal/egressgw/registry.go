package egressgw

import (
	"sync"

	"github.com/runkora/kora/backend/internal/credentials"
)

// AdapterRegistry maps an AdapterType to its adapter implementation. The owning
// binary's main populates the registry at startup, one registration per
// supported credential mechanism, and passes it to the broker. The registry is
// concurrency-safe so adapters can be added from several init paths.
//
// A connection naming an unregistered AdapterType yields
// credentials.ErrUnsupportedAdapter at resolve time; the broker never panics
// on a missing registration.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[credentials.AdapterType]credentials.Adapter
}

// NewAdapterRegistry returns an empty AdapterRegistry. Use Register to populate.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: make(map[credentials.AdapterType]credentials.Adapter)}
}

// Register adds an adapter to the registry under its declared Type. A second
// registration for the same type silently replaces the first, supporting test
// fixtures that swap an adapter out.
//
// Passthrough is refused. Reaching a host that needs no credential is granted
// by the egress whitelist, so a Connection must never be able to name it; the
// broker holds that adapter directly instead. Enforcing it here rather than by
// convention in main means a mistaken wiring cannot reopen the path.
func (r *AdapterRegistry) Register(a credentials.Adapter) {
	if a.Type() == credentials.AdapterPassthrough {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Type()] = a
}

// Lookup returns the adapter for t. The second return value is false if no
// adapter is registered.
func (r *AdapterRegistry) Lookup(t credentials.AdapterType) (credentials.Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[t]
	return a, ok
}
