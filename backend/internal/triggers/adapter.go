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
// The fields a trigger can read come from the provider's schema.
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
	// Schema returns the provider's schema in the Orbit format: event types
	// mapped to the JSON Schema of their payloads. See BuildCatalog.
	Schema() []byte
}

// senderIdentifier is implemented by adapters whose provider reports who
// caused an event. The adapter owns its provider's rules: which associations
// exist, and how logins compare.
type senderIdentifier interface {
	// Sender reads the sender from the decoded body. Login is normalized;
	// ok is false when this event carries no sender.
	Sender(eventType string, body any) (s Sender, ok bool)
	// NormalizeLogin puts a login in the form Sender returns, for comparison.
	NormalizeLogin(login string) string
	// Associations lists the association values a restriction may name.
	Associations() []string
}

// Sender is who caused an event, as the provider reports it.
type Sender struct {
	Login       string
	Association string
}

// Registry maps provider id → adapter and its catalog.
type Registry struct {
	adapters map[string]SourceAdapter
	catalogs map[string]*Catalog
}

// NewRegistry registers every adapter and builds its catalog. A schema that
// does not build fails startup.
func NewRegistry() (*Registry, error) {
	r := &Registry{adapters: make(map[string]SourceAdapter), catalogs: make(map[string]*Catalog)}
	for _, a := range []SourceAdapter{&githubAdapter{}, &alertmanagerAdapter{}} {
		c, err := BuildCatalog(a.Schema())
		if err != nil {
			return nil, fmt.Errorf("build %s catalog: %w", a.Provider(), err)
		}
		r.adapters[a.Provider()] = a
		r.catalogs[a.Provider()] = c
	}
	return r, nil
}

// Get returns the adapter for a provider, or (nil, false) if unregistered.
func (r *Registry) Get(provider string) (SourceAdapter, bool) {
	a, ok := r.adapters[provider]
	return a, ok
}

// Catalog returns a provider's catalog, or nil for an unknown provider.
func (r *Registry) Catalog(provider string) *Catalog {
	return r.catalogs[provider]
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
