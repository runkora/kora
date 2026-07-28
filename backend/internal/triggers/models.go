// Package triggers implements the Orbit Triggers feature: a generic, inbound
// event framework that starts agent runs automatically in response to external
// events. The webhook trigger is the first (v1) kind; scheduled and manual kinds
// are added here. See docs/adr/2026-06-20-extended-trigger-types.md and the
// original docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
//
// The package is hosted by the identity binary (the only externally-reachable
// service and the platform token authority) but is a self-contained feature
// package: it never imports other feature packages. It reaches the agent control
// plane through a typed platform client and mints each trigger source's identity
// via the TokenMinter interface (satisfied by the identity service in main).
package triggers

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Trigger kind identifiers.
const (
	KindWebhook   = "webhook"   // inbound HTTP push from an external system (v1)
	KindScheduled = "scheduled" // cron-based, time-driven firing
	KindManual    = "manual"    // explicit API call (agent-to-agent, CI scripts, etc.)
)

// Provider identifiers for the source adapters that ship in v1.
const (
	ProviderGitHub       = "github"
	ProviderAlertmanager = "alertmanager"
)

// Delivery statuses recorded in trigger_deliveries — what happened to the
// arrival itself, before any individual trigger was involved.
//
// This is a summary DERIVED from the per-trigger outcomes below once the fan-out
// completes; it is never authored independently. A delivery that started two
// runs and was denied by a third trigger is StatusDispatched at this level and
// carries all three outcomes at the trigger level.
//
// Every row here is an arrival that authenticated successfully. A webhook that
// fails signature verification is an unauthenticated request, so it updates a
// counter on its source rather than creating a row (ADR Decision 4).
const (
	StatusVerified   = "verified"   // established as genuine; matching pending
	StatusDispatched = "dispatched" // at least one trigger started a run
	StatusSkipped    = "skipped"    // genuine, but no trigger was listening
	StatusFailed     = "failed"     // matched, but no trigger started a run
	StatusDuplicate  = "duplicate"  // already handled; references the original
	StatusMissed     = "missed"     // a scheduled fire that was due but not run
)

// Per-trigger outcomes recorded in trigger_delivery_runs — what happened for
// each trigger a delivery matched. This is the level admission control,
// auto-pause and the console's per-trigger badges act on.
const (
	RunStatusDispatched = "dispatched" // this trigger started a run
	RunStatusFailed     = "failed"     // tried and failed, including dispatch timeout
	RunStatusFiltered   = "filtered"   // listening, but match_condition excluded this event
	RunStatusDenied     = "denied"     // policy refused this trigger's run
	RunStatusSuperseded = "superseded" // replaced by a newer fire under 'replace' overflow
	RunStatusDropped    = "dropped"    // refused at the ceiling, or evicted from the backlog
	RunStatusPaused     = "paused"     // the trigger was auto-paused when this fire arrived
)

// TerminalRunStatuses are the per-trigger outcomes that mean no run will ever
// start for this trigger and delivery. The console must show them as settled
// rather than pending.
var TerminalRunStatuses = map[string]bool{
	RunStatusFailed:     true,
	RunStatusFiltered:   true,
	RunStatusDenied:     true,
	RunStatusSuperseded: true,
	RunStatusDropped:    true,
	RunStatusPaused:     true,
}

