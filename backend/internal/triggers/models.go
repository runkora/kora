// Package triggers implements the Orbit Triggers feature: a generic, inbound
// event framework that starts agent runs automatically in response to external
// events. The webhook trigger is the first (v1) kind — see
// docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md and
// docs/prd/agent-webhook-triggers.md.
//
// The package is hosted by the identity binary (the only externally-reachable
// service and the platform token authority) but is a self-contained feature
// package: it never imports other feature packages. It reaches the agent control
// plane through a typed platform client and mints each webhook source's identity
// via the TokenMinter interface (satisfied by the identity service in main).
package triggers

import (
	"errors"
	"time"
)

// Provider identifiers for the source adapters that ship in v1.
const (
	ProviderGitHub = "github"
)

// Delivery processing statuses recorded in trigger_deliveries.
const (
	StatusVerified   = "verified"   // signature verified; matching pending
	StatusDispatched = "dispatched" // matched a trigger and started a run
	StatusSkipped    = "skipped"    // verified but matched no trigger
	StatusFailed     = "failed"     // matched but dispatch failed
)

// Sentinel errors. Callers branch on these with errors.Is.
var (
	// ErrSourceNotFound is returned when a webhook source does not exist (or is
	// not visible to the caller's tenant).
	ErrSourceNotFound = errors.New("webhook source not found")
	// ErrTriggerNotFound is returned when a trigger does not exist.
	ErrTriggerNotFound = errors.New("trigger not found")
	// ErrSourceDisabled is returned when a delivery targets a disabled source.
	ErrSourceDisabled = errors.New("webhook source is disabled")
	// ErrSignatureInvalid is returned when a delivery fails signature verification.
	ErrSignatureInvalid = errors.New("webhook signature verification failed")
	// ErrMissingDeliveryID is returned when a verified delivery carries no
	// provider delivery id. The id is the sole basis for replay dedup, and it is
	// not part of the signed payload, so a delivery without one cannot be
	// deduplicated and is rejected fail-closed rather than accepted unprotected.
	ErrMissingDeliveryID = errors.New("webhook delivery has no provider delivery id")
	// ErrUnknownProvider is returned when a source references an unregistered adapter.
	ErrUnknownProvider = errors.New("unknown webhook provider")
	// ErrValidation indicates a malformed create/update request.
	ErrValidation = errors.New("validation failed")
)

// WebhookSource is a registered inbound origin of webhook deliveries. It owns the
// HMAC signing secret (stored encrypted) and a mapped SERVICE principal under
// which the runs its triggers start execute.
type WebhookSource struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	Name        string         `json:"name"`
	Provider    string         `json:"provider"`
	PrincipalID string         `json:"-"`
	GrantID     *string        `json:"-"`
	Config      map[string]any `json:"config,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	// SecretCiphertext is the AES-GCM encrypted HMAC signing secret. It is never
	// serialized; only the ingest path reads it (to verify deliveries).
	SecretCiphertext string `json:"-"`
}

// Trigger binds (source, event type, match condition) → (agent, prompt template).
type Trigger struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenant_id"`
	SourceID       string              `json:"source_id"`
	Name           string              `json:"name"`
	AgentTypeID    string              `json:"agent_type_id"`
	AgentSlug      string              `json:"agent_slug"`
	EventType      string              `json:"event_type"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate string              `json:"prompt_template"`
	GrantID        *string             `json:"-"`
	Enabled        bool                `json:"enabled"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// Delivery is the durable record of an inbound webhook delivery and its outcome.
type Delivery struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	SourceID           string    `json:"source_id"`
	ProviderDeliveryID string    `json:"provider_delivery_id,omitempty"`
	EventType          string    `json:"event_type,omitempty"`
	Status             string    `json:"status"`
	TriggerID          *string   `json:"trigger_id,omitempty"`
	RunID              *string   `json:"run_id,omitempty"`
	Error              *string   `json:"error,omitempty"`
	PayloadExcerpt     string    `json:"payload_excerpt,omitempty"`
	ReceivedAt         time.Time `json:"received_at"`
}

// ─── API request/response DTOs ────────────────────────────────────────────────

// CreateSourceRequest is the body for POST /api/v1/triggers/sources.
type CreateSourceRequest struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	Config   map[string]any `json:"config,omitempty"`
}

// UpdateSourceRequest is the body for PATCH /api/v1/triggers/sources/{id}.
// Only the enabled flag is mutable in v1; rotating the secret regenerates it.
type UpdateSourceRequest struct {
	Enabled      *bool `json:"enabled,omitempty"`
	RotateSecret bool  `json:"rotate_secret,omitempty"`
}

// SourceResponse is the API view of a webhook source. The signing secret and the
// inbound webhook path are returned ONCE, at creation (or on rotation), in the
// Secret/WebhookPath fields — never on reads.
type SourceResponse struct {
	WebhookSource
	WebhookPath string `json:"webhook_path"`
	Secret      string `json:"secret,omitempty"`
}

// CreateTriggerRequest is the body for POST /api/v1/triggers. AgentSlug is
// supplied by the caller (the UI has it) so the service need not call the agent
// control plane to resolve it.
type CreateTriggerRequest struct {
	SourceID       string              `json:"source_id"`
	Name           string              `json:"name"`
	AgentTypeID    string              `json:"agent_type_id"`
	AgentSlug      string              `json:"agent_slug"`
	EventType      string              `json:"event_type"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate string              `json:"prompt_template"`
}

// UpdateTriggerRequest is the body for PATCH /api/v1/triggers/{id}. The agent
// binding is immutable (changing it would re-key the run grant) — recreate the
// trigger to retarget it.
type UpdateTriggerRequest struct {
	Name           *string             `json:"name,omitempty"`
	EventType      *string             `json:"event_type,omitempty"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate *string             `json:"prompt_template,omitempty"`
	Enabled        *bool               `json:"enabled,omitempty"`
}
