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
	// ListEnabledTriggersForSource returns the enabled webhook triggers for a
	// source, used by the ingest path to match a delivery.
	ListEnabledTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error)
	// ListEnabledScheduledTriggers returns all enabled scheduled triggers across
	// all tenants, used by the cron scheduler.
	ListEnabledScheduledTriggers(ctx context.Context) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error

	// ─── Deliveries ─────────────────────────────────────────────────────────
	// InsertDelivery persists a new delivery row. It returns false (no error)
	// when a row with the same dedup key already exists:
	//   - webhook: same (source_id, provider_delivery_id)
	//   - scheduled: same (trigger_id, scheduled_at)
	InsertDelivery(ctx context.Context, d *Delivery) (inserted bool, err error)
	UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error
	ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error)

	// InsertScheduledDelivery persists a scheduled tick's delivery, deduped on
	// (trigger_id, scheduled_at). This is the scheduler's concurrency control:
	// a replica that loses the insert race stops, so a schedule fires at most
	// once per minute no matter how many replicas are running.
	InsertScheduledDelivery(ctx context.Context, d *Delivery) (inserted bool, err error)

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

// ScheduledTick describes one invocation of the cron scheduler for a trigger.
type ScheduledTick struct {
	Trigger     Trigger
	ScheduledAt time.Time
}
