// Package triggers implements the kora Triggers feature: the front door for the
// ways something can start in the platform. Three kinds ship today: an inbound
// webhook, a cron schedule, and a manual fire. See
// docs/adr/2026-06-20-extended-trigger-types.md and the original
// docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
//
// The package is hosted by the orbit-triggers binary, deliberately separate from
// identity: webhook ingestion is the one unauthenticated, internet-reachable
// surface in the platform, and it should not share a process with the service
// that mints every token. It is a self-contained feature package: it never
// imports other feature packages. It reaches the agent control plane through a
// typed platform client and mints each trigger's identity via the TokenMinter
// interface (satisfied by a typed identity client in main).
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
	// ErrProviderNotFound is ErrUnknownProvider for a provider named in a URL path.
	ErrProviderNotFound = errors.New("webhook provider not found")
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

	// ErrReplayedPayload is returned when a source is sent a body it has
	// already handled inside the replay window, under a different provider
	// delivery id. The delivery id is not part of what a provider signs, so
	// without this a captured, correctly-signed body could be re-sent forever
	// with a fresh id each time and every copy would start a run.
	ErrReplayedPayload = errors.New("this exact payload has already been delivered to this source")
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

	// AcceptedCount is how many inbound requests authenticated and were taken.
	// It is what makes RejectionCount readable: five rejections against a
	// million accepted is background noise from the internet, and five against
	// five is a signing secret that is simply wrong. Always serialized, never
	// omitted at zero — "none accepted yet" is the answer that matters most.
	AcceptedCount int64 `json:"accepted_count"`

	// SecretRotatedAt dates the signing secret currently on this source.
	// Replaying a stored payload does not re-verify it, so a payload that
	// arrived under an older secret is refused: rotation is what an operator
	// does when the previous secret may be in someone else's hands.
	SecretRotatedAt *time.Time `json:"secret_rotated_at,omitempty"`
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
	// SenderRestriction limits who can fire a webhook trigger; nil fires for every sender.
	SenderRestriction *SenderRestriction `json:"sender_restriction,omitempty"`
	// MatchContains maps a field to substrings, one of which it must contain.
	MatchContains map[string][]string `json:"match_contains,omitempty"`
	// Coalesce collapses bursts of fires about one subject into one run; nil
	// dispatches every fire. Webhook triggers only.
	Coalesce *CoalesceConfig `json:"coalesce,omitempty"`
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
	// TimeoutSeconds bounds the start request, which returns once orbit-agents
	// has queued the run. A long-running agent is not a failure.
	TimeoutSeconds   int16 `json:"timeout_seconds"`
	DedupeEnabled    bool  `json:"dedupe_enabled"`
	AutopauseEnabled bool  `json:"autopause_enabled"`
	// ConsecutiveFailures counts failures since the last success. A policy
	// denial counts; an admission rejection does not.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// AutoPausedAt is set when the platform disabled this trigger itself, which
	// the console shows differently from a trigger a person turned off.
	AutoPausedAt *time.Time `json:"auto_paused_at,omitempty"`

	// PendingCount is how many fires are waiting for a slot on this trigger,
	// as of the read. Only unclaimed fires count: a claimed one is already being
	// dispatched, and showing it as waiting would promise work that has started.
	//
	// This is the same quantity MaxQueueDepth bounds — unclaimed depth — so the
	// two are directly comparable: pending_count approaching max_queue_depth is
	// a trigger about to start evicting its oldest waiting fires. Fires being
	// dispatched right now are in neither number; they are bounded by
	// MaxParallel instead.
	//
	// Always serialized, never omitted at zero. "Nothing is waiting" is the
	// answer an operator most needs to trust, and an absent field reads as
	// "unknown" rather than "none". Derived per read, not a stored column.
	PendingCount int `json:"pending_count"`
}

// DisabledByPerson reports whether a person turned this trigger off, as
// opposed to the platform pausing it after sustained failure.
//
// The two are different answers to a fire. A person's OFF is a decision to
// respect: refuse with nothing recorded, and never work around it with a
// replay. The platform's pause is a state to surface: carry the fire through
// to admission so it records 'paused', because otherwise an auto-paused
// trigger and a deleted one look identical in the delivery history. One
// predicate, so the two callers cannot drift on which is which.
func (t *Trigger) DisabledByPerson() bool {
	return !t.Enabled && t.AutoPausedAt == nil
}

