package triggers

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// SourceAdapter is the per-source plug-in that makes the inbound pipeline generic.
// Each provider (GitHub, Alertmanager, and later others) implements it. The
// pipeline (verify → match → template → dispatch → log) is adapter-agnostic.
//
// One adapter per file: see adapter_github.go, adapter_alertmanager.go. To add
// a new adapter, drop a new adapter_<provider>.go file with the implementation
// and register it in NewRegistry below.
type SourceAdapter interface {
	// Provider returns the adapter's stable identifier (e.g. "github").
	Provider() string
	// Verify checks the delivery signature against the source's signing secret.
	// Returns nil if the signature is valid, ErrSignatureInvalid otherwise.
	Verify(secret string, headers http.Header, body []byte) error
	// DeliveryID returns the provider's unique delivery id for redelivery dedup,
	// or "" if the provider supplies none. Adapters that carry the id in a header
	// ignore body; adapters that derive the id from payload content (e.g. a
	// content hash) use it.
	DeliveryID(headers http.Header, body []byte) string
	// EventType returns the canonical event type for the delivery.
	EventType(headers http.Header, body []byte) string
	// Fields parses the payload into a flat, allow-listed map of dotted keys →
	// string values, used for both condition matching and prompt templating.
	Fields(eventType string, body []byte) map[string]string
}

// Registry maps provider id → adapter.
type Registry struct {
	adapters map[string]SourceAdapter
}

// NewRegistry builds the adapter registry with all v1 adapters registered.
func NewRegistry() *Registry {
	r := &Registry{adapters: make(map[string]SourceAdapter)}
	r.register(&githubAdapter{})
	r.register(&alertmanagerAdapter{})
	return r
}

func (r *Registry) register(a SourceAdapter) { r.adapters[a.Provider()] = a }

// Get returns the adapter for a provider, or (nil, false) if unregistered.
func (r *Registry) Get(provider string) (SourceAdapter, bool) {
	a, ok := r.adapters[provider]
	return a, ok
}

// Providers returns the registered provider ids (for validation/UI).
func (r *Registry) Providers() []string {
	out := make([]string, 0, len(r.adapters))
	for p := range r.adapters {
		out = append(out, p)
	}
	return out
}

// matchesCondition reports whether the delivery fields satisfy every clause of a
// trigger's match condition. Each clause is `field → allowed values`; a clause
// is satisfied when the field's value is one of the allowed values. An empty
// condition matches everything. Empty allowed-value lists are ignored.
func matchesCondition(condition map[string][]string, fields map[string]string) bool {
	for key, allowed := range condition {
		if len(allowed) == 0 {
			continue
		}
		if !slices.Contains(allowed, fields[key]) {
			return false
		}
	}
	return true
}

// validateProvider returns nil if the provider has a registered adapter.
func (r *Registry) validateProvider(provider string) error {
	if _, ok := r.Get(provider); !ok {
		return fmt.Errorf("%w: %q (known: %s)", ErrUnknownProvider, provider, strings.Join(r.Providers(), ", "))
	}
	return nil
}
