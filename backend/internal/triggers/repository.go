package triggers

import (
	"context"
	"time"
)

// Repository is the storage contract for the Triggers feature: the union of the
// role interfaces below, satisfied whole by the Postgres implementation.
//
// The split is documentation with teeth. Each component declares only the role
// it needs — the Worker takes a PendingQueue, the retention job a
// RetentionStore — so what a component may touch is visible in its constructor
// rather than implied by a several-dozen-method surface, and a change of mind
// about that shows up as a compile error.
type Repository interface {
	SourceStore
	TriggerStore
	DeliveryStore
	PendingQueue
	SafetyStore
	RetentionStore
	RoutingStore
}

// RoutingStore is the decision record of routed triggers. A routed trigger's
// settings and candidates are written and read with the trigger itself.
type RoutingStore interface {
	// LatestJoinKeyRun returns the run and agent of the newest decision of this
	// trigger with this join key and a run. found is false when there is none.
	LatestJoinKeyRun(ctx context.Context, tenantID, triggerID, joinKey string) (runID, agentTypeID string, found bool, err error)
	// InsertRoutingDecision records one routed delivery's decision. A second
	// decision for the same delivery and trigger is ignored.
	InsertRoutingDecision(ctx context.Context, d *RoutingDecision) error
}

// SourceStore is the webhook sources a tenant registers, and the counters the
// unauthenticated ingest path keeps on them.
type SourceStore interface {
	CreateSource(ctx context.Context, s *WebhookSource) error
	GetSource(ctx context.Context, tenantID, id string) (*WebhookSource, error)
	// GetSourceByID looks a source up by id alone (no tenant scoping) — used by
	// the public ingest path, which has no authenticated tenant context.
	//
	// Deliberately unscoped, and fenced off here rather than sitting beside the
	// tenant-scoped reads without comment: an inbound webhook arrives with a
	// source id and a signature and nothing else, so the tenant is a conclusion
	// of the lookup rather than an input to it. Every caller that HAS a tenant
	// must use GetSource.
	GetSourceByID(ctx context.Context, id string) (*WebhookSource, error)
	ListSources(ctx context.Context, tenantID string) ([]WebhookSource, error)
	UpdateSource(ctx context.Context, s *WebhookSource) error
	DeleteSource(ctx context.Context, tenantID, id string) error

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

	// ObservedEventTypes returns the distinct event types this source has
	// actually delivered. It is the half of the event catalog no adapter can
	// know: what a provider really sends, including events the adapter has not
	// modeled.
	ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error)
}

// TriggerStore is the triggers themselves, plus the two matchable listings the
// ingest and scheduler paths dispatch from.
type TriggerStore interface {
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
	// UpdateTrigger persists the mutable fields of a trigger. writeEnablement
	// gates the enabled / auto_paused_at columns: an edit that did not touch
	// enablement (a rename, a filter change) passes false so it cannot write
	// back the stale enabled/auto_paused_at it read at the start of the request
	// and silently revert a concurrent auto-pause.
	UpdateTrigger(ctx context.Context, t *Trigger, writeEnablement bool) error
	// UpdateTriggerGrant is UpdateTrigger for an edit that replaced the
	// trigger's grant: it writes only while the stored grant is still
	// oldGrantID, and returns ErrTriggerConflict otherwise.
	UpdateTriggerGrant(ctx context.Context, t *Trigger, writeEnablement bool, oldGrantID *string) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error
}

// DeliveryStore is the audit spine: what arrived, what each trigger did with it,
// and the derived summary that ties the two together.
type DeliveryStore interface {
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

	// InsertDeliveryRun records one per-trigger outcome for a delivery.
	InsertDeliveryRun(ctx context.Context, r *DeliveryRun) error
	// ListDeliveryRunsForDeliveries returns the per-trigger dispatch records for
	// the given delivery ids, scoped to the tenant, keyed by delivery id. Used to
	// attach the full run fan-out to a delivery listing without an N+1 query.
	ListDeliveryRunsForDeliveries(ctx context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryRun, error)
	// InsertDeliveryKey records the coalescing key a trigger rendered for a
	// delivery. It is written before the fire is enqueued, so the key is
	// readable for as long as the fire waits.
	InsertDeliveryKey(ctx context.Context, k *DeliveryKey) error
	// ListDeliveryKeysForDeliveries returns the recorded keys for the given
	// delivery ids, scoped to the tenant, keyed by delivery id.
	ListDeliveryKeysForDeliveries(ctx context.Context, tenantID string, deliveryIDs []string) (map[string][]DeliveryKey, error)

	// ClaimReplayKey records a body fingerprint against a source and reports
	// whether the source may take that body under this provider delivery id.
	// False means the same bytes have already been handled under a DIFFERENT
	// id, which is the replay of a captured, correctly-signed delivery that a
	// rewritten delivery-id header would otherwise disguise as a fresh arrival.
	// A retry under the same id stays true: that is an ordinary duplicate.
	ClaimReplayKey(ctx context.Context, sourceID string, bodyHash []byte, providerDeliveryID string, window time.Duration) (fresh bool, err error)

	// CountRedeliveries is how many replays already descend from a delivery,
	// so replaying can be bounded per original.
	CountRedeliveries(ctx context.Context, originalID string) (int, error)
}

