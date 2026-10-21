package inferencegw

import (
	"context"
	"slices"
)

// PricingCache is the single source of truth for model cost data.
// It periodically refreshes pricing from provider APIs or a local override
// file and makes the latest rates available to the service layer.
//
// Implementations must be safe for concurrent use.
type PricingCache interface {
	// Start launches the background refresh goroutine. It runs until ctx is
	// canceled. Callers should invoke this once at service startup and pass
	// the service-scoped context so shutdown is coordinated automatically.
	Start(ctx context.Context)

	// Price returns pricing for a provider+model pair. Returns false when no
	// pricing data is available (provider disabled, model unknown, cache cold).
	Price(provider ProviderName, modelID string) (ModelPricing, bool)

	// Refresh triggers an immediate out-of-band refresh of the cache.
	// It is safe to call concurrently with Price.
	Refresh(ctx context.Context) error

	// All returns all known model pricing entries. Used by GET /v1/models.
	All() []ModelPricing
}

// StaticPricingCache is a simple in-memory PricingCache backed by a fixed map.
// It is used in tests and in early development before provider price APIs are
// integrated. The Start and Refresh methods are no-ops.
type StaticPricingCache struct {
	entries map[string]ModelPricing // key: pricingExactKey, plus pricingKey as fallback
	rows    []ModelPricing
	bare    map[string]ModelPricing // key: pricingBareKey (cross-provider fallback)
}

// NewStaticPricingCache constructs a StaticPricingCache from the given entries.
func NewStaticPricingCache(entries []ModelPricing) *StaticPricingCache {
	return &StaticPricingCache{entries: buildPricingIndex(entries), rows: entries, bare: bareFallbackIndex(entries, modelPricingProvider, modelPricingModelID)}
}

// NewNopPricingCache returns a PricingCache with no pricing data.
// All Price calls return (ModelPricing{}, false) and EstimatedCostUSD returns 0.
// Use this until a live pricing source is wired in.
func NewNopPricingCache() *StaticPricingCache {
	return &StaticPricingCache{entries: map[string]ModelPricing{}, bare: map[string]ModelPricing{}}
}

func (c *StaticPricingCache) Start(_ context.Context) {}

func (c *StaticPricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	if p, ok := lookupPricingIndex(c.entries, provider, modelID); ok {
		return p, true
	}
	// Best-effort: no same-provider row, so borrow the same model's price from
	// another provider (e.g. gpt-5.x on Bedrock, priced only under Azure). Keeps
	// usage cost attribution non-zero for models the catalog prices elsewhere.
	p, ok := c.bare[pricingBareKey(provider, modelID)]
	return p, ok
}

func (c *StaticPricingCache) Refresh(_ context.Context) error { return nil }

func (c *StaticPricingCache) All() []ModelPricing {
	return slices.Clone(c.rows)
}