// Delivery is the durable record of a trigger delivery and its outcome.
// TriggerID/RunID summarize the first dispatched run; Runs carries the complete
// per-trigger fan-out (one entry per matched trigger) and is populated on reads.
type Delivery struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	SourceID  string  `json:"source_id,omitempty"`
	TriggerID *string `json:"trigger_id,omitempty"`
	// TriggerName is the trigger's name as it was when this delivery arrived.
	//
	// Captured rather than joined: deliveries have no foreign key to triggers so
	// history survives a deletion, and a join would return nothing for exactly
	// the deleted trigger someone is trying to read about. A rename after the
	// fact must not rewrite what this row says happened either.
	TriggerName        string  `json:"trigger_name,omitempty"`
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
	// CoalesceKeys are the keys the triggers this delivery matched rendered for
	// it, one per coalescing trigger. Populated on reads.
	CoalesceKeys []DeliveryKey `json:"coalesce_keys,omitempty"`
}

// DeliveryKey is the coalescing key one trigger rendered for one delivery.
//
// It is per trigger because each trigger writes its own key template. It is
// written when the fire is enqueued, so it is readable while the fire waits.
type DeliveryKey struct {
	DeliveryID string `json:"-"`
	TenantID   string `json:"-"`
	TriggerID  string `json:"trigger_id"`
	// Key is the rendered key. Empty when KeyTooLong is set.
	Key string `json:"key,omitempty"`
	// KeyTooLong marks a render over MaxCoalesceKeyBytes. Such a fire did not coalesce.
	KeyTooLong bool `json:"key_too_long,omitempty"`
}

// MaxCoalesceKeyBytes is the longest rendered key that coalesces. It matches the
// length check on trigger_delivery_keys.
const MaxCoalesceKeyBytes = 256

