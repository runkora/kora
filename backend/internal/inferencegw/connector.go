package inferencegw

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ============================================================================
// Dialect interfaces
// ============================================================================

// OpenAIConnector is the OpenAI-compatible dialect surface of a provider.
// It is invoked when the client speaks the OpenAI Chat Completions API
// (POST /v1/chat/completions). Request and response types are native openai-go
// SDK wire types — no hand-rolled translation layer, so tool calls, function
// calling, logprobs, and all other fields pass through unchanged.
type OpenAIConnector interface {
	ChatCompletion(ctx context.Context, req openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
	ChatCompletionStream(ctx context.Context, req openai.ChatCompletionNewParams) (<-chan OpenAIStreamResult, error)
}

// OpenAIStreamResult is one element emitted on a ChatCompletionStream channel.
// On a mid-stream provider error Err is non-nil and Chunk is nil; callers must
// check Err before using Chunk. The channel is always closed when the stream ends.
type OpenAIStreamResult struct {
	Chunk *openai.ChatCompletionChunk
	Err   error
}

// AnthropicStreamEvent wraps a raw Anthropic SDK stream event with an optional
// error. On a mid-stream provider error Err is non-nil and Event is the zero
// value; callers must check Err before using Event.
type AnthropicStreamEvent struct {
	Event anthropic.MessageStreamEventUnion
	Err   error
}

// AnthropicConnector is the Anthropic Messages API dialect surface of a provider.
// It is invoked when the client speaks the Anthropic Messages API
// (POST /v1/messages). Request and response types are native anthropic-sdk-go
// wire types — zero translation, no information loss.
type AnthropicConnector interface {
	// AnthropicMessages handles a non-streaming POST /v1/messages request.
	AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
	// AnthropicMessagesStream handles a streaming POST /v1/messages request.
	// Mid-stream errors are communicated via AnthropicStreamEvent.Err; the
	// channel is always closed when the stream ends (error or not).
	AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan AnthropicStreamEvent, error)
}

// ============================================================================
// Modality
// ============================================================================

// Modality describes a content type that a model can accept as input or produce
// as output. Values are lower-case strings so they serialize cleanly to JSON.
type Modality string

const (
	ModalityText      Modality = "text"
	ModalityImage     Modality = "image"
	ModalityEmbedding Modality = "embedding"
	ModalityAudio     Modality = "audio"
	ModalityVideo     Modality = "video"
)

// ============================================================================
// ModelCapability
// ============================================================================

// ModelCapability identifies the endpoint family a model belongs to.
// This is orthogonal to Modality (physical I/O types) and APIDialect
// (wire format within a family). A model's Capabilities slice drives which
// gateway handlers can serve it and whether it appears in GET /v1/models.
//
// Three-layer taxonomy:
//
//	ModelCapability  — what endpoint family?    (chat, embedding, image, …)
//	APIDialect       — which wire format?       (openai, anthropic)  [chat only]
//	Modality         — what physical I/O types? (text, image, embedding, …)
type ModelCapability string

const (
	// CapabilityChat — model supports POST /v1/chat/completions or POST /v1/messages.
	CapabilityChat ModelCapability = "chat"

	// CapabilityEmbedding — model supports POST /v1/embeddings.
	// Handler not yet implemented; models with this capability are hidden from
	// GET /v1/models until the handler ships.
	CapabilityEmbedding ModelCapability = "embedding"

	// CapabilityImageGeneration — model supports POST /v1/images/generations.
	// Handler not yet implemented.
	CapabilityImageGeneration ModelCapability = "image"

	// CapabilityVideoGeneration — model produces video output (async/polling).
	// No standard endpoint yet.
	CapabilityVideoGeneration ModelCapability = "video"

	// CapabilityRerank — model supports POST /v1/rerank (Cohere-style).
	// Handler not yet implemented.
	CapabilityRerank ModelCapability = "rerank"

	// CapabilityAudioGeneration — model produces audio output (TTS, live audio).
	// No standard endpoint yet.
	CapabilityAudioGeneration ModelCapability = "audio"
)

// ============================================================================
// ProviderModel
// ============================================================================

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

	// InputModalities describes what content types this model accepts as input.
	// e.g. ["text"] for text-only LLMs, ["text","image"] for vision models.
	InputModalities []Modality

	// OutputModalities describes what content types this model produces.
	// e.g. ["text"] for LLMs, ["embedding"] for embedding models,
	// ["image"] for image-generation models.
	OutputModalities []Modality

	// Dialects lists which gateway wire protocols can be used to call this model.
	// Only populated for models with CapabilityChat.
	// ["openai"]             - OpenAI Chat Completions (POST /v1/chat/completions)
	// ["openai","anthropic"] - both Chat Completions and Anthropic Messages API
	Dialects []APIDialect

	// Capabilities lists the endpoint families this model supports.
	// The GET /v1/models handler filters to models whose Capabilities overlap
	// with the set of active (implemented) handler families.
	// e.g. ["chat"] for LLMs, ["embedding"] for embedding models,
	//      ["image"] for image-generation models.
	Capabilities []ModelCapability
}

// ============================================================================
// ProviderConnector
// ============================================================================

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

// ============================================================================
// ConnectorFactory
// ============================================================================

// ConnectorFactory creates a ProviderConnector from environment-sourced config.
// Each provider package exports a Factory struct that implements this interface.
//
// Build returns (nil, nil) when the provider is disabled (env var not set),
// allowing main.go to skip registration without treating it as an error.
type ConnectorFactory interface {
	Name() ProviderName
	Build(logger logging.Logger) (ProviderConnector, error)
}

// ============================================================================
// ConnectorError
// ============================================================================

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

func (e ConnectorError) Error() string {
	return fmt.Sprintf("provider %s error [%s]: %v", e.Provider, e.Code, e.Underlying)
}

func (e ConnectorError) Unwrap() error {
	return e.Underlying
}

// ============================================================================
// ModelPricing
// ============================================================================

// ModelPricing holds per-million-token cost data for a specific provider+model.
// Pricing data flows exclusively from PricingCache -- never from the connector.
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
