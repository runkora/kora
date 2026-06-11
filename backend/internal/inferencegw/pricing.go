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

	// All returns all known model pricing entries. Used by GET /v1/models.
	All() []ModelPricing
}

// StaticPricingCache is a simple in-memory PricingCache backed by a fixed map.
// It is used in tests and in early development before provider price APIs are
// integrated. The Start and Refresh methods are no-ops.
type StaticPricingCache struct {
	entries map[string]ModelPricing // key: "<provider>/<modelID>"
}

// NewStaticPricingCache constructs a StaticPricingCache from the given entries.
func NewStaticPricingCache(entries []ModelPricing) *StaticPricingCache {
	m := make(map[string]ModelPricing, len(entries))
	for _, e := range entries {
		m[string(e.Provider)+"/"+e.ModelID] = e
	}
	return &StaticPricingCache{entries: m}
}

// NewNopPricingCache returns a PricingCache with no pricing data.
// All Price calls return (ModelPricing{}, false) and EstimatedCostUSD returns 0.
// Use this until a live pricing source is wired in.
func NewNopPricingCache() *StaticPricingCache {
	return &StaticPricingCache{entries: map[string]ModelPricing{}}
}

func (c *StaticPricingCache) Start(_ context.Context) {}

func (c *StaticPricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	p, ok := c.entries[string(provider)+"/"+normalisePricingModelID(provider, modelID)]
	return p, ok
}

func (c *StaticPricingCache) Refresh(_ context.Context) error { return nil }

func (c *StaticPricingCache) All() []ModelPricing {
	result := make([]ModelPricing, 0, len(c.entries))
	for _, v := range c.entries {
		result = append(result, v)
	}
	return result
}
