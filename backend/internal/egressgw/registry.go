package egressgw

import (
	"sync"

	"github.com/clear-route/orbit/internal/credentials"
)

// AdapterRegistry maps an AdapterType to its adapter implementation. The owning
// binary's main constructs and populates the registry at startup — one
// registration per supported credential mechanism — and passes it to the
// broker. The registry is concurrency-safe so adapters can be added during
// initialization from multiple init paths.
//
// A connection naming an unregistered AdapterType yields
// credentials.ErrUnsupportedAdapter at resolve time; the broker never panics
// on a missing registration.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[credentials.AdapterType]credentials.CredentialAdapter
}

// NewAdapterRegistry returns an empty AdapterRegistry. Use Register to populate.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: make(map[credentials.AdapterType]credentials.CredentialAdapter)}
}

// Register adds an adapter to the registry under its declared Type. A second
// registration for the same type silently replaces the first, supporting test
// fixtures that swap an adapter out.
func (r *AdapterRegistry) Register(a credentials.CredentialAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Type()] = a
}

// Lookup returns the adapter for t. The second return value is false if no
// adapter is registered.
func (r *AdapterRegistry) Lookup(t credentials.AdapterType) (credentials.CredentialAdapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[t]
	return a, ok
}
