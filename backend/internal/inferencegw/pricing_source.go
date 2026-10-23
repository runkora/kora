package inferencegw

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
)

// ModelPriceEntry is a provider/model price tuple produced by a PricingSource.
// It is the intermediate representation between the raw upstream price file and
// the DB — written by LiteLLMPricingSource.Fetch() and consumed by
// PostgresPricingCatalogRepository.UpsertSynced(). It is never stored long-term
// or served to API clients.
//
// Naming convention: token and audio costs use the Per1M suffix (USD per
// 1,000,000 units). Per-unit non-token costs (image, request, second) do not
// carry a scaled suffix — their comments state the unit explicitly.
//
// Do not confuse with:
//   - PricingCatalogEntry (pricing_catalog.go) — the DB row struct, which adds
//     override columns and is used by the PricingCache.
//   - ModelPricing (connector.go) — the runtime in-memory snapshot served on
//     every inference request.
type ModelPriceEntry struct {
	Provider            ProviderName
	ModelID             string
	InputCostPer1M      float64
	OutputCostPer1M     float64
	CacheReadCostPer1M  float64
	CacheWriteCostPer1M float64
	// Non-token dimensions — 0 when not present in the source:
	ImageInputCost       float64 // USD per image input (per-unit, not scaled)
	ImageOutputCost      float64 // USD per image output (per-unit, not scaled)
	RequestInputCost     float64 // USD flat per-request input (per-unit, not scaled)
	RequestOutputCost    float64 // USD flat per-request output (per-unit, not scaled)
	SecondInputCost      float64 // USD per-second input, e.g. audio/video (per-unit, not scaled)
	SecondOutputCost     float64 // USD per-second output (per-unit, not scaled)
	AudioInputCostPer1M  float64 // USD per 1M audio tokens input (source value ×1,000,000)
	AudioOutputCostPer1M float64 // USD per 1M audio tokens output (source value ×1,000,000)
}

// PricingSource abstracts the upstream price catalog. The interface does not
// expose any source-specific field names so the implementation can be swapped
// without changing the sync worker or the pricing cache.
type PricingSource interface {
	Fetch(ctx context.Context) ([]ModelPriceEntry, error)
}

// LiteLLMPricingSource fetches pricing from LiteLLM's community-maintained
// model_prices_and_context_window.json file. It converts per-token costs to
// per-million-token costs and maps LiteLLM provider names to kora ProviderNames.
type LiteLLMPricingSource struct {
	url    string
	client *http.Client
}

// NewLiteLLMPricingSource returns a LiteLLMPricingSource using the given URL.
func NewLiteLLMPricingSource(sourceURL string) *LiteLLMPricingSource {
	return &LiteLLMPricingSource{
		url:    sourceURL,
		client: httpclient.NewHTTPClient(30*time.Second, httpclient.NewTracingTransport("kora.litellm-pricing", nil)),
	}
}

