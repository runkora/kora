package triggers

import (
	"context"
	"sync"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// outcomeStore is the two writes recording an outcome takes.
type outcomeStore interface {
	InsertDeliveryRun(ctx context.Context, r *DeliveryRun) error
	DeriveDeliveryStatus(ctx context.Context, deliveryID string) error
}

// outcomeRecorder writes the per-trigger outcome rows that make up a delivery's
// fan-out, and keeps the delivery's own status derived from them.
//
// It exists so that "record the outcome, then re-derive the delivery" is written
// once. That pairing is the audit spine (docs/adr/2026-06-20-extended-trigger-types.md,
// Decision 3): a delivery's status is never set directly, it is always a
// function of the outcomes recorded against it, so an outcome written without a
// derive leaves a delivery reading as still in progress forever. The service and
// admission both record outcomes, and one type keeps that invariant for both.
//
// Failures are logged rather than returned throughout: the outcome row is a
// record of something that already happened, and there is no caller who could
// act on the error.
type outcomeRecorder struct {
	repo    outcomeStore
	logger  logging.Logger
	metrics *Metrics // nil records nothing
	// providers caches trigger → provider so counting an outcome does not read
	// the trigger and its source every time. Nil disables the cache.
	providers *providerCache
}

// providerCacheTTL bounds how stale a cached provider can be. A trigger's
// source rarely changes; the label tolerates a few minutes of lag.
const providerCacheTTL = 5 * time.Minute

// providerCacheMax bounds the cache. At the limit it is emptied, not evicted
// entry by entry: it is a metrics label cache, refilling is cheap.
const providerCacheMax = 10000

type providerCacheEntry struct {
	provider string
	expires  time.Time
}

type providerCache struct {
	mu sync.Mutex
	m  map[string]providerCacheEntry
}

func newProviderCache() *providerCache {
	return &providerCache{m: map[string]providerCacheEntry{}}
}

func (c *providerCache) get(triggerID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[triggerID]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.provider, true
}

func (c *providerCache) put(triggerID, provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= providerCacheMax {
		c.m = map[string]providerCacheEntry{}
	}
	c.m[triggerID] = providerCacheEntry{provider: provider, expires: time.Now().Add(providerCacheTTL)}
}

// providerLookup resolves the provider behind a trigger, for the delivery
// metric's provider label. The recorder's store satisfies it where it can read
// triggers and sources; otherwise the provider is reported as generic.
type providerLookup interface {
	GetTrigger(ctx context.Context, tenantID, id string) (*Trigger, error)
	GetSourceByID(ctx context.Context, id string) (*WebhookSource, error)
}

// count records the outcome on the delivery metric.
func (o outcomeRecorder) count(ctx context.Context, dr *DeliveryRun) {
	if o.metrics == nil {
		return
	}
	o.metrics.recordOutcome(ctx, o.provider(ctx, dr), dr.Status)
}

// provider resolves the provider behind the outcome's trigger. A trigger with no
// source is generic and cached as such; a failed read is not cached, so the next
// outcome retries it, and reports generic for this one.
func (o outcomeRecorder) provider(ctx context.Context, dr *DeliveryRun) string {
	if o.providers != nil {
		if p, ok := o.providers.get(dr.TriggerID); ok {
			return p
		}
	}
	l, ok := o.repo.(providerLookup)
	if !ok {
		return ProviderGeneric
	}
	t, err := l.GetTrigger(ctx, dr.TenantID, dr.TriggerID)
	if err != nil {
		return ProviderGeneric
	}
	provider := ProviderGeneric
	if t.SourceID != "" {
		src, err := l.GetSourceByID(ctx, t.SourceID)
		if err != nil {
			return ProviderGeneric
		}
		provider = src.Provider
	}
	if o.providers != nil {
		o.providers.put(dr.TriggerID, provider)
	}
	return provider
}

// record writes one outcome and re-derives the delivery. This is the default;
// use insert plus derive only when writing a whole fan-out, where one derive at
// the end is both cheaper and more accurate than one per row.
func (o outcomeRecorder) record(ctx context.Context, dr *DeliveryRun) {
	if o.insert(ctx, dr) {
		o.derive(ctx, dr.DeliveryID)
	}
}

// insert writes one per-trigger outcome, reporting whether it landed. The caller
// owes the delivery a derive.
//
// TriggerName is captured onto the row rather than joined at read time: a
// delivery outlives the trigger it names, and the console showing a bare id for
// a deleted trigger is exactly the gap US-004 closes. It is empty only where the
// name is genuinely unknown — an outcome recorded because the trigger could not
// be read at all.
func (o outcomeRecorder) insert(ctx context.Context, dr *DeliveryRun) bool {
	if dr.ID == "" {
		dr.ID = newID()
	}
	if err := o.repo.InsertDeliveryRun(ctx, dr); err != nil {
		o.logger.Error(ctx, "failed to record delivery run",
			logging.Field{Key: "delivery_id", Value: dr.DeliveryID},
			logging.Field{Key: "trigger_id", Value: dr.TriggerID},
			logging.Field{Key: "status", Value: dr.Status},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return false
	}
	o.count(ctx, dr)
	return true
}

// derive recomputes a delivery's summary status from its per-trigger outcomes.
func (o outcomeRecorder) derive(ctx context.Context, deliveryID string) {
	if err := o.repo.DeriveDeliveryStatus(ctx, deliveryID); err != nil {
		o.logger.Error(ctx, "failed to derive delivery status",
			logging.Field{Key: "delivery_id", Value: deliveryID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
