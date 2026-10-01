package inferencegw

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// syncedPriceWriter stores synced prices. Override columns are left as they are.
type syncedPriceWriter interface {
	UpsertSynced(ctx context.Context, entries []ModelPriceEntry) error
}

// pricingRefresher reloads the in-memory price cache.
type pricingRefresher interface {
	Refresh(ctx context.Context) error
}

// DiscoveredPrices stores the prices a provider publishes with its model list,
// such as OpenRouter's, as that provider's synced prices.
type DiscoveredPrices struct {
	store  syncedPriceWriter
	cache  pricingRefresher
	logger logging.Logger
}

// NewDiscoveredPrices builds a recorder for discovered prices.
func NewDiscoveredPrices(store syncedPriceWriter, cache pricingRefresher, logger logging.Logger) *DiscoveredPrices {
	return &DiscoveredPrices{store: store, cache: cache, logger: logger}
}

// Record upserts the published prices of models and reloads the cache. Each
// row is keyed by the provider's pricing key, so a compatible provider's own
// prices win over a price borrowed from another provider.
func (d *DiscoveredPrices) Record(ctx context.Context, models []ProviderModel) {
	var entries []ModelPriceEntry
	for _, m := range models {
		if m.Price == nil {
			continue
		}
		e := *m.Price
		e.Provider = normalisePricingProvider(m.Provider)
		e.ModelID = normalisePricingModelID(m.Provider, m.ModelID)
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return
	}
	if err := d.store.UpsertSynced(ctx, entries); err != nil {
		d.logger.Warn(ctx, "discovered prices not stored", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	if err := d.cache.Refresh(ctx); err != nil {
		d.logger.Warn(ctx, "price cache not refreshed", logging.Field{Key: "error", Value: err.Error()})
	}
	d.logger.Info(ctx, "discovered prices stored", logging.Field{Key: "models", Value: len(entries)})
}