// DeliveryRun records a single matched-trigger dispatch for a delivery — the
// authoritative one-to-many audit detail behind a delivery. RunID is nil when
// the dispatch failed (Status == RunStatusFailed), in which case Error explains.
type DeliveryRun struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	DeliveryID string `json:"delivery_id"`
	TriggerID  string `json:"trigger_id"`
	// TriggerName is the trigger's name when this outcome was recorded. It is
	// what the console shows per trigger in a delivery's fan-out; without it a
	// deleted trigger leaves a row identified only by an id nobody recognizes,
	// which is the state US-004 exists to fix. Empty on rows written before this
	// was captured, and on the rare outcome recorded when the trigger could no
	// longer be read at all.
	TriggerName string    `json:"trigger_name,omitempty"`
	RunID       *string   `json:"run_id,omitempty"`
	Status      string    `json:"status"`
	Error       *string   `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// SupersededBy is the delivery whose fire replaced this one. Set only on a
	// superseded outcome.
	SupersededBy *string `json:"superseded_by,omitempty"`
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
// For webhook kind: SourceID, EventType are required; MatchCondition and
// SenderRestriction are optional.
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
	// SenderRestriction applies to webhook triggers only.
	SenderRestriction *SenderRestriction `json:"sender_restriction,omitempty"`
	// MatchContains applies to webhook triggers only.
	MatchContains map[string][]string `json:"match_contains,omitempty"`
	// Coalesce applies to webhook triggers only, and needs overflow "wait".
	Coalesce *CoalesceConfig `json:"coalesce,omitempty"`

	// Embedded, not repeated: create and update accept the same settings with
	// the same bounds, and JSON field promotion keeps them flat on the wire.
	AdmissionSettings
}

// AdmissionSettings is the admission policy a caller may tune on a trigger: the
// concurrency ceiling, what happens beyond it, and the two safety behaviors.
//
// Every field is optional. On create, an omitted field takes the platform
// default (see applyAdmissionCreate); on update, it leaves the stored value
// alone, so a PATCH that only renames a trigger cannot quietly re-arm its
// auto-pause.
//
// It is embedded in both request types rather than declared twice: one set of
// fields, one Validate, one applyTo. The embedding is anonymous so the wire
// format stays flat — the fields are promoted into the enclosing object.
type AdmissionSettings struct {
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
	// SenderRestriction replaces the stored restriction when present; one with
	// both lists empty removes it.
	SenderRestriction *SenderRestriction `json:"sender_restriction,omitempty"`
	// MatchContains replaces the stored map when present; an empty map removes it.
	MatchContains map[string][]string `json:"match_contains,omitempty"`
	// Coalesce replaces the stored setting when present; an empty object removes it.
	Coalesce *CoalesceConfig `json:"coalesce,omitempty"`
	// Editable after creation with exactly the create-time bounds. Changes
	// apply to the next delivery, not to work already in flight.
	AdmissionSettings
}

// FireTriggerRequest is the body for POST /api/v1/triggers/{id}/fire.
// Variables are substituted into {{ var.key }} placeholders in the prompt template.
type FireTriggerRequest struct {
	Variables map[string]string `json:"variables,omitempty"`
}

// FireTriggerResponse is returned from POST /api/v1/triggers/{id}/fire.
//
// The delivery id and nothing else. There is deliberately no run id: the fire is
// durably queued and the run starts moments later, so at the moment of the
// acknowledgement no run exists to name. The field used to be declared and was
// never once populated, which is worse than absent — a caller reading the type
// would code against a value they can never receive. Follow the delivery id to
// see what the fire did.
type FireTriggerResponse struct {
	DeliveryID string `json:"delivery_id"`
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

// EventTypesResponse lists the event types a trigger can listen for.
//
// Declared is every event type in the provider's schema. Observed is what a
// source has sent, including events newer than the schema; a provider's catalog
// has none.
// Associations are the values a sender restriction may name, empty when the
// provider identifies no sender.
type EventTypesResponse struct {
	Declared     []string `json:"declared"`
	Observed     []string `json:"observed,omitempty"`
	Associations []string `json:"associations"`
}

// FieldsResponse lists the fields a trigger on one event type can reference.
type FieldsResponse struct {
	Fields []Field `json:"fields"`
}

// ─── Sender restriction ─────────────────────────────────────────────────────

// SenderRestriction limits which senders can fire a webhook trigger. A sender
// passes when its login is listed or its association is listed.
type SenderRestriction struct {
	Logins       []string `json:"logins,omitempty"`
	Associations []string `json:"associations,omitempty"`
}

// empty reports whether the restriction lets every sender through.
func (r *SenderRestriction) empty() bool {
	return r == nil || (len(r.Logins) == 0 && len(r.Associations) == 0)
}

// CoalesceConfig makes a webhook trigger start one run for a burst of fires
// about the same subject.
//
// Key is a template in the prompt_template syntax, rendered per fire against
// the event. Fires of one trigger that render the same key within WindowSeconds
// of the first collapse into one run from the newest payload. Both fields are
// required for it to apply, and the platform keeps no default key: what the
// subject of an event is depends on the provider and on what the trigger does.
type CoalesceConfig struct {
	Key           string `json:"key"`
	WindowSeconds int    `json:"window_seconds"`
}

// WebhookConfig is the kind_config for a webhook trigger.
type WebhookConfig struct {
	SenderRestriction *SenderRestriction  `json:"sender_restriction,omitempty"`
	MatchContains     map[string][]string `json:"match_contains,omitempty"`
	Coalesce          *CoalesceConfig     `json:"coalesce,omitempty"`
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

func (e *InputValidationError) Error() string { return inputProblemSummary(e.Problems) }

// inputProblemSummary renders per-field problems as one compact line, e.g.
//
//	invalid input: env: required; row_limit: must be a number
//
// One formatter for both the error a caller sees and the reason stored on the
// rejected delivery, so the history and the response cannot describe the same
// refusal differently. Sorted by field name to keep it stable: an audit line
// that reorders itself between reads is not one anybody can diff.
func inputProblemSummary(problems map[string]string) string {
	names := make([]string, 0, len(problems))
	for k := range problems {
		names = append(names, k)
	}
	slices.Sort(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+": "+problems[n])
	}
	return "invalid input: " + strings.Join(parts, "; ")
}

func (e *InputValidationError) Is(target error) bool { return target == ErrValidationFailed }

// ErrValidationFailed lets callers branch on an input-validation failure with
// errors.Is without reaching for the concrete type.
var ErrValidationFailed = errors.New("input validation failed")

// dispatchTimeout is how long this trigger's start request may take. orbit-agents
// queues the run and answers at once. Falls back to the package default for
// triggers created before the setting existed.
func (t Trigger) dispatchTimeout() time.Duration {
	if t.TimeoutSeconds <= 0 {
		return dispatchTimeout
	}
	return time.Duration(t.TimeoutSeconds) * time.Second
}

// ─── Schedule preview ─────────────────────────────────────────────────────────

// SchedulePreviewRequest asks what a cron expression would actually do.
type SchedulePreviewRequest struct {
	ScheduleConfig *ScheduleConfig `json:"schedule_config"`
}

// SchedulePreviewResponse returns the upcoming fire times in the schedule's own
// timezone, plus a human-readable rendering of each.
type SchedulePreviewResponse struct {
	NextFires []SchedulePreviewFire `json:"next_fires"`
	Timezone  string                `json:"timezone"`
}

// SchedulePreviewFire is one upcoming fire.
type SchedulePreviewFire struct {
	At    time.Time `json:"at"`
	Label string    `json:"label"`
}
