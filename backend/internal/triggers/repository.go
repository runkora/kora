package triggers

import (
	"context"
	"time"
)

// Repository is the storage contract for the Triggers feature.
type Repository interface {
	// ─── Sources ────────────────────────────────────────────────────────────
	CreateSource(ctx context.Context, s *WebhookSource) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	// GetSourceByID looks a source up by id alone (no tenant scoping) — used by
	// the public ingest path, which has no authenticated tenant context.
	GetSourceByID(ctx context.Context, id string) (*WebhookSource, error)
	ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error)
	UpdateSource(ctx context.Context, s *WebhookSource) error
	DeleteSource(ctx context.Context, tenantID, id string) error

	// ─── Triggers ───────────────────────────────────────────────────────────
	CreateTrigger(ctx context.Context, t *Trigger) error
	GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error)
	// ListTriggers returns triggers for a tenant, optionally filtered by
	// agentTypeID (empty = all) — the agentTypeID filter backs the per-agent tab.
	ListTriggers(ctx context.Context, tenantID, agentTypeID string) ([]Trigger, error)
	// ListMatchableTriggersForSource returns the webhook triggers a delivery for
	// this source is matched against: the enabled ones, plus the ones the
	// platform auto-paused.
	//
	// Auto-paused triggers are included deliberately. They are filtered out of
	// dispatch by admission, which records 'paused' against the delivery — so an
	// admin can see that their trigger was listening and the platform refused,
	// which is the distinction an auto-paused trigger otherwise loses: with
	// enabled = false alone it is indistinguishable from a deleted one. A trigger
	// a PERSON turned off stays invisible; that silence was asked for.
	ListMatchableTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error)
	// ListMatchableScheduledTriggers returns the scheduled triggers the cron
	// scheduler evaluates, across all tenants: enabled plus auto-paused, on the
	// same reasoning as ListMatchableTriggersForSource.
	ListMatchableScheduledTriggers(ctx context.Context) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error

	// ─── Deliveries ─────────────────────────────────────────────────────────
	// InsertDelivery persists a new delivery row. It returns false (no error)
	// when a row with the same dedup key already exists:
	//   - webhook: same (source_id, provider_delivery_id)
	//   - scheduled: same (trigger_id, scheduled_at)
	//
	// A row that names the delivery it descends from (RedeliveryOf) is lineage
	// rather than an original arrival, and is never deduped against: recording
	// the repeat is the entire point of writing it.
	InsertDelivery(ctx context.Context, d *Delivery) (inserted bool, err error)
	UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error
	ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error)
	// GetDelivery returns one delivery, scoped to the tenant that owns it.
	GetDelivery(ctx context.Context, tenantID, id string) (*Delivery, error)
	// GetDeliveryByProviderID returns the original delivery for a provider's
	// delivery id on a source — the row a repeat arrival is a repeat OF. It
	// ignores lineage rows, so a source that has already been replayed still
	// resolves to the one original.
	GetDeliveryByProviderID(ctx context.Context, sourceID, providerDeliveryID string) (*Delivery, error)

	// InsertScheduledDelivery persists a scheduled tick's delivery, deduped on
	// (trigger_id, scheduled_at). This is the scheduler's concurrency control:
	// a replica that loses the insert race stops, so a schedule fires at most
	// once per minute no matter how many replicas are running.
	InsertScheduledDelivery(ctx context.Context, d *Delivery) (inserted bool, err error)

	// DueScheduledMinutesWithoutDelivery returns the minutes since `since` for
	// which this trigger has no delivery row — the candidates for a fire that
	// was missed while the platform was down. The caller filters them against
	// the trigger's own cron expression.
	DueScheduledMinutesWithoutDelivery(ctx context.Context, triggerID string, since time.Time) ([]time.Time, error)

	// DeriveDeliveryStatus recomputes a delivery's status from its per-trigger
	// outcomes: dispatched if any run dispatched, failed if runs exist and none
	// did, skipped if no trigger matched. The summary is never authored
	// independently — writing it inline from the first success is what makes
	// "started two, denied one" unrepresentable (ADR Decision 3).
	DeriveDeliveryStatus(ctx context.Context, deliveryID string) error

	// RecordSourceRejection counts an inbound request that failed
	// authentication and stores its reason on the source.
	//
	// This deliberately does not create a delivery: the request is
	// unauthenticated, so a row per request would let anyone on the internet
	// grow the table. Updating one already-existing row bounds the write
	// regardless of volume (ADR Decision 4).
	RecordSourceRejection(ctx context.Context, sourceID, reason string) error

	// RecordAccepted counts an inbound request that authenticated and was
	// taken. The counterpart to RecordSourceRejection: a rejection count is
	// only readable next to the number of requests that got through.
	RecordAccepted(ctx context.Context, sourceID string) error

	// ClaimReplayKey records a body fingerprint against a source and reports
	// whether the source may take that body under this provider delivery id.
	// False means the same bytes have already been handled under a DIFFERENT
	// id, which is the replay of a captured, correctly-signed delivery that a
	// rewritten delivery-id header would otherwise disguise as a fresh arrival.
	// A retry under the same id stays true: that is an ordinary duplicate.
	ClaimReplayKey(ctx context.Context, sourceID string, bodyHash []byte, providerDeliveryID string, window time.Duration) (fresh bool, err error)
	// PruneReplayKeys drops fingerprints past the window.
	PruneReplayKeys(ctx context.Context, window time.Duration) (int64, error)

	// CountRedeliveries is how many replays already descend from a delivery,
	// so replaying can be bounded per original.
	CountRedeliveries(ctx context.Context, originalID string) (int, error)

	// ─── Admission control ──────────────────────────────────────────────────
	// EnqueuePending durably records a fire waiting for a slot. Every fire goes
	// through here, so an acknowledged fire always starts.
	EnqueuePending(ctx context.Context, p *PendingDelivery) error
	// ClaimPending takes up to limit unclaimed fires for triggers under their
	// ceiling, using FOR UPDATE SKIP LOCKED so replicas do not contend.
	ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error)
	// ReleasePending removes a claimed row once its dispatch has settled.
	ReleasePending(ctx context.Context, id string) error
	// TouchPendingClaim re-stamps a claim at the moment its dispatch starts, so
	// the stale-claim window bounds one dispatch rather than a whole batch.
	TouchPendingClaim(ctx context.Context, id string) error
	// ReclaimStalePending un-claims fires whose worker died mid-dispatch,
	// returning the ids it took back so the caller can say so.
	//
	// Without it a pod that dies between claiming and dispatching strands the
	// row forever: nothing re-claims it, so the fire never starts, and it holds
	// one of its trigger's parallel slots for good. olderThan must stay above
	// the longest dispatch a trigger may legitimately take, or this restarts
	// work that is still running.
	ReclaimStalePending(ctx context.Context, olderThan time.Duration) ([]string, error)
	// InFlightCount is how many runs this trigger has started that have not yet
	// reached a terminal state.
	InFlightCount(ctx context.Context, triggerID string) (int, error)
	// SupersedePending removes everything waiting for a trigger, returning the
	// displaced rows so the caller can record them.
	SupersedePending(ctx context.Context, triggerID string) ([]PendingDelivery, error)
	// TrimPending evicts the oldest waiting rows beyond depth, returning them.
	TrimPending(ctx context.Context, triggerID string, depth int) ([]PendingDelivery, error)
	// DrainPending removes everything still WAITING for a trigger, returning it.
	// Claimed rows are left to the worker already dispatching them.
	DrainPending(ctx context.Context, triggerID string) ([]PendingDelivery, error)
	// DrainOrphanedPending removes waiting fires whose trigger was deleted or
	// switched off, up to limit. The backstop for a fire admitted in the window
	// between a disable committing and its drain running: nothing else collects
	// those, because claiming requires an enabled trigger and the stale-claim
	// reaper only touches rows already claimed.
	DrainOrphanedPending(ctx context.Context, limit int) ([]OrphanedPending, error)
	// UnclaimedPendingCount is how many fires are waiting on a trigger.
	UnclaimedPendingCount(ctx context.Context, triggerID string) (int, error)

	// ─── Delivery safety ────────────────────────────────────────────────────
	// BumpConsecutiveFailures increments the failure counter and returns the
	// new value, so the caller can auto-pause on the threshold.
	BumpConsecutiveFailures(ctx context.Context, triggerID string) (int, error)
	// ResetConsecutiveFailures clears the counter after a successful start.
	ResetConsecutiveFailures(ctx context.Context, triggerID string) error
	// AutoPause disables a trigger and records that the platform did it, which
	// the console distinguishes from a person turning it off.
	AutoPause(ctx context.Context, triggerID string) error
	// PurgePayloads nulls payload bodies past each tenant's retention, leaving
	// every other column intact so history survives the payload.
	PurgePayloads(ctx context.Context, defaultDays int) (int64, error)
	// GetRetention returns a tenant's chosen payload retention window.
	// configured is false when the tenant has never set one, which the caller
	// reports as the inherited default rather than inventing a stored value.
	GetRetention(ctx context.Context, tenantID string) (days int, configured bool, err error)
	// SetRetention upserts a tenant's payload retention window.
	SetRetention(ctx context.Context, tenantID string, days int) error

	// ObservedEventTypes returns the distinct event types this source has
	// actually delivered. It is the half of the event catalog no adapter can
	// know: what a provider really sends, including events the adapter has not
	// modeled.
	ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error)

	// PendingStats summarizes the delivery queue in one query, for the metrics
	// callback. One query rather than three: it is polled on the collection
	// interval, and a gauge that costs three round trips per scrape is a cost
	// the thing it measures did not have.
	PendingStats(ctx context.Context) (PendingStats, error)

	// InsertDeliveryRun records one per-trigger outcome for a delivery.
	InsertDeliveryRun(ctx context.Context, r *DeliveryRun) error
	// ListDeliveryRunsForDeliveries returns the per-trigger dispatch records for
	// the given delivery ids, scoped to the tenant, keyed by delivery id. Used to
	// attach the full run fan-out to a delivery listing without an N+1 query.
	ListDeliveryRunsForDeliveries(ctx context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryRun, error)
}

// DeliveryFilter narrows a delivery listing. Empty/zero fields are ignored.
type DeliveryFilter struct {
	SourceID  string
	TriggerID string
	Kind      string
	Limit     int
}

// PendingStats is a point-in-time view of the delivery queue.
//
// Depth alone does not say whether a queue is healthy: a steady depth of 50 that
// drains in a second is fine, and a depth of 2 stuck for an hour is not. The age
// of the oldest waiting fire is the half that catches a stalled worker.
type PendingStats struct {
	Waiting int64
	Claimed int64
	// OldestUnclaimedAge is how long the oldest waiting fire has been waiting,
	// or zero when nothing is waiting.
	OldestUnclaimedAge time.Duration
}

// ScheduledTick describes one invocation of the cron scheduler for a trigger.
type ScheduledTick struct {
	Trigger     Trigger
	ScheduledAt time.Time
}
