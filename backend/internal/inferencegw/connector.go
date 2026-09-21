package inferencegw

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

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
	ChatCompletion(ctx context.Context, req openai.ChatCompletionNewParams) (*OpenAIResult, error)
	ChatCompletionStream(ctx context.Context, req openai.ChatCompletionNewParams) (<-chan OpenAIStreamResult, error)
}

// OpenAIResult is a non-streaming chat answer plus its cache-write token count.
// Anthropic prices a cache write above ordinary input, and neither the OpenAI
// nor the Responses usage shape has a field for that bucket, so a provider
// translating from Anthropic reports it here. Zero for providers that do not
// write cache. Never serialized to the client.
type OpenAIResult struct {
	Completion          *openai.ChatCompletion
	CacheCreationTokens int64
}

// ResponsesResult is the Responses equivalent of OpenAIResult.
type ResponsesResult struct {
	Response            *responses.Response
	CacheCreationTokens int64
}

// OpenAIStreamResult is one element emitted on a ChatCompletionStream channel.
// On a mid-stream provider error Err is non-nil and Chunk is nil; callers must
// check Err before using Chunk. The channel is always closed when the stream ends.
// CacheCreationTokens carries the cache-write count, as on OpenAIResult.
type OpenAIStreamResult struct {
	Chunk               *openai.ChatCompletionChunk
	Err                 error
	CacheCreationTokens int64
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

// EmbeddingsConnector is the embeddings dialect surface of a provider.
// It is invoked when the client calls POST /v1/embeddings.
type EmbeddingsConnector interface {
	Embeddings(ctx context.Context, req openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error)
}

// ResponsesStreamResult is one element emitted on a ResponsesStream channel.
// On a mid-stream provider error Err is non-nil and Event is the zero value;
// callers must check Err before using Event. The channel is always closed when
// the stream ends.
type ResponsesStreamResult struct {
	Event responses.ResponseStreamEventUnion
	Err   error
	// CacheCreationTokens carries the cache-write count on the terminal event,
	// matching the side channel on OpenAIStreamResult.
	CacheCreationTokens int64
}

// ResponsesConnector is the OpenAI Responses API dialect surface of a provider.
// It is invoked when the client calls POST /v1/responses.
type ResponsesConnector interface {
	Responses(ctx context.Context, req responses.ResponseNewParams) (*ResponsesResult, error)
	ResponsesStream(ctx context.Context, req responses.ResponseNewParams) (<-chan ResponsesStreamResult, error)
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

	// Family is the canonical vendor/model-family identifier used for policy
	// scope construction. It is provider-agnostic — the same family may be
	// served by multiple providers (e.g. "anthropic" models are available on
	// both Bedrock and Vertex AI).
	// Examples: "anthropic", "amazon", "meta", "mistral", "google", "cohere".
	Family string

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

// PolicyAlias returns the shortest clean alias for use in policy scope
// construction (e.g. "claude-3-5-haiku", "nova-pro-v1").
// Picks the shortest entry across ModelID and Aliases so that policy scopes
// remain human-readable regardless of which provider canonical form is stored.
func (m ProviderModel) PolicyAlias() string {
	best := m.ModelID
	for _, a := range m.Aliases {
		if len(a) < len(best) {
			best = a
		}
	}
	return best
}

// ============================================================================
// ProviderConnector
// ============================================================================

// ProviderConnector is the full abstraction over a cloud AI provider.
// Every provider (Bedrock, Vertex AI, Azure) implements all dialect interfaces.
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
	EmbeddingsConnector
	ResponsesConnector
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

	// ProviderMessage is the human-readable error message from the provider's
	// response body. Populated when the provider returns a structured error (e.g.
	// Anthropic {"error":{"message":"..."}}). Empty for unstructured errors.
	// Safe to surface to API callers — contains no internal infrastructure detail.
	ProviderMessage string

	// HTTPStatus is the HTTP response status code returned by the provider.
	// Zero if the error did not originate from an HTTP response (e.g. network
	// failure, context cancellation, or internal routing errors).
	HTTPStatus int
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
	Provider                 ProviderName
	ModelID                  string
	InputCostPerMillion      float64 // USD per 1,000,000 input tokens
	OutputCostPerMillion     float64 // USD per 1,000,000 output tokens
	CacheReadCostPerMillion  float64 // USD per 1,000,000 cache-read tokens (0 → use 10% of input)
	CacheWriteCostPerMillion float64 // USD per 1,000,000 cache-write tokens (0 → use 125% of input)
}

// EstimatedCostUSD returns the estimated total cost for the given token counts,
// including Anthropic prompt-cache read and write costs when non-zero.
// Cache pricing falls back to industry-standard ratios when not explicitly set:
//   - cache read  = 10% of input price
//   - cache write = 125% of input price
func (p ModelPricing) EstimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens int) float64 {
	cacheReadPrice := p.CacheReadCostPerMillion
	if cacheReadPrice == 0 {
		cacheReadPrice = p.InputCostPerMillion * 0.10
	}
	cacheCreationPrice := p.CacheWriteCostPerMillion
	if cacheCreationPrice == 0 {
		cacheCreationPrice = p.InputCostPerMillion * 1.25
	}
	return (float64(promptTokens)/1_000_000)*p.InputCostPerMillion +
		(float64(completionTokens)/1_000_000)*p.OutputCostPerMillion +
		(float64(cacheReadTokens)/1_000_000)*cacheReadPrice +
		(float64(cacheCreationTokens)/1_000_000)*cacheCreationPrice
}

// ============================================================================
// MultiConnectorFactory
// ============================================================================

// NamedConnector pairs a ProviderConnector with its runtime-assigned name.
// Used by MultiConnectorFactory to return multiple connectors — one per
// configured account/environment — from a single Build call.
type NamedConnector struct {
	Name      ProviderName
	Connector ProviderConnector
}

// MultiConnectorFactory creates zero or more ProviderConnectors from
// environment-sourced config. Providers that support multiple simultaneous
// environments (e.g. multiple AWS accounts for Bedrock) implement this
// interface alongside or instead of ConnectorFactory.
//
// Build returns nil when the provider is disabled, allowing main.go to skip
// registration without treating it as an error.
type MultiConnectorFactory interface {
	Build(logger logging.Logger) ([]NamedConnector, error)
}
