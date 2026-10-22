package inferencegw

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// pricingSnapshot is the immutable in-memory projection of model_pricing.
// It is rebuilt on every refresh and swapped atomically.
type pricingSnapshot struct {
	index *pricingIndex[ModelPricing]
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
	// warned holds the provider/model pairs already logged as priced by a
	// fallback row, so a hot route logs once rather than once per request.
	warned sync.Map
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
	c.snap.Store(&pricingSnapshot{index: newModelPricingIndex(nil)})
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
// A Bedrock cross-region profile ID (e.g. "us.anthropic.claude-…") is priced by
// its own row. Without one it falls back to the model's base row, which AWS
// prices below the regional profile, or to another region's row. Either way the
// price may differ from what AWS bills, so the route is logged once.
func (c *DatabasePricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	p, match, ok := c.snap.Load().index.Resolve(provider, modelID)
	if ok && match != matchExact && (match == matchRegion || hasRegionPrefix(provider, modelID)) {
		c.warnFallbackPrice(provider, modelID, match)
	}
	return p, ok
}

// warnFallbackPrice logs, once per provider and model ID, that a route has no
// pricing row of its own and is priced by a fallback row.
func (c *DatabasePricingCache) warnFallbackPrice(provider ProviderName, modelID string, match priceMatch) {
	if _, seen := c.warned.LoadOrStore(string(provider)+"/"+modelID, struct{}{}); seen {
		return
	}
	// Price has no request context: this is cache state, not a request's log.
	c.logger.Warn(context.Background(), "pricing.route_priced_by_fallback",
		logging.Field{Key: "provider", Value: string(provider)},
		logging.Field{Key: "model_id", Value: modelID},
		logging.Field{Key: "match", Value: match.String()},
	)
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

	c.snap.Store(&pricingSnapshot{index: newModelPricingIndex(priced)})
	return nil
}
