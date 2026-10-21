package inferencegw

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// pricingSnapshot is the immutable in-memory projection of model_pricing.
// It is rebuilt on every refresh and swapped atomically.
type pricingSnapshot struct {
	entries map[string]ModelPricing // key: pricingExactKey, plus pricingKey as fallback
	rows    []ModelPricing          // every row, unindexed
	bare    map[string]ModelPricing // key: pricingBareKey (cross-provider fallback)
}

// DatabasePricingCache implements PricingCache backed by the model_pricing
// table. The cache is loaded at startup and refreshed in the background on a
// fixed interval. All inference request path reads are served from the in-memory
// snapshot with no database I/O.
type DatabasePricingCache struct {
	catalog  PricingCatalogRepository
	interval time.Duration
	logger   logging.Logger
	snap     atomic.Pointer[pricingSnapshot]
}

// NewDatabasePricingCache constructs a DatabasePricingCache.
// The catalog is used for initial load and background refreshes.
// interval is the background refresh period (e.g. 5 * time.Minute).
func NewDatabasePricingCache(catalog PricingCatalogRepository, interval time.Duration, logger logging.Logger) *DatabasePricingCache {
	c := &DatabasePricingCache{
		catalog:  catalog,
		interval: interval,
		logger:   logger,
	}
	// Seed with an empty snapshot so Price() never panics before Start().
	c.snap.Store(&pricingSnapshot{entries: map[string]ModelPricing{}, bare: map[string]ModelPricing{}})
	return c
}

// Start loads the cache immediately and then spawns a background goroutine that
// refreshes it on the configured interval. Runs until ctx is canceled.
func (c *DatabasePricingCache) Start(ctx context.Context) {
	if err := c.Refresh(ctx); err != nil {
		c.logger.Warn(ctx, "pricing cache initial load failed — pricing data unavailable until next refresh",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.Refresh(ctx); err != nil {
					c.logger.Warn(ctx, "pricing cache refresh failed — retaining previous prices",
						logging.Field{Key: "error", Value: err.Error()},
					)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Price returns the effective pricing for a provider+model pair.
// Returns (ModelPricing{}, false) when the model is not in the catalog.
//
// The row for the exact routed ID is used first, so a regional profile
// ("us.anthropic.claude-…") gets its own price. Otherwise the ID is normalised
// to its base form ("anthropic.claude-…") for the lookup.
func (c *DatabasePricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	snap := c.snap.Load()
	if p, ok := lookupPricingIndex(snap.entries, provider, modelID); ok {
		return p, true
	}
	// Best-effort: no same-provider row, so borrow the same model's price from
	// another provider (e.g. gpt-5.x on Bedrock, priced only under Azure). Keeps
	// usage cost attribution non-zero for models the catalog prices elsewhere.
	p, ok := snap.bare[pricingBareKey(provider, modelID)]
	return p, ok
}

// Refresh reloads the pricing catalog from the database and atomically replaces
// the in-memory snapshot. If the load fails the previous snapshot is retained.
func (c *DatabasePricingCache) Refresh(ctx context.Context) error {
	catalog, err := c.catalog.LoadAll(ctx)
	if err != nil {
		return err
	}

	priced := make([]ModelPricing, 0, len(catalog))
	for _, e := range catalog {
		mp := ModelPricing{
			Provider:                 e.Provider,
			ModelID:                  e.ModelID,
			InputCostPerMillion:      e.EffectiveInput(),
			OutputCostPerMillion:     e.EffectiveOutput(),
			CacheReadCostPerMillion:  e.EffectiveCacheRead(),
			CacheWriteCostPerMillion: e.EffectiveCacheWrite(),
			ImageInputCost:           e.SyncedImageInputCostPerImage,
			ImageOutputCost:          e.SyncedImageOutputCostPerImage,
			RequestInputCost:         e.SyncedRequestInputCost,
			RequestOutputCost:        e.SyncedRequestOutputCost,
			SecondInputCost:          e.SyncedSecondInputCost,
			SecondOutputCost:         e.SyncedSecondOutputCost,
			AudioInputCostPer1M:      e.SyncedAudioInputCostPer1M,
			AudioOutputCostPer1M:     e.SyncedAudioOutputCostPer1M,
		}
		priced = append(priced, mp)
	}

	bare := bareFallbackIndex(priced, modelPricingProvider, modelPricingModelID)
	c.snap.Store(&pricingSnapshot{entries: buildPricingIndex(priced), rows: priced, bare: bare})
	return nil
}

// All returns all known model pricing entries.
func (c *DatabasePricingCache) All() []ModelPricing {
	snap := c.snap.Load()
	return slices.Clone(snap.rows)
}
