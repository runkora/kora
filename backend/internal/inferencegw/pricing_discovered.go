package inferencegw

import (
	"context"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// syncedPriceReplacer replaces one provider's synced prices. Overrides are kept.
type syncedPriceReplacer interface {
	ReplaceSynced(ctx context.Context, provider ProviderName, entries []ModelPriceEntry) error
}

// pricingRefresher reloads the in-memory price cache.
type pricingRefresher interface {
	Refresh(ctx context.Context) error
}

// DiscoveredPrices stores the prices a compatible provider publishes with its
// model list, such as OpenRouter's, as that provider's synced prices.
type DiscoveredPrices struct {
	store  syncedPriceReplacer
	cache  pricingRefresher
	logger logging.Logger
}

// NewDiscoveredPrices builds a recorder for discovered prices.
func NewDiscoveredPrices(store syncedPriceReplacer, cache pricingRefresher, logger logging.Logger) *DiscoveredPrices {
	return &DiscoveredPrices{store: store, cache: cache, logger: logger}
}

// Record makes the published prices in models the provider's synced prices,
// dropping prices for models it no longer lists or prices. Other types sync from LiteLLM.
func (d *DiscoveredPrices) Record(ctx context.Context, provider ProviderName, models []ProviderModel) {
	if provider.Type() != ProviderCompatible {
		return
	}
	entries := make([]ModelPriceEntry, 0, len(models))
	for _, m := range models {
		if m.Price == nil {
			continue
		}
		e := *m.Price
		e.Provider = normalisePricingProvider(provider)
		e.ModelID = normalisePricingModelID(provider, m.ModelID)
		entries = append(entries, e)
	}
	if err := d.store.ReplaceSynced(ctx, normalisePricingProvider(provider), entries); err != nil {
		d.logger.Warn(ctx, "discovered prices not stored",
			logging.Field{Key: "provider", Value: string(provider)}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	if err := d.cache.Refresh(ctx); err != nil {
		d.logger.Warn(ctx, "price cache not refreshed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	d.logger.Info(ctx, "discovered prices stored",
		logging.Field{Key: "provider", Value: string(provider)}, logging.Field{Key: "models", Value: len(entries)})
}
