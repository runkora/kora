package bedrock

import (
	"regexp"
	"strings"
)

// backendID identifies the calling convention to use for a Bedrock model.
type backendID uint8

const (
	backendOpenAICompat   backendID = iota // Bedrock /openai/v1 endpoint (Titan, Nova, Llama, Mistral, …)
	backendAnthropic                       // Anthropic Messages API on Bedrock (Claude models)
	backendMantle                          // Bedrock Mantle endpoint /v1 — OpenAI-hosted models (chat/completions + responses)
	backendMantleOpenAIV1                  // Bedrock Mantle endpoint /openai/v1 — every openai.gpt-* model except gpt-oss, plus xAI Grok (responses only)
	backendTitanEmbed                      // native InvokeModel, Amazon Titan embed format
	backendCohereEmbed                     // native InvokeModel, Cohere embed format
)

// isMantleOpenAIV1Model reports whether modelID is served on Bedrock Mantle's
// /openai/v1 path. That path serves every openai.gpt-* model except gpt-oss,
// plus xAI Grok, and supports only the Responses API. Cross-region prefixes
// are stripped before the check.
func isMantleOpenAIV1Model(modelID string) bool {
	base := stripRegionPrefix(modelID)
	if strings.HasPrefix(base, "xai.grok-") {
		return true
	}
	return strings.HasPrefix(base, "openai.gpt-") && !strings.HasPrefix(base, "openai.gpt-oss")
}

// mantleVersionSuffix matches the "-1:0" version suffix of foundation model IDs.
var mantleVersionSuffix = regexp.MustCompile(`-\d+:\d+$`)

// mantleModelID returns the bare ID Mantle lists for modelID. It strips the
// cross-region prefix and the foundation model version suffix, so
// "us.openai.gpt-6-sol" becomes "openai.gpt-6-sol" and "openai.gpt-oss-20b-1:0"
// becomes "openai.gpt-oss-20b". Non-openai IDs only lose the region prefix.
func mantleModelID(modelID string) string {
	base := stripRegionPrefix(modelID)
	if strings.HasPrefix(base, "openai.") {
		return mantleVersionSuffix.ReplaceAllString(base, "")
	}
	return base
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
	// GPT and xAI Grok live on Mantle but use a distinct /openai/v1 path
	// that only supports the Responses API — check before the generic openai. prefix.
	if isMantleOpenAIV1Model(base) {
		return backendMantleOpenAIV1
	}
	// All remaining openai.* IDs (e.g. openai.gpt-oss-120b) are standard
	// Mantle models that support /v1/chat/completions and /v1/responses.
	if strings.HasPrefix(base, "openai.") {
		return backendMantle
	}
	return backendOpenAICompat
}