// Target kinds: what a trigger starts. Triggers are the platform's front door
// for starting work; what gets started is a separate concern. 'agent' is the
// only kind today, and 'workflow' becomes legal when that pillar lands
// (ADR Decision 8).
const (
	TargetKindAgent = "agent"
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
	// ErrTriggerDisabled is returned when trying to fire a disabled trigger.
	// Reserved for the manual case: a person turned this trigger off, and the
	// fix is a person turning it back on.
	ErrTriggerDisabled = errors.New("trigger is disabled")
	// ErrTriggerAutoPaused is returned when a fire reaches a trigger the platform
	// paused itself. Distinct from ErrTriggerDisabled because the causes and the
	// fixes differ: the failures that paused it have to be dealt with before
	// re-enabling helps. The fire is still recorded against the trigger.
	ErrTriggerAutoPaused = errors.New("trigger was auto-paused after repeated failures")
	// ErrDeliveryNotFound is returned when a delivery does not exist, or is not
	// visible to the caller's tenant.
	ErrDeliveryNotFound = errors.New("delivery not found")
	// ErrNotRedeliverable is returned when a delivery exists but cannot be
	// replayed. The message names which of the reasons applies.
	ErrNotRedeliverable = errors.New("delivery cannot be redelivered")
	// ErrWrongKind is returned when an operation is not valid for the trigger's kind.
	ErrWrongKind = errors.New("operation not valid for trigger kind")

	// ErrFireForbidden is returned when a caller lacks the per-trigger
	// trigger:<id>:fire scope. Firing is authorized per trigger because the run
	// executes under the trigger's principal, not the caller's, so a broad
	// trigger cannot be borrowed by a narrowly-scoped caller.
	ErrFireForbidden = errors.New("not authorized to fire this trigger")

	// ErrTriggerAtCapacity is returned when a manual fire is refused because
	// the trigger is already at its concurrency ceiling.
	ErrTriggerAtCapacity = errors.New("trigger is at its concurrency limit; try again shortly")
)

// WebhookSource is a registered inbound origin of webhook deliveries. It owns the
// HMAC signing secret (stored encrypted) and a mapped SERVICE principal under
// which the runs its triggers start execute.
// RejectionCount / LastRejectionAt / LastRejectionReason record inbound requests
// that failed authentication. These never become deliveries: a bad signature is
// an unauthenticated request, so recording one as a row would let anyone on the
// internet grow the table. Updating a counter on the source the tenant already
// owns bounds the write no matter what an attacker sends, while still telling an
// admin their signing secret is wrong (ADR Decision 4).
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

	// Inbound requests that failed authentication. Surfaced so an admin with a
	// wrong signing secret can see it, without an unauthenticated caller ever
	// creating a row.
	RejectionCount      int64      `json:"rejection_count"`
	LastRejectionAt     *time.Time `json:"last_rejection_at,omitempty"`
	LastRejectionReason string     `json:"last_rejection_reason,omitempty"`
}

// ScheduleConfig holds the cron schedule for a KindScheduled trigger.
type ScheduleConfig struct {
	CronExpression string `json:"cron_expression"` // standard 5-field cron
	Timezone       string `json:"timezone"`        // IANA timezone name, e.g. "Australia/Sydney"
	// CatchUp controls what happens to a fire that was due while the platform
	// was down. Off by default: an agent doing real work twice is worse than not
	// doing it, and a long outage on a frequent schedule would otherwise dump a
	// burst of runs at once. Missed fires are still recorded (PD1).
	CatchUp bool `json:"catch_up"`
}

// Input field types a manual trigger may declare. Deliberately a short list:
// this is enough to render a plain form and validate an API call, and anything
// richer starts becoming a form builder, which is a different product (PD2).
const (
	InputTypeText    = "text"
	InputTypeNumber  = "number"
	InputTypeBoolean = "boolean"
	InputTypeChoice  = "choice"
)

// InputField is one field a manual trigger accepts.
//
// The declaration is the single source of truth: the console renders a form
// from it and the API validates against the same definition, so the two cannot
// drift.
type InputField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	// Choices are the permitted values when Type is "choice".
	Choices []string `json:"choices,omitempty"`
}

// ManualConfig is the kind_config for a manual trigger.
type ManualConfig struct {
	Inputs []InputField `json:"inputs,omitempty"`
}

