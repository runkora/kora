package inferencegw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
)

const liteLLMPricesURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// ModelPriceEntry is a provider/model price tuple produced by a PricingSource.
// All cost values are USD per 1,000,000 tokens. Fields are 0 when not provided
// by the source — never negative.
type ModelPriceEntry struct {
	Provider            ProviderName
	ModelID             string
	InputCostPer1M      float64
	OutputCostPer1M     float64
	CacheReadCostPer1M  float64
	CacheWriteCostPer1M float64
}

// PricingSource abstracts the upstream price catalog. The interface does not
// expose any source-specific field names so the implementation can be swapped
// without changing the sync worker or the pricing cache.
type PricingSource interface {
	Fetch(ctx context.Context) ([]ModelPriceEntry, error)
}

// LiteLLMPricingSource fetches pricing from LiteLLM's community-maintained
// model_prices_and_context_window.json file. It converts per-token costs to
// per-million-token costs and maps LiteLLM provider names to Orbit ProviderNames.
type LiteLLMPricingSource struct {
	url    string
	client *http.Client
}

// NewLiteLLMPricingSource returns a LiteLLMPricingSource using the canonical
// LiteLLM GitHub URL.
func NewLiteLLMPricingSource() *LiteLLMPricingSource {
	return &LiteLLMPricingSource{
		url:    liteLLMPricesURL,
		client: httpclient.NewHTTPClient(30*time.Second, httpclient.NewTracingTransport("orbit.litellm-pricing", nil)),
	}
}

// liteLLMEntry is the subset of fields we parse from each model entry in the
// LiteLLM JSON. Field names match the JSON exactly.
type liteLLMEntry struct {
	LiteLLMProvider             string  `json:"litellm_provider"`
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
}

// liteLLMProviderToOrbit maps LiteLLM litellm_provider field values to Orbit ProviderNames.
var liteLLMProviderToOrbit = map[string]ProviderName{
	"bedrock":                    ProviderBedrock,
	"vertex_ai":                  ProviderVertexAI,
	"vertex_ai-text-models":      ProviderVertexAI,
	"vertex_ai-chat-models":      ProviderVertexAI,
	"vertex_ai-code-text-models": ProviderVertexAI,
	"vertex_ai-language-models":  ProviderVertexAI,
	"azure":                      ProviderAzure,
	"azure_ai":                   ProviderAzure,
}

// resolveProvider maps a LiteLLM provider field value to an Orbit ProviderName.
// Returns ("", false) for providers not supported by Orbit.
func resolveProvider(liteLLMProvider string) (ProviderName, bool) {
	p, ok := liteLLMProviderToOrbit[liteLLMProvider]
	return p, ok
}

// stripProviderPrefix removes a leading "provider/" prefix from a LiteLLM model key,
// returning the bare model ID that matches what Orbit stores in virtual_model_routes.
func stripProviderPrefix(key string) string {
	prefixes := []string{
		"bedrock/",
		"vertex_ai/",
		"vertex_ai-text-models/",
		"vertex_ai-chat-models/",
		"vertex_ai-code-text-models/",
		"vertex_ai-language-models/",
		"azure/",
		"azure_ai/",
	}
	for _, pfx := range prefixes {
		if trimmed, ok := strings.CutPrefix(key, pfx); ok {
			return trimmed
		}
	}
	return key
}

// Fetch retrieves the LiteLLM price file and returns entries for Orbit's
// supported providers (bedrock, vertexai, azure) only.
func (s *LiteLLMPricingSource) Fetch(ctx context.Context) ([]ModelPriceEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("litellm pricing source build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("litellm pricing source fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("litellm pricing source: unexpected status %d", resp.StatusCode)
	}

	// The file is a top-level object where each key is a model name. We decode
	// it as a map so we preserve key order and can inspect the key prefix.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("litellm pricing source decode: %w", err)
	}

	entries := make([]ModelPriceEntry, 0, len(raw))
	for key, v := range raw {
		var entry liteLLMEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			continue
		}

		provider, ok := resolveProvider(entry.LiteLLMProvider)
		if !ok {
			continue
		}

		modelID := stripProviderPrefix(key)
		if modelID == "" {
			continue
		}

		const perTokenToPerMillion = 1_000_000
		entries = append(entries, ModelPriceEntry{
			Provider:            provider,
			ModelID:             modelID,
			InputCostPer1M:      entry.InputCostPerToken * perTokenToPerMillion,
			OutputCostPer1M:     entry.OutputCostPerToken * perTokenToPerMillion,
			CacheReadCostPer1M:  entry.CacheReadInputTokenCost * perTokenToPerMillion,
			CacheWriteCostPer1M: entry.CacheCreationInputTokenCost * perTokenToPerMillion,
		})
	}

	return entries, nil
}
