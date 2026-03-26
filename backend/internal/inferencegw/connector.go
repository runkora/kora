package inferencegw

import (
	"context"
	"fmt"
)

// ─── ProviderConnector ────────────────────────────────────────────────────────

// ProviderConnector is the abstraction over a cloud AI provider.
// Each enabled provider (Bedrock, Vertex AI, Azure) has exactly one
// implementation. Pricing is handled by PricingCache — not by connectors.
type ProviderConnector interface {
	// Name returns the canonical provider identifier.
	Name() ProviderName

	// ChatCompletion sends a non-streaming inference request and returns
	// the complete response. Usage stats are embedded in the response;
	// the service layer writes an InferenceUsageRecord.
	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)

	// ChatCompletionStream sends a streaming inference request. The connector
	// writes ChatCompletionChunk values to the returned channel until the
	// stream is exhausted or the context is cancelled. Errors encountered
	// mid-stream are sent on the error channel, then both channels are closed.
	// The caller must always drain both channels (or cancel the context).
	ChatCompletionStream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error)
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
