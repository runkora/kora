package bedrock

import "strings"

// backendID identifies the calling convention to use for a Bedrock model.
type backendID uint8

const (
	backendOpenAICompat backendID = iota // Bedrock /openai/v1 endpoint (Titan, Nova, Llama, Mistral, …)
	backendAnthropic                     // Anthropic Messages API on Bedrock (Claude models)
	backendMantle                        // Bedrock Mantle endpoint /v1 — OpenAI-hosted models (chat/completions + responses)
	backendMantleGPT5                    // Bedrock Mantle endpoint /openai/v1 — proprietary GPT-5.x family (responses only)
	backendTitanEmbed                    // native InvokeModel, Amazon Titan embed format
	backendCohereEmbed                   // native InvokeModel, Cohere embed format
)

// gpt5ModelIDs is the exhaustive set of OpenAI GPT-5.x model IDs served by
// Bedrock Mantle on the /openai/v1 path.  AWS documentation states these
// models use a path different from the /v1 path used by all other Mantle
// models.  They support only the Responses API — Invoke, Converse, and Chat
// Completions all return a 400.
//
// Source: AWS Bedrock model cards (confirmed live against us-east-1 2026-07-23).
var gpt5ModelIDs = map[string]bool{
	"openai.gpt-5.4":            true,
	"openai.gpt-5.4-2026-03-05": true,
	"openai.gpt-5.5":            true,
	"openai.gpt-5.5-2026-04-23": true,
	"openai.gpt-5.6-luna":       true,
	"openai.gpt-5.6-sol":        true,
	"openai.gpt-5.6-terra":      true,
}

// isGPT5Model reports whether modelID belongs to the GPT-5.x family that AWS
// serves on the Mantle /openai/v1 path rather than the standard /v1 path.
// Cross-region prefixes are stripped before the lookup because Mantle models
// have no inference-profile form, but we strip defensively to be future-safe.
func isGPT5Model(modelID string) bool {
	return gpt5ModelIDs[stripRegionPrefix(modelID)]
}

// dispatchBackend returns the backend to use for modelID.
// It reads the registry populated by ListModels; on cold start (before the
// first ListModels sweep completes) it falls back to prefix heuristics.
func (c *Connector) dispatchBackend(modelID string) backendID {
	if p := c.modelRegistry.Load(); p != nil {
		if id, ok := (*p)[modelID]; ok {
			return id
		}
	}
	return inferBackendID(modelID)
}

// inferBackendID derives a backendID from model ID prefix patterns.
// It is the cold-start fallback for dispatchBackend and the source of truth
// used when populating the registry in ListModels.
func inferBackendID(modelID string) backendID {
	if isAnthropicModel(modelID) {
		return backendAnthropic
	}
	base := stripRegionPrefix(modelID)
	if strings.HasPrefix(base, "amazon.titan-embed") || strings.HasPrefix(base, "amazon.titan-text-embed") {
		return backendTitanEmbed
	}
	if strings.HasPrefix(base, "cohere.embed") {
		return backendCohereEmbed
	}
	// GPT-5.x lives on Mantle but uses a distinct /openai/v1 path that only
	// supports the Responses API — check before the generic openai. prefix.
	if isGPT5Model(base) {
		return backendMantleGPT5
	}
	// All remaining openai.* IDs (e.g. openai.gpt-oss-120b) are standard
	// Mantle models that support /v1/chat/completions and /v1/responses.
	if strings.HasPrefix(base, "openai.") {
		return backendMantle
	}
	return backendOpenAICompat
}
