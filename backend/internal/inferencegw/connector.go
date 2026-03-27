package inferencegw

import (
	"context"
	"fmt"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Dialect interfaces ───────────────────────────────────────────────────────

// OpenAIConnector is the OpenAI-compatible dialect surface of a provider.
// It is invoked when the client speaks the OpenAI Chat Completions API
// (POST /v1/chat/completions). The router/handler layer selects this dialect
// based on the endpoint the client called — never based on model name.
type OpenAIConnector interface {
	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)
	ChatCompletionStream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, error)
}

// AnthropicConnector is the Anthropic Messages API dialect surface of a provider.
// It is invoked when the client speaks the Anthropic Messages API
// (POST /v1/messages). The router/handler layer selects this dialect
// based on the endpoint the client called — never based on model name.
type AnthropicConnector interface {
	AnthropicChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)
	AnthropicChatCompletionStream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, error)
}

// ─── ProviderModel ───────────────────────────────────────────────────────────

// ProviderModel is a model returned by a live provider listing.
type ProviderModel struct {
	// Provider is the connector that owns this model.
	Provider ProviderName

	// ModelID is the canonical working ID passed to the provider on invocation.
	// No provider/ prefix. For Bedrock this is the inference profile ID where one
	// exists (e.g. "us.anthropic.claude-sonnet-4-6"), not the bare foundation
	// model ID.
	ModelID string

	// DisplayName is a human-readable label suitable for UIs.
	// e.g. "Claude Sonnet 4.6", "Llama 3.3 70B Instruct".
	// Auto-generated from ModelID when no curated name is available.
	DisplayName string

	// Aliases are stable, clean identifiers that resolve to this model.
	// No provider/ prefix; clients may use "provider/alias" in requests.
	// e.g. ["claude-sonnet-4-6", "anthropic.claude-sonnet-4-6"].
	// Operator-curated aliases will be merged in by the routing engine.
	Aliases []string
}

// ─── ProviderConnector ────────────────────────────────────────────────────────

// ProviderConnector is the full abstraction over a cloud AI provider.
// Every provider (Bedrock, Vertex AI, Azure) implements both dialect interfaces.
// Providers that cannot support a dialect return ErrDialectNotSupported.
//
// Dialect selection is the responsibility of the HTTP handler and service layer.
// Connectors never inspect the model name to decide which dialect to invoke.
type ProviderConnector interface {
	// Name returns the canonical provider identifier.
	Name() ProviderName

	// ListModels returns the live list of usable models available on this provider.
	// Each entry carries a canonical ModelID (no provider/ prefix), a human-readable
	// DisplayName, and zero or more clean Aliases that the routing engine will
	// register for alias resolution.
	ListModels(ctx context.Context) ([]ProviderModel, error)

	OpenAIConnector
	AnthropicConnector
}

// ─── ConnectorFactory ────────────────────────────────────────────────────────

// ConnectorFactory creates a ProviderConnector from environment-sourced config.
// Each provider package exports a Factory struct that implements this interface.
//
// Build returns (nil, nil) when the provider is disabled (env var not set),
// allowing main.go to skip registration without treating it as an error.
type ConnectorFactory interface {
	Name() ProviderName
	Build(logger logging.Logger) (ProviderConnector, error)
}

// ─── ConnectorError ──────────────────────────────────────────────────────────

// ConnectorError wraps a provider-side error with a stable code that can be
// stored in inference_usage.error_code and used for routing decisions.
type ConnectorError struct {
	// Provider is the connector that produced the error.
	Provider ProviderName

	// Code is a stable upper-case identifier, e.g. "THROTTLED", "CONTEXT_WINDOW_EXCEEDED".
	Code string

	// Retryable indicates whether the routing engine should attempt another
	// provider in the failover chain.
	Retryable bool

	// Underlying is the raw provider error (never surfaced to callers).
	Underlying error
}

func (e *ConnectorError) Error() string {
	return fmt.Sprintf("provider %s error [%s]: %v", e.Provider, e.Code, e.Underlying)
}

func (e *ConnectorError) Unwrap() error {
	return e.Underlying
}

// ─── ModelPricing ─────────────────────────────────────────────────────────────

// ModelPricing holds per-million-token cost data for a specific provider+model.
// Pricing data flows exclusively from PricingCache — never from the connector.
type ModelPricing struct {
	Provider             ProviderName
	ModelID              string
	InputCostPerMillion  float64 // USD per 1,000,000 input tokens
	OutputCostPerMillion float64 // USD per 1,000,000 output tokens
}

// EstimatedCostUSD returns the estimated total cost for the given token counts.
func (p ModelPricing) EstimatedCostUSD(promptTokens, completionTokens int) float64 {
	return (float64(promptTokens)/1_000_000)*p.InputCostPerMillion +
		(float64(completionTokens)/1_000_000)*p.OutputCostPerMillion
}
