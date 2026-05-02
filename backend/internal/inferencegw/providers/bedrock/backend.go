package bedrock

import "strings"

// backendID identifies the calling convention to use for a Bedrock model.
type backendID uint8

const (
	backendOpenAICompat backendID = iota // Bedrock /openai/v1 endpoint (Titan, Nova, Llama, Mistral, …)
	backendAnthropic                     // Anthropic Messages API on Bedrock (Claude models)
	backendMantle                        // Bedrock Mantle endpoint (AWS-hosted OpenAI models)
	backendTitanEmbed                    // native InvokeModel, Amazon Titan embed format
	backendCohereEmbed                   // native InvokeModel, Cohere embed format
)

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
	return backendOpenAICompat
}
