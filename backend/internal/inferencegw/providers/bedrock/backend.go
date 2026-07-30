package bedrock

import "strings"

// backendID identifies the calling convention to use for a Bedrock model.
type backendID uint8

const (
	backendOpenAICompat   backendID = iota // Bedrock /openai/v1 endpoint (Titan, Nova, Llama, Mistral, …)
	backendAnthropic                       // Anthropic Messages API on Bedrock (Claude models)
	backendMantle                          // Bedrock Mantle endpoint /v1 — OpenAI-hosted models (chat/completions + responses)
	backendMantleOpenAIV1                  // Bedrock Mantle endpoint /openai/v1 — GPT-5.x and xAI Grok (responses only)
	backendTitanEmbed                      // native InvokeModel, Amazon Titan embed format
	backendCohereEmbed                     // native InvokeModel, Cohere embed format
)

// mantleOpenAIV1ModelIDs is the exhaustive set of model IDs served by Bedrock
// Mantle on the /openai/v1 path.  AWS documentation states these models use a
// path different from the /v1 path used by all other Mantle models.  They
// support only the Responses API — Invoke, Converse, and Chat Completions all
// return a 400.  Today this includes the OpenAI GPT-5.x family and xAI Grok.
//
// Source: AWS Bedrock model cards (confirmed live against us-east-1 2026-07-23).
var mantleOpenAIV1ModelIDs = map[string]bool{
	"openai.gpt-5.4":            true,
	"openai.gpt-5.4-2026-03-05": true,
	"openai.gpt-5.5":            true,
	"openai.gpt-5.5-2026-04-23": true,
	"openai.gpt-5.6-luna":       true,
	"openai.gpt-5.6-sol":        true,
	"openai.gpt-5.6-terra":      true,
	"xai.grok-4.3":              true,
}

// isMantleOpenAIV1Model reports whether modelID is served on Bedrock Mantle's
// /openai/v1 path (GPT-5.x and xAI Grok). Cross-region prefixes are stripped
// before the lookup.
func isMantleOpenAIV1Model(modelID string) bool {
	return mantleOpenAIV1ModelIDs[stripRegionPrefix(modelID)]
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
	// GPT-5.x and xAI Grok live on Mantle but use a distinct /openai/v1 path
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