// PendingQueue is the durable fire queue: everything between a fire being
// accepted and its run starting.
type PendingQueue interface {
	// EnqueuePending durably records a fire waiting for a slot. Every fire goes
	// through here, so an acknowledged fire always starts.
	EnqueuePending(ctx context.Context, p *PendingDelivery) error
	// ClaimPending takes up to limit unclaimed fires for triggers under their
	// ceiling, using FOR UPDATE SKIP LOCKED so replicas do not contend. A fire
	// still inside its coalescing window is not claimable.
	ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error)
	// ReleasePending removes a claimed row once its dispatch has settled.
	ReleasePending(ctx context.Context, id string) error
	// TouchPendingClaims re-stamps a set of claims, so the stale-claim window
	// bounds one dispatch rather than a whole batch. The worker re-stamps the
	// rows still waiting behind each dispatch, keeping them inside the window
	// while it works through the batch serially.
	TouchPendingClaims(ctx context.Context, ids []string) error
	// ReclaimStalePending un-claims fires whose worker died mid-dispatch,
	// returning the ids it took back so the caller can say so.
	//
	// Without it a pod that dies between claiming and dispatching strands the
	// row forever: nothing re-claims it, so the fire never starts, and it holds
	// one of its trigger's parallel slots for good. olderThan must stay above
	// the longest dispatch a trigger may legitimately take, or this restarts
	// work that is still running.
	ReclaimStalePending(ctx context.Context, olderThan time.Duration) ([]string, error)
	// SupersedeAndEnqueue atomically replaces a trigger's waiting backlog with
	// one new fire: it displaces every unclaimed row (returning them so the
	// caller records each as superseded) and enqueues the replacement in one
	// transaction, under a lock on the trigger.
	SupersedeAndEnqueue(ctx context.Context, p *PendingDelivery) ([]PendingDelivery, error)
	// EnqueueCoalescing enqueues a fire under its coalescing key. The fire waits
	// window before it can be claimed.
	//
	// If the trigger already has a waiting row with the same key that is still
	// inside its window, that row is replaced in place: the newest fire's
	// payload is kept, the original dispatch time is kept, and the replaced
	// fire's delivery id is returned so the caller can record it as superseded.
	// Otherwise a new row opens a new window and the returned id is empty.
	// Claimed rows are never replaced. The lookup and the write are one
	// transaction under a lock on the trigger, so concurrent fires with one key
	// leave one row.
	EnqueueCoalescing(ctx context.Context, p *PendingDelivery, window time.Duration) (replacedDeliveryID string, err error)
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
	// EnqueueWithinCeiling admits a fire only if the trigger is under
	// maxParallel, deciding and inserting under one lock. Counting and then
	// enqueueing separately lets two simultaneous fires both pass the check,
	// which turns "refuse" into "wait" for whichever one loses. Reports false
	// when the fire was refused.
	EnqueueWithinCeiling(ctx context.Context, p *PendingDelivery, maxParallel int) (bool, error)

	// PendingStats summarizes the delivery queue in one query, for the metrics
	// callback. One query rather than three: it is polled on the collection
	// interval, and a gauge that costs three round trips per scrape is a cost
	// the thing it measures did not have.
	PendingStats(ctx context.Context) (PendingStats, error)
}

// SafetyStore is the auto-pause state: the consecutive-failure counter and the
// pause it eventually trips.
type SafetyStore interface {
	// BumpConsecutiveFailures increments the failure counter and returns the
	// new value, so the caller can auto-pause on the threshold.
	BumpConsecutiveFailures(ctx context.Context, triggerID string) (int, error)
	// ResetConsecutiveFailures clears the counter after a successful start.
	ResetConsecutiveFailures(ctx context.Context, triggerID string) error
	// AutoPause disables a trigger and records that the platform did it, which
	// the console distinguishes from a person turning it off.
	AutoPause(ctx context.Context, triggerID string) error
}

// RetentionStore is what the platform promises about data at rest: the tenant's
// chosen window, and the purges that keep it.
type RetentionStore interface {
	// PurgePayloads nulls payload bodies past each tenant's retention, leaving
	// every other column intact so history survives the payload.
	PurgePayloads(ctx context.Context, defaultDays int) (int64, error)
	// PruneReplayKeys drops fingerprints past the window.
	PruneReplayKeys(ctx context.Context, window time.Duration) (int64, error)
	// GetRetention returns a tenant's chosen payload retention window.
	// configured is false when the tenant has never set one, which the caller
	// reports as the inherited default rather than inventing a stored value.
	GetRetention(ctx context.Context, tenantID string) (days int, configured bool, err error)
	// SetRetention upserts a tenant's payload retention window.
	SetRetention(ctx context.Context, tenantID string, days int) error
}

// DeliveryFilter narrows a delivery listing. Empty/zero fields are ignored.
type DeliveryFilter struct {
	SourceID  string
	TriggerID string
	Kind      string
	// ReceivedAfter and ReceivedBefore bound received_at: at or after, and
	// before. Zero means unbounded.
	ReceivedAfter  time.Time
	ReceivedBefore time.Time
	// OmitPayload leaves the full payload out of each delivery. The excerpt,
	// outcomes and runs stay.
	OmitPayload bool
	Limit       int
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
