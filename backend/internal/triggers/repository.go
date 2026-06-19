package triggers

import "context"

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
	// ListEnabledTriggersForSource returns the enabled triggers for a source,
	// used by the ingest path to match a delivery.
	ListEnabledTriggersForSource(ctx context.Context, sourceID string) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger) error
	DeleteTrigger(ctx context.Context, tenantID, id string) error

	// ─── Deliveries ─────────────────────────────────────────────────────────
	// InsertDelivery persists a new delivery row. It returns false (no error)
	// when a row with the same (source_id, provider_delivery_id) already exists —
	// the redelivery dedup signal.
	InsertDelivery(ctx context.Context, d *Delivery) (inserted bool, err error)
	UpdateDeliveryOutcome(ctx context.Context, id, status string, triggerID, runID, errMsg *string) error
	ListDeliveries(ctx context.Context, tenantID string, filter DeliveryFilter) ([]Delivery, error)
}

// DeliveryFilter narrows a delivery listing. Empty fields are ignored.
type DeliveryFilter struct {
	SourceID string
	Limit    int
}
