// Package inferencegw implements the Orbit Inference Gateway service.
// It provides a provider-agnostic inference API, routing requests to
// cloud AI providers (AWS Bedrock, Google Vertex AI, Azure AI Inference)
// with per-principal usage attribution and policy enforcement hooks.
package inferencegw

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ─── Dialect ─────────────────────────────────────────────────────────────────

// APIDialect identifies which wire protocol was used for the inbound request.
type APIDialect string

const (
	// DialectOpenAI is the OpenAI Chat Completions API format (default).
	DialectOpenAI APIDialect = "openai"
	// DialectAnthropic is the Anthropic Messages API format.
	DialectAnthropic APIDialect = "anthropic"
)

// ─── Provider ────────────────────────────────────────────────────────────────

// ProviderName identifies a cloud AI provider.
type ProviderName string

const (
	ProviderBedrock  ProviderName = "bedrock"
	ProviderVertexAI ProviderName = "vertexai"
	ProviderAzure    ProviderName = "azure"
)

// ─── Routing ─────────────────────────────────────────────────────────────────

// RoutingStrategy controls how the routing engine selects a provider+model.
type RoutingStrategy string

const (
	// StrategyLowestCost selects the cheapest enabled model for the request.
	StrategyLowestCost RoutingStrategy = "lowest_cost"
	// StrategyFailoverChain tries providers in priority order, advancing on error.
	StrategyFailoverChain RoutingStrategy = "failover_chain"
	// StrategyExplicit honors the model field in the request exactly.
	StrategyExplicit RoutingStrategy = "explicit"
)

// ─── Request / response ──────────────────────────────────────────────────────

// MessageContent holds the content field of a ChatMessage.
// The OpenAI API allows content to be either a plain string or an array
// of typed content parts (e.g. [{"type":"text","text":"..."}] for multi-modal).
// We normalise both forms to a plain string so connectors never see the array form.
type MessageContent string

// UnmarshalJSON accepts either a JSON string or an array of content-part objects.
// Content parts must all be of type "text" — the gateway is text-only and returns
// a validation error if any non-text part (e.g. image_url) is encountered, rather
// than silently dropping it and sending a truncated prompt to the provider.
func (c *MessageContent) UnmarshalJSON(b []byte) error {
	// Fast path: plain string.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = MessageContent(s)
		return nil
	}

	// Slow path: array of content-part objects.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &parts); err != nil {
		return err
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			return fmt.Errorf("%w: content part type %q is not supported; the inference gateway is text-only",
				ErrInvalidRequest, p.Type)
		}
		sb.WriteString(p.Text)
	}
	*c = MessageContent(sb.String())
	return nil
}

// String returns the plain-text content.
func (c MessageContent) String() string { return string(c) }

// ChatMessage is a single message in a chat conversation.
type ChatMessage struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

// ChatCompletionRequest is the normalised internal request type.
// All dialects are translated to this before being handed to a connector.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stream      bool          `json:"stream,omitempty"`

	// SystemPrompt is extracted from a leading system message when present.
	SystemPrompt string `json:"-"`
}

// ChatCompletionChoice is a single generated completion.
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// UsageStats records token consumption for a single inference call.
type UsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse is the normalised internal response type.
// Connectors translate provider responses into this before returning.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   UsageStats             `json:"usage"`
}

// StreamDelta carries a partial token in a streaming response.
type StreamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}

// ChatCompletionChunk is one SSE event in a streaming response.
type ChatCompletionChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Delta        StreamDelta `json:"delta"`
		FinishReason *string     `json:"finish_reason"`
	} `json:"choices"`
	// Usage is populated on the final chunk by providers that support
	// streaming token counts (e.g. Bedrock, Vertex AI).
	// Earlier chunks carry zero values.
	Usage UsageStats `json:"usage"`
	// Err is non-nil when the stream encountered an error and must be closed.
	// Connectors send a terminal chunk with Err set rather than returning an
	// error from the channel itself.
	Err error `json:"-"`
}

// ─── Usage record ────────────────────────────────────────────────────────────

// UsageStatus describes the outcome of an inference request.
type UsageStatus string

const (
	UsageStatusSuccess       UsageStatus = "success"
	UsageStatusProviderError UsageStatus = "provider_error"
	UsageStatusPolicyDenied  UsageStatus = "policy_denied"
	UsageStatusAuthError     UsageStatus = "auth_error"
	UsageStatusTimeout       UsageStatus = "timeout"
	UsageStatusRoutingFailed UsageStatus = "routing_failed"
)

// InferenceUsageRecord is a completed inference event written to the database.
type InferenceUsageRecord struct {
	ID               string
	TenantID         string
	PrincipalID      string
	PrincipalType    string
	APIDialect       APIDialect
	Provider         *ProviderName // nil when routing failed
	ModelID          string
	RoutingStrategy  *RoutingStrategy // nil when routing failed
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCostUSD float64
	Status           UsageStatus
	RetryCount       int
	ErrorCode        *string
	LatencyMS        int
	CreatedAt        time.Time
}

// InferenceUsageFilter is used to query usage records.
type InferenceUsageFilter struct {
	TenantID      *string
	PrincipalID   *string
	PrincipalType *string
	Provider      *ProviderName
	APIDialect    *APIDialect
	Status        *UsageStatus
	From          *time.Time
	To            *time.Time
	Limit         int
	Offset        int
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrPolicyDenied          = errors.New("inference request denied by policy")
	ErrNoProvidersEnabled    = errors.New("no providers enabled")
	ErrProviderNotConfigured = errors.New("provider is not enabled or configured")
	ErrRoutingFailed         = errors.New("routing failed: no suitable provider/model found")
	ErrProviderError         = errors.New("provider returned an error")
	ErrInvalidRequest        = errors.New("invalid inference request")
	ErrTokenParseFailed      = errors.New("failed to parse platform token")
	ErrUsageRecordNotFound   = errors.New("inference usage record not found")
)
