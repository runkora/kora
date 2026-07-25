package inferencegw

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// pricingSnapshot is the immutable in-memory projection of model_pricing.
// It is rebuilt on every refresh and swapped atomically.
type pricingSnapshot struct {
	entries map[string]ModelPricing // key: pricingKey
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
// For Bedrock models, cross-region profile IDs (e.g. "us.anthropic.claude-…")
// are normalised to their base form (e.g. "anthropic.claude-…") before the
// lookup because the pricing catalog is keyed on the base ID from LiteLLM.
func (c *DatabasePricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	snap := c.snap.Load()
	if p, ok := snap.entries[pricingKey(provider, modelID)]; ok {
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

	entries := make(map[string]ModelPricing, len(catalog))
	priced := make([]ModelPricing, 0, len(catalog))
	for _, e := range catalog {
		mp := ModelPricing{
			Provider:                 e.Provider,
			ModelID:                  e.ModelID,
			InputCostPerMillion:      e.EffectiveInput(),
			OutputCostPerMillion:     e.EffectiveOutput(),
			CacheReadCostPerMillion:  e.EffectiveCacheRead(),
			CacheWriteCostPerMillion: e.EffectiveCacheWrite(),
		}
		entries[pricingKey(e.Provider, e.ModelID)] = mp
		priced = append(priced, mp)
	}

	bare := bareFallbackIndex(priced, modelPricingProvider, modelPricingModelID)
	c.snap.Store(&pricingSnapshot{entries: entries, bare: bare})
	return nil
}

// All returns all known model pricing entries.
func (c *DatabasePricingCache) All() []ModelPricing {
	snap := c.snap.Load()
	result := make([]ModelPricing, 0, len(snap.entries))
	for _, v := range snap.entries {
		result = append(result, v)
	}
	return result
}
