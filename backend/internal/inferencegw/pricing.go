package inferencegw

import "context"

// PricingCache is the single source of truth for model cost data.
// It periodically refreshes pricing from provider APIs or a local override
// file and makes the latest rates available to the service layer.
//
// Implementations must be safe for concurrent use.
type PricingCache interface {
	// Start launches the background refresh goroutine. It runs until ctx is
	// cancelled. Callers should invoke this once at service startup and pass
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

func (c *StaticPricingCache) Start(_ context.Context) {}

func (c *StaticPricingCache) Price(provider ProviderName, modelID string) (ModelPricing, bool) {
	p, ok := c.entries[string(provider)+"/"+modelID]
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

// DefaultPricing returns a StaticPricingCache seeded with conservative
// published list prices for MVP-supported models.
// Prices are USD per 1,000,000 tokens as of 2025-07 and will be superseded
// when a live PricingCache implementation is added.
func DefaultPricing() *StaticPricingCache {
	return NewStaticPricingCache([]ModelPricing{
		// ── AWS Bedrock ───────────────────────────────────────────────
		{
			Provider:             ProviderBedrock,
			ModelID:              "amazon.titan-text-premier-v1:0",
			InputCostPerMillion:  0.50,
			OutputCostPerMillion: 1.50,
		},
		{
			Provider:             ProviderBedrock,
			ModelID:              "anthropic.claude-3-5-sonnet-20241022-v2:0",
			InputCostPerMillion:  3.00,
			OutputCostPerMillion: 15.00,
		},
		{
			Provider:             ProviderBedrock,
			ModelID:              "anthropic.claude-3-haiku-20240307-v1:0",
			InputCostPerMillion:  0.25,
			OutputCostPerMillion: 1.25,
		},
		{
			Provider:             ProviderBedrock,
			ModelID:              "meta.llama3-2-90b-instruct-v1:0",
			InputCostPerMillion:  2.00,
			OutputCostPerMillion: 2.00,
		},
		// ── Google Vertex AI ─────────────────────────────────────────
		{
			Provider:             ProviderVertexAI,
			ModelID:              "gemini-1.5-pro",
			InputCostPerMillion:  3.50,
			OutputCostPerMillion: 10.50,
		},
		{
			Provider:             ProviderVertexAI,
			ModelID:              "gemini-1.5-flash",
			InputCostPerMillion:  0.075,
			OutputCostPerMillion: 0.30,
		},
		// ── Azure AI Inference ───────────────────────────────────────
		{
			Provider:             ProviderAzure,
			ModelID:              "gpt-4o",
			InputCostPerMillion:  2.50,
			OutputCostPerMillion: 10.00,
		},
		{
			Provider:             ProviderAzure,
			ModelID:              "gpt-4o-mini",
			InputCostPerMillion:  0.15,
			OutputCostPerMillion: 0.60,
		},
	})
}