// Validate checks a field declaration is usable before it is stored. Catching a
// bad declaration at creation is far better than at fire time, when the caller
// has no way to fix it.
func (f InputField) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("%w: input field needs a name", ErrValidation)
	}
	switch f.Type {
	case InputTypeText, InputTypeNumber, InputTypeBoolean:
	case InputTypeChoice:
		if len(f.Choices) == 0 {
			return fmt.Errorf("%w: input %q is a choice but declares no choices", ErrValidation, f.Name)
		}
	default:
		return fmt.Errorf("%w: input %q has unsupported type %q", ErrValidation, f.Name, f.Type)
	}
	return nil
}

// ValidateInputs checks a caller's values against the declaration, returning a
// per-field error map so the console can mark the offending inputs rather than
// showing one opaque message.
func (m *ManualConfig) ValidateInputs(values map[string]string) map[string]string {
	if m == nil || len(m.Inputs) == 0 {
		return nil
	}
	problems := map[string]string{}
	for _, f := range m.Inputs {
		if problem := f.check(values); problem != "" {
			problems[f.Name] = problem
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return problems
}

// check returns the problem with this field's supplied value, or "" if it is
// acceptable. An omitted optional field, and an omitted required field that has
// a default, are both fine.
func (f InputField) check(values map[string]string) string {
	v, present := values[f.Name]
	if !present || v == "" {
		if f.Required && f.Default == "" {
			return "required"
		}
		return ""
	}
	switch f.Type {
	case InputTypeNumber:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "must be a number"
		}
	case InputTypeBoolean:
		if v != "true" && v != "false" {
			return "must be true or false"
		}
	case InputTypeChoice:
		if !slices.Contains(f.Choices, v) {
			return "must be one of: " + strings.Join(f.Choices, ", ")
		}
	}
	return ""
}

// ApplyDefaults fills in declared defaults for values the caller omitted, so a
// trigger with sensible defaults can be fired with an empty body.
func (m *ManualConfig) ApplyDefaults(values map[string]string) map[string]string {
	if m == nil {
		return values
	}
	if values == nil {
		values = map[string]string{}
	}
	for _, f := range m.Inputs {
		if _, ok := values[f.Name]; !ok && f.Default != "" {
			values[f.Name] = f.Default
		}
	}
	return values
}

// Trigger binds a firing condition to (agent, prompt template).
// For KindWebhook: source_id, event_type, and match_condition drive firing.
// For KindScheduled: kind_config carries the cron schedule; source_id is empty.
// For KindManual: firing is driven by POST /triggers/{id}/fire; source_id is empty.
// Scheduled and manual triggers have their own principal_id (not inherited from a source).
type Trigger struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Kind     string `json:"kind"`
	// TargetKind is what this trigger starts. "agent" is the only kind today;
	// "workflow" becomes legal when that pillar lands. Triggers are the front
	// door for starting work, so nothing here assumes the target is an agent.
	TargetKind     string              `json:"target_kind"`
	SourceID       string              `json:"source_id,omitempty"`
	Name           string              `json:"name"`
	AgentTypeID    string              `json:"agent_type_id"`
	AgentSlug      string              `json:"agent_slug"`
	EventType      string              `json:"event_type,omitempty"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate string              `json:"prompt_template"`
	ScheduleConfig *ScheduleConfig     `json:"schedule_config,omitempty"`
	// ManualConfig declares the inputs a manual trigger accepts. One
	// declaration drives both the console form and API validation.
	ManualConfig *ManualConfig `json:"manual_config,omitempty"`
	// PrincipalID is the SERVICE principal for this trigger (scheduled/manual only;
	// webhook triggers inherit from their WebhookSource).
	PrincipalID string    `json:"-"`
	GrantID     *string   `json:"-"` // webhook: per-agent run grant; scheduled/manual: source-level run grant
	GrantIDSvc  *string   `json:"-"` // scheduled/manual: the trigger's own SERVICE principal grant
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// ─── Admission control ──────────────────────────────────────────────────
	// MaxParallel caps how many runs from this trigger may be in flight.
	// Overflow says what happens beyond that: wait, replace, or refuse.
	MaxParallel   int16  `json:"max_parallel"`
	Overflow      string `json:"overflow"`
	MaxQueueDepth int16  `json:"max_queue_depth"`

	// ─── Delivery safety ────────────────────────────────────────────────────
	// TimeoutSeconds bounds starting the run, not running it: a long-running
	// agent is not a failure.
	TimeoutSeconds   int16 `json:"timeout_seconds"`
	DedupeEnabled    bool  `json:"dedupe_enabled"`
	AutopauseEnabled bool  `json:"autopause_enabled"`
	// ConsecutiveFailures counts failures since the last success. A policy
	// denial counts; an admission rejection does not.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// AutoPausedAt is set when the platform disabled this trigger itself, which
	// the console shows differently from a trigger a person turned off.
	AutoPausedAt *time.Time `json:"auto_paused_at,omitempty"`
}

// Delivery is the durable record of a trigger delivery and its outcome.
// TriggerID/RunID summarize the first dispatched run; Runs carries the complete
// per-trigger fan-out (one entry per matched trigger) and is populated on reads.
type Delivery struct {
	ID                 string  `json:"id"`
	TenantID           string  `json:"tenant_id"`
	SourceID           string  `json:"source_id,omitempty"`
	TriggerID          *string `json:"trigger_id,omitempty"`
	Kind               string  `json:"kind"`
	ProviderDeliveryID string  `json:"provider_delivery_id,omitempty"`
	EventType          string  `json:"event_type,omitempty"`
	Status             string  `json:"status"`
	RunID              *string `json:"run_id,omitempty"`
	Error              *string `json:"error,omitempty"`
	PayloadExcerpt     string  `json:"payload_excerpt,omitempty"`
	// Payload is the full captured body. Superseded PayloadExcerpt; retention
	// nulls it after the tenant's window while every other column survives, so
	// a purged delivery still answers "did this fire, and what happened".
	Payload       []byte `json:"payload,omitempty"`
	PayloadBytes  int    `json:"payload_bytes,omitempty"`
	PayloadPurged bool   `json:"payload_purged,omitempty"`
	// Reason explains a non-dispatch outcome in the arrival's own terms
	// (duplicate, missed). Per-trigger reasons live on DeliveryRun.Error.
	Reason *string `json:"reason,omitempty"`
	// RedeliveryOf points at the delivery this one re-runs. A redelivery is a
	// new delivery referencing the original, never an edit of it.
	RedeliveryOf *string `json:"redelivery_of,omitempty"`
	// FiredBy is the principal ID of the caller who fired a manual trigger.
	FiredBy string `json:"fired_by,omitempty"`
	// ScheduledAt is the cron tick time for scheduled trigger deliveries.
	ScheduledAt *time.Time    `json:"scheduled_at,omitempty"`
	ReceivedAt  time.Time     `json:"received_at"`
	Runs        []DeliveryRun `json:"runs,omitempty"`
}

// DeliveryRun records a single matched-trigger dispatch for a delivery — the
// authoritative one-to-many audit detail behind a delivery. RunID is nil when
// the dispatch failed (Status == RunStatusFailed), in which case Error explains.
type DeliveryRun struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	DeliveryID string    `json:"delivery_id"`
	TriggerID  string    `json:"trigger_id"`
	RunID      *string   `json:"run_id,omitempty"`
	Status     string    `json:"status"`
	Error      *string   `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ─── API request/response DTOs ────────────────────────────────────────────────

// CreateSourceRequest is the body for POST /api/v1/trigger-sources.
type CreateSourceRequest struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	Config   map[string]any `json:"config,omitempty"`
}