// liteLLMEntry is the subset of fields we parse from each model entry in the
// LiteLLM JSON. Field names match the JSON exactly.
type liteLLMEntry struct {
	LiteLLMProvider             string   `json:"litellm_provider"`
	InputCostPerToken           float64  `json:"input_cost_per_token"`
	OutputCostPerToken          float64  `json:"output_cost_per_token"`
	CacheReadInputTokenCost     float64  `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64  `json:"cache_creation_input_token_cost"`
	InputCostPerImage           *float64 `json:"input_cost_per_image"`
	OutputCostPerImage          *float64 `json:"output_cost_per_image"`
	InputCostPerRequest         *float64 `json:"input_cost_per_request"`
	OutputCostPerRequest        *float64 `json:"output_cost_per_request"`
	InputCostPerSecond          *float64 `json:"input_cost_per_second"`
	OutputCostPerSecond         *float64 `json:"output_cost_per_second"`
	InputCostPerAudioToken      *float64 `json:"input_cost_per_audio_token"`
	OutputCostPerAudioToken     *float64 `json:"output_cost_per_audio_token"`
}

// liteLLMProviderToKora maps LiteLLM litellm_provider field values to kora ProviderNames.
var liteLLMProviderToKora = map[string]ProviderName{
	"bedrock":                    ProviderBedrock,
	"bedrock_converse":           ProviderBedrock,
	"bedrock_mantle":             ProviderBedrock,
	"vertex_ai":                  ProviderVertexAI,
	"vertex_ai-text-models":      ProviderVertexAI,
	"vertex_ai-chat-models":      ProviderVertexAI,
	"vertex_ai-code-text-models": ProviderVertexAI,
	"vertex_ai-language-models":  ProviderVertexAI,
	"azure":                      ProviderAzure,
	"azure_ai":                   ProviderAzure,
}

// resolveProvider maps a LiteLLM provider field value to a kora ProviderName.
// Returns ("", false) for providers not supported by kora.
func resolveProvider(liteLLMProvider string) (ProviderName, bool) {
	p, ok := liteLLMProviderToKora[liteLLMProvider]
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
	return stripPricingVersion(provider, stripPricingRegion(provider, modelID))
}

// stripPricingRegion removes a Bedrock cross-region prefix and nothing else.
// Other providers' IDs are returned unchanged.
func stripPricingRegion(provider ProviderName, modelID string) string {
	if !provider.IsBedrockFamily() {
		return modelID
	}
	for _, pfx := range bedrockRegionPrefixes {
		if strings.HasPrefix(modelID, pfx) {
			return modelID[len(pfx):]
		}
	}
	return modelID
}

// stripPricingVersion removes a trailing Bedrock version suffix and keeps the
// region prefix, so "us.anthropic.claude-haiku-4-5-v1:0" becomes
// "us.anthropic.claude-haiku-4-5". It names the pricing row for the inference
// profile a route calls. Other providers' IDs are returned unchanged.
func stripPricingVersion(provider ProviderName, modelID string) string {
	if !provider.IsBedrockFamily() {
		return modelID
	}
	return bedrockLookupSuffix.ReplaceAllString(modelID, "")
}

// normalisePricingProvider returns the provider key of a pricing row. Bedrock,
// Vertex AI and Azure prices sync per type and are shared by every resource of
// that type. A compatible provider is priced on its own: two compatible
// endpoints serve unrelated catalogs.
func normalisePricingProvider(provider ProviderName) ProviderName {
	if provider.Type() == ProviderCompatible {
		return provider
	}
	return provider.Type()
}

// pricingKey is the canonical key for the pricing catalog: the normalised
// provider and the normalised model ID. Every writer and reader of a pricing
// map must build its key through this function, otherwise entries written under
// the base provider are unreachable from an aliased one.
func pricingKey(provider ProviderName, modelID string) string {
	return string(normalisePricingProvider(provider)) + "/" + normalisePricingModelID(provider, modelID)
}

// pricingExactKey is the key of the pricing row for the exact inference profile
// a route calls: the normalised provider and the model ID with its region
// prefix kept. Regional and global variants of one model have distinct keys.
func pricingExactKey(provider ProviderName, modelID string) string {
	return string(normalisePricingProvider(provider)) + "/" + stripPricingVersion(provider, modelID)
}

// pricingBareKey returns a provider-agnostic key for a best-effort,
// cross-provider pricing fallback: the normalised model ID with its Bedrock
// vendor namespace ("openai." in "openai.gpt-5.4") and any leading region
// segment ("us/gpt-5.4") removed, leaving the bare model name ("gpt-5.4").
//
// This lets a model served on a provider the pricing catalog does not price
// borrow the price of the same model on another provider — e.g. gpt-5.x runs on
// Bedrock (Mantle) but LiteLLM prices it only under Azure. It is deliberately
// approximate and used ONLY when no exact (provider, model) row exists.
func pricingBareKey(provider ProviderName, modelID string) string {
	id := normalisePricingModelID(provider, modelID)
	// Bedrock IDs are vendor-namespaced ("openai.gpt-5.4", "anthropic.claude-…").
	// Other providers use the bare name, so only strip the namespace for Bedrock
	// (stripping the first "." unconditionally would mangle "gpt-5.4" -> "4").
	if provider.IsBedrockFamily() {
		if i := strings.IndexByte(id, '.'); i > 0 {
			id = id[i+1:]
		}
	}
	// Some providers prefix a region segment ("us/gpt-5.4", "eu/gpt-5.4").
	if i := strings.IndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// modelPricingProvider and modelPricingModelID are field accessors for
// newPricingIndex over ModelPricing rows.
func modelPricingProvider(p ModelPricing) ProviderName { return p.Provider }
func modelPricingModelID(p ModelPricing) string        { return p.ModelID }

// barePricingRank orders candidates for the same bare model name. Lower wins:
// the plainest (shortest) model ID first, then (provider, modelID) lexically as
// a total-order tiebreak so the choice is deterministic regardless of the input
// (or map) iteration order — identical requests must always resolve the same
// price.
func barePricingRank(provider ProviderName, modelID string) (int, string, string) {
	return len(modelID), string(provider), modelID
}

func barePricingLess(aLen int, aProv, aID string, bLen int, bProv, bID string) bool {
	if aLen != bLen {
		return aLen < bLen
	}
	if aProv != bProv {
		return aProv < bProv
	}
	return aID < bID
}

// bareFallbackIndex indexes pricing rows by pricingBareKey for the cross-provider
// fallback, keeping the plainest model ID per bare name (barePricingRank) so a
// bare "gpt-5.4" is preferred over a dated or regional variant. provOf and idOf
// extract the provider and model ID from each row so it works for any row type.
func bareFallbackIndex[T any](rows []T, provOf func(T) ProviderName, idOf func(T) string) map[string]T {
	idx := make(map[string]T, len(rows))
	type rank struct {
		length int
		prov   string
		id     string
	}
	best := make(map[string]rank, len(rows))
	for _, r := range rows {
		prov, id := provOf(r), idOf(r)
		bk := pricingBareKey(prov, id)
		if bk == "" {
			continue
		}
		l, pv, i := barePricingRank(prov, id)
		if cur, ok := best[bk]; !ok || barePricingLess(l, pv, i, cur.length, cur.prov, cur.id) {
			idx[bk] = r
			best[bk] = rank{l, pv, i}
		}
	}
	return idx
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
// returning the bare model ID that matches what kora stores in virtual_model_routes.
func stripProviderPrefix(key string) string {
	prefixes := []string{
		"bedrock/",
		"bedrock_mantle/",
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

// derefFloat64 dereferences a *float64, returning 0 for nil.
func derefFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// applyNonTokenCosts copies non-token cost fields from a liteLLMEntry into e.
// Audio token costs are scaled ×1,000,000 to per-1M to match the token cost convention.
func applyNonTokenCosts(e *ModelPriceEntry, src liteLLMEntry) {
	const scale = 1_000_000
	e.ImageInputCost = derefFloat64(src.InputCostPerImage)
	e.ImageOutputCost = derefFloat64(src.OutputCostPerImage)
	e.RequestInputCost = derefFloat64(src.InputCostPerRequest)
	e.RequestOutputCost = derefFloat64(src.OutputCostPerRequest)
	e.SecondInputCost = derefFloat64(src.InputCostPerSecond)
	e.SecondOutputCost = derefFloat64(src.OutputCostPerSecond)
	e.AudioInputCostPer1M = derefFloat64(src.InputCostPerAudioToken) * scale
	e.AudioOutputCostPer1M = derefFloat64(src.OutputCostPerAudioToken) * scale
}

// Fetch retrieves the LiteLLM price file and returns entries for kora's
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

	const perTokenToPerMillion = 1_000_000
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

		e := ModelPriceEntry{
			Provider:            provider,
			ModelID:             modelID,
			InputCostPer1M:      entry.InputCostPerToken * perTokenToPerMillion,
			OutputCostPer1M:     entry.OutputCostPerToken * perTokenToPerMillion,
			CacheReadCostPer1M:  entry.CacheReadInputTokenCost * perTokenToPerMillion,
			CacheWriteCostPer1M: entry.CacheCreationInputTokenCost * perTokenToPerMillion,
		}
		applyNonTokenCosts(&e, entry)
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
