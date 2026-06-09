package inferencegw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
)

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

// NewLiteLLMPricingSource returns a LiteLLMPricingSource using the given URL.
func NewLiteLLMPricingSource(sourceURL string) *LiteLLMPricingSource {
	return &LiteLLMPricingSource{
		url:    sourceURL,
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
	"bedrock_converse":           ProviderBedrock,
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

// bedrockRegionPrefixes are the cross-region inference profile prefixes that
// Bedrock prepends to a base model ID (e.g. "us.anthropic.claude-…" → "anthropic.claude-…").
// This list must stay in sync with providers/bedrock/models.go:knownRegionPrefixes.
var bedrockRegionPrefixes = []string{
	"apac.", "au.", "global.",
	"us.", "eu.", "ap.", "jp.",
}

// normalisePricingModelID strips any Bedrock cross-region prefix and any
// trailing version suffix from modelID so it matches the base model ID stored
// in the pricing catalog (sourced from LiteLLM bare keys like
// "anthropic.claude-…" or "qwen.qwen3-coder-480b-a35b").
//
// This intentionally does NOT strip "-instruct": the DB stores bare aliases
// with the "-instruct" suffix intact (e.g. "meta.llama3-8b-instruct"), and
// stripping it at lookup time would produce a miss.
// For non-Bedrock providers the ID is returned unchanged.
func normalisePricingModelID(provider ProviderName, modelID string) string {
	if provider != ProviderBedrock {
		return modelID
	}
	for _, pfx := range bedrockRegionPrefixes {
		if strings.HasPrefix(modelID, pfx) {
			modelID = modelID[len(pfx):]
			break
		}
	}
	return bedrockLookupSuffix.ReplaceAllString(modelID, "")
}

// bedrockLookupSuffix strips version/date suffixes from a **discovered** Bedrock
// model ID at pricing-lookup time. Intentionally excludes "-instruct" because
// that suffix is part of the model's identity in the pricing catalog for
// instruct-only models (e.g. "meta.llama3-8b-instruct").
//
//	"anthropic.claude-haiku-4-5-20251001-v1:0"  → "anthropic.claude-haiku-4-5"
//	"anthropic.claude-opus-4-6-v1"              → "anthropic.claude-opus-4-6"
//	"anthropic.claude-haiku-4-5@20251001"        → "anthropic.claude-haiku-4-5"
//	"openai.gpt-oss-120b-1:0"                    → "openai.gpt-oss-120b"
//	"meta.llama3-3-70b-instruct-v1:0"            → "meta.llama3-3-70b-instruct" (preserved)
//	"qwen.qwen3-coder-480b-a35b-instruct"        → unchanged (caller falls back to stripping -instruct)
var bedrockLookupSuffix = regexp.MustCompile(`(-\d{8})?(-v\d+(?::\d+)?|@\d{8}|-\d+:\d+)$`)

// bedrockInstructSuffix matches a trailing "-instruct" used in the fallback
// pricing lookup for models like "qwen.qwen3-coder-480b-a35b-instruct" whose
// DB entry is stored without the suffix.
var bedrockInstructSuffix = regexp.MustCompile(`-instruct$`)

// bedrockVersionSuffix strips version/date/alias suffixes from a LiteLLM model
// key at **sync time** to emit a bare alias DB entry alongside the versioned key.
// Includes "-instruct" so that LiteLLM versioned keys like
// "qwen.qwen3-coder-480b-a35b-v1:0" also emit "qwen.qwen3-coder-480b-a35b"
// (matched by AWS aliases of the form "qwen.qwen3-coder-480b-a35b-instruct"
// via the lookup fallback above).
//
//	"qwen.qwen3-coder-480b-a35b-v1:0"       → alias "qwen.qwen3-coder-480b-a35b"
//	"anthropic.claude-haiku-4-5@20251001"   → alias "anthropic.claude-haiku-4-5"
var bedrockVersionSuffix = regexp.MustCompile(`(-\d{8})?(-v\d+(?::\d+)?|@\d{8}|-instruct|-\d+:\d+)$`)

// bedrockBareAlias returns the unversioned alias form of a Bedrock model ID by
// stripping any trailing date/version suffix. Returns "" when the ID is already
// bare (no suffix to strip), to avoid creating a duplicate entry.
func bedrockBareAlias(modelID string) string {
	stripped := bedrockVersionSuffix.ReplaceAllString(modelID, "")
	if stripped == modelID {
		return ""
	}
	return stripped
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
		e := ModelPriceEntry{
			Provider:            provider,
			ModelID:             modelID,
			InputCostPer1M:      entry.InputCostPerToken * perTokenToPerMillion,
			OutputCostPer1M:     entry.OutputCostPerToken * perTokenToPerMillion,
			CacheReadCostPer1M:  entry.CacheReadInputTokenCost * perTokenToPerMillion,
			CacheWriteCostPer1M: entry.CacheCreationInputTokenCost * perTokenToPerMillion,
		}
		entries = append(entries, e)

		// For Bedrock, also emit a bare alias (no date/version suffix) so that
		// unversioned model IDs returned by ListFoundationModels can be priced.
		if provider == ProviderBedrock {
			if alias := bedrockBareAlias(modelID); alias != "" {
				aliasEntry := e
				aliasEntry.ModelID = alias
				entries = append(entries, aliasEntry)
			}
		}
	}

	return entries, nil
}