// UpdateSourceRequest is the body for PATCH /api/v1/trigger-sources/{id}.
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

// CreateTriggerRequest is the body for POST /api/v1/triggers.
//
// Kind selects the trigger kind ("webhook", "scheduled", or "manual").
// Defaults to "webhook" when omitted for backward compatibility.
//
// For webhook kind: SourceID, EventType are required; MatchCondition is optional.
// For scheduled kind: ScheduleConfig is required; SourceID/EventType are ignored.
// For manual kind: no extra fields required beyond Name/AgentTypeID/PromptTemplate.
//
// AgentSlug is accepted for backward compatibility but IGNORED: CreateTrigger
// resolves the canonical slug from AgentTypeID via the agent control plane and
// never trusts the client-supplied value.
type CreateTriggerRequest struct {
	Kind           string              `json:"kind"`
	SourceID       string              `json:"source_id"`
	Name           string              `json:"name"`
	AgentTypeID    string              `json:"agent_type_id"`
	AgentSlug      string              `json:"agent_slug"`
	EventType      string              `json:"event_type"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate string              `json:"prompt_template"`
	ScheduleConfig *ScheduleConfig     `json:"schedule_config,omitempty"`
	ManualConfig   *ManualConfig       `json:"manual_config,omitempty"`

	// ─── Admission control ──────────────────────────────────────────────────
	// These tune admission control: the concurrency ceiling, what happens
	// beyond it, and the two safety behaviors. Every one is optional and
	// defaults per kind (see DefaultOverflowForKind and applyAdmissionCreate),
	// so a request that omits them creates exactly the trigger it did before
	// these fields existed.
	MaxParallel      *int16  `json:"max_parallel,omitempty"`
	Overflow         *string `json:"overflow,omitempty"`
	MaxQueueDepth    *int16  `json:"max_queue_depth,omitempty"`
	TimeoutSeconds   *int16  `json:"timeout_seconds,omitempty"`
	DedupeEnabled    *bool   `json:"dedupe_enabled,omitempty"`
	AutopauseEnabled *bool   `json:"autopause_enabled,omitempty"`
}

// UpdateTriggerRequest is the body for PATCH /api/v1/triggers/{id}. The agent
// binding and kind are immutable — recreate the trigger to retarget it.
type UpdateTriggerRequest struct {
	Name           *string             `json:"name,omitempty"`
	EventType      *string             `json:"event_type,omitempty"`
	MatchCondition map[string][]string `json:"match_condition,omitempty"`
	PromptTemplate *string             `json:"prompt_template,omitempty"`
	Enabled        *bool               `json:"enabled,omitempty"`
	ScheduleConfig *ScheduleConfig     `json:"schedule_config,omitempty"`
	ManualConfig   *ManualConfig       `json:"manual_config,omitempty"`
	// The two safety behaviors an operator turns off deliberately. Pointers
	// so that omitting them leaves the stored setting alone: a PATCH that
	// only renames a trigger must not quietly re-arm its auto-pause.
	DedupeEnabled    *bool `json:"dedupe_enabled,omitempty"`
	AutopauseEnabled *bool `json:"autopause_enabled,omitempty"`
	// Admission tuning, editable after creation with the same bounds as
	// create. Changes apply to the next delivery, not work in flight.
	MaxParallel    *int16  `json:"max_parallel,omitempty"`
	Overflow       *string `json:"overflow,omitempty"`
	MaxQueueDepth  *int16  `json:"max_queue_depth,omitempty"`
	TimeoutSeconds *int16  `json:"timeout_seconds,omitempty"`
}

// FireTriggerRequest is the body for POST /api/v1/triggers/{id}/fire.
// Variables are substituted into {{ var.key }} placeholders in the prompt template.
type FireTriggerRequest struct {
	Variables map[string]string `json:"variables,omitempty"`
}

// FireTriggerResponse is returned from POST /api/v1/triggers/{id}/fire.
//
// RunID is empty on acceptance: the fire is durably queued and the run starts
// moments later, so there is no run to name yet. Follow DeliveryID to see the
// outcome. Reporting a run id here would mean starting the run before
// acknowledging, which is the behavior that loses fires on a restart.
type FireTriggerResponse struct {
	DeliveryID string `json:"delivery_id"`
	RunID      string `json:"run_id,omitempty"`
}

// ─── Payload retention ────────────────────────────────────────────────────────

// DefaultPayloadRetentionDays is how long a delivery payload is kept when a
// tenant has not shortened it, and simultaneously the ceiling: a tenant may
// lower this but never raise it, so the platform's exposure to stored
// third-party payload data has a limit no tenant can lift. It matches the
// payload_days CHECK (BETWEEN 1 AND 30) in the admission_control migration —
// validating against a looser bound here would turn a caller's mistake into a
// database error.
const DefaultPayloadRetentionDays = 30

// MinPayloadRetentionDays is the shortest window a tenant may choose. Zero is
// not offered: "keep nothing" is a delete-on-write setting dressed up as
// retention, and a payload that never survives its own delivery cannot be
// debugged at all.
const MinPayloadRetentionDays = 1

// RetentionSettings is a tenant's payload retention window.
//
// IsDefault distinguishes "this tenant chose 30" from "this tenant has chosen
// nothing and inherits 30". The number is the same; whether a later change to
// the platform default would move it is not.
type RetentionSettings struct {
	PayloadDays int  `json:"payload_days"`
	IsDefault   bool `json:"is_default"`
}

// UpdateRetentionRequest is the body for PUT /api/v1/trigger-retention.
type UpdateRetentionRequest struct {
	PayloadDays int `json:"payload_days"`
}

// ─── Event catalog ──────────────────────────────────────────────────────────

// EventTypesResponse answers "what can I write a trigger for on this source?".
//
// Two lists, because neither alone is the answer. Declared is what the adapter
// models: the event types it parses fields out of, which is what a trigger can
// match on richly. Observed is what this source has actually sent, which catches
// the events a provider emits that the adapter has not modeled yet — and tells
// an admin whose trigger never fires that their event type never arrived.
type EventTypesResponse struct {
	Declared []string `json:"declared"`
	Observed []string `json:"observed"`
}

// RedeliverResponse is returned from POST /api/v1/trigger-deliveries/{id}/redeliver.
//
// A redelivery is a new delivery that names the one it replays, never an edit of
// the original, so the caller gets a new id to follow. Whether any trigger
// actually ran is the new delivery's own fan-out, exactly as for a fresh
// arrival.
type RedeliverResponse struct {
	DeliveryID   string `json:"delivery_id"`
	RedeliveryOf string `json:"redelivery_of"`
}

// InputValidationError reports which declared inputs a fire request got wrong.
//
// Per-field rather than a single message: the console marks the offending
// inputs, and a caller fixing three mistakes should not have to discover them
// one round-trip at a time.
type InputValidationError struct {
	Problems map[string]string
}

func (e *InputValidationError) Error() string {
	names := make([]string, 0, len(e.Problems))
	for k := range e.Problems {
		names = append(names, k)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+": "+e.Problems[n])
	}
	return "invalid input — " + strings.Join(parts, "; ")
}

func (e *InputValidationError) Is(target error) bool { return target == ErrValidationFailed }

// ErrValidationFailed lets callers branch on an input-validation failure with
// errors.Is without reaching for the concrete type.
var ErrValidationFailed = errors.New("input validation failed")

// dispatchTimeout is how long this trigger gets to start a run.
//
// It bounds starting, not running: a long-running agent is not a failure, but
// an agent that never accepts the start is. Falls back to the package default
// for triggers created before the setting existed.
func (t Trigger) dispatchTimeout() time.Duration {
	if t.TimeoutSeconds <= 0 {
		return dispatchTimeout
	}
	return time.Duration(t.TimeoutSeconds) * time.Second
}
