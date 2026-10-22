package inferencegw

import "context"

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
}

// StaticPricingCache is a simple in-memory PricingCache backed by a fixed set of
// rows. It is used in tests and in early development before provider price APIs
// are integrated. The Start and Refresh methods are no-ops.
type StaticPricingCache struct {
	index *pricingIndex[ModelPricing]
}

// NewStaticPricingCache constructs a StaticPricingCache from the given entries.
func NewStaticPricingCache(entries []ModelPricing) *StaticPricingCache {
	return &StaticPricingCache{index: newModelPricingIndex(entries)}
}

// NewNopPricingCache returns a PricingCache with no pricing data.
// All Price calls return (ModelPricing{}, false) and EstimatedCostUSD returns 0.
// Use this until a live pricing source is wired in.
func NewNopPricingCache() *StaticPricingCache {
	return &StaticPricingCache{index: newModelPricingIndex(nil)}
}

// newModelPricingIndex indexes ModelPricing rows for Resolve.
func newModelPricingIndex(rows []ModelPricing) *pricingIndex[ModelPricing] {
	return newPricingIndex(rows, modelPricingProvider, modelPricingModelID, hasAnyModelPrice)
}

// hasAnyModelPrice reports whether a row carries a token or non-token price.
func hasAnyModelPrice(p ModelPricing) bool {
	return !lacksTokenPricing(p) || hasNonTokenPricing(p)
}

func (c *StaticPricingCache) Start(_ context.Context) {}

func (c *StaticPricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	p, _, ok := c.index.Resolve(provider, modelID)
	return p, ok
}

func (c *StaticPricingCache) Refresh(_ context.Context) error { return nil }
