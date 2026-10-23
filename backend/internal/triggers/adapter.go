package triggers

import (
	_ "embed"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// SourceAdapter is the per-source plug-in that makes the inbound pipeline generic.
// Each provider (GitHub, Alertmanager, Slack, and later others) implements it. The
// pipeline (verify → match → template → dispatch → log) is adapter-agnostic.
// The fields a trigger can read come from the provider's schema.
//
// One adapter per file: see adapter_github.go, adapter_alertmanager.go,
// adapter_slack.go and adapter_discord.go. To add a new adapter, drop a new
// adapter_<provider>.go file with the implementation and register it in NewRegistry below.
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
	// Schema returns the provider's schema in the kora format: event types
	// mapped to the JSON Schema of their payloads. See BuildCatalog.
	Schema() []byte
}

// senderIdentifier is implemented by adapters whose provider reports who
// caused an event. The adapter owns its provider's rules: which roles exist,
// and how ids compare.
type senderIdentifier interface {
	// Sender reads the sender from the decoded body. ok is false when the event carries no sender.
	Sender(eventType string, body any) (s Sender, ok bool)
	// NormalizeID puts an id in the form Sender returns, for comparison.
	NormalizeID(id string) string
	// Roles lists the roles a restriction may name, with their labels.
	Roles() []SenderRole
}

// SenderRole is a role a provider reports, with the text the console shows.
type SenderRole struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// chatAdapter is implemented by chat platform adapters. A trigger on a chat
// source reads the document ChatEvent returns, in the shape of schemas/chat.json.
type chatAdapter interface {
	// ChatEvent maps a decoded payload to the chat document. ok is true only
	// when user.id, conversation.id and thread.id are all non-empty.
	ChatEvent(eventType string, doc any) (event map[string]any, ok bool)
}

// chatSchema is the catalog every chat adapter shares, so a chat trigger is
// written once for every platform.
//
//go:embed schemas/chat.json
var chatSchema []byte

// Source categories, read by the console in place of a provider list.
const (
	CategoryChat    = "chat"
	CategoryWebhook = "webhook"
)

// categoryOf reports whether an adapter's sources are chat or webhook sources.
func categoryOf(a SourceAdapter) string {
	if _, ok := a.(chatAdapter); ok {
		return CategoryChat
	}
	return CategoryWebhook
}

// issuedSecret is implemented by adapters whose provider issues the signing
// secret. Their sources take the secret from the admin at create and rotate.
type issuedSecret interface {
	providerIssuesSecret()
}

// handshaker is implemented by adapters whose provider sends a verification
// request that expects a reply and is not a delivery.
type handshaker interface {
	// Handshake returns the reply for a handshake request. ok is false for a delivery.
	Handshake(body []byte) (reply map[string]string, ok bool)
}

// Sender is who caused an event, as the provider reports it.
type Sender struct {
	ID    string
	Roles []string
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
	for _, a := range []SourceAdapter{&githubAdapter{}, &alertmanagerAdapter{}, &slackAdapter{}, &discordAdapter{}} {
		if err := r.register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// register adds an adapter with its catalog. A chat adapter gets the shared
// chat catalog, never its own schema.
func (r *Registry) register(a SourceAdapter) error {
	schema := a.Schema()
	if categoryOf(a) == CategoryChat {
		schema = chatSchema
	}
	c, err := BuildCatalog(schema)
	if err != nil {
		return fmt.Errorf("build %s catalog: %w", a.Provider(), err)
	}
	r.adapters[a.Provider()] = a
	r.catalogs[a.Provider()] = c
	return nil
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

// matchesContains reports whether every field in contains holds at least one of
// its substrings, ignoring case. A missing field is "" and contains nothing.
// Empty substring lists are ignored, as in matchesCondition.
func matchesContains(contains map[string][]string, fields map[string]string) bool {
	for key, subs := range contains {
		if len(subs) == 0 {
			continue
		}
		value := strings.ToLower(fields[key])
		if !slices.ContainsFunc(subs, func(sub string) bool { return strings.Contains(value, strings.ToLower(sub)) }) {
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
