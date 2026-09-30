// Package openaicompat connects the gateway to any OpenAI-compatible endpoint:
// OpenAI, OpenRouter, vLLM, KServe. Chat completions and embeddings pass
// through. Responses run through the chat-completions bridge; not every
// compatible server implements /responses. Anthropic Messages are not served.
package openaicompat

import (
	"context"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bridge"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds one OpenAI-compatible endpoint. An empty APIKey sends no
// Authorization header.
type Config struct {
	BaseURL string
	APIKey  string
}

// Connector implements inferencegw.ProviderConnector for an OpenAI-compatible
// endpoint.
type Connector struct {
	name   inferencegw.ProviderName
	client *openai.Client
	bridge *bridge.ChatCompletionsResponsesBridge
	logger logging.Logger
}

// New builds a connector. The SDK's environment defaults (OPENAI_API_KEY,
// OPENAI_BASE_URL) are overridden; only cfg reaches the upstream.
func New(cfg Config, name inferencegw.ProviderName, logger logging.Logger) (*Connector, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("openaicompat: base URL is required")
	}
	opts := []openaiopt.RequestOption{
		openaiopt.WithBaseURL(cfg.BaseURL),
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.openai-compatible", nil),
		)),
	}
	if cfg.APIKey != "" {
		opts = append(opts, openaiopt.WithAPIKey(cfg.APIKey))
	} else {
		opts = append(opts, openaiopt.WithAPIKey(""), openaiopt.WithHeaderDel("Authorization"))
	}
	client := openai.NewClient(opts...)
	return &Connector{
		name:   name,
		client: &client,
		bridge: bridge.NewChatCompletionsResponsesBridge(&client, name, logger),
		logger: logger,
	}, nil
}

// Name returns the provider name.
func (c *Connector) Name() inferencegw.ProviderName { return c.name }

// ListModels lists the endpoint's /models.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	iter := c.client.Models.ListAutoPaging(ctx)
	var models []inferencegw.ProviderModel
	for iter.Next() {
		models = append(models, ModelFromID(c.name, iter.Current().ID))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}
	return models, nil
}

// ModelFromID describes a model known only by its id. OpenRouter-style ids
// carry the vendor before "/"; others are matched on well-known substrings.
func ModelFromID(provider inferencegw.ProviderName, id string) inferencegw.ProviderModel {
	text := []inferencegw.Modality{inferencegw.ModalityText}
	m := inferencegw.ProviderModel{
		Provider:         provider,
		ModelID:          id,
		Family:           familyFromID(id),
		DisplayName:      id,
		InputModalities:  text,
		OutputModalities: text,
		Dialects:         []inferencegw.APIDialect{inferencegw.DialectOpenAI, inferencegw.DialectResponses},
		Capabilities:     []inferencegw.ModelCapability{inferencegw.CapabilityChat},
	}
	if strings.Contains(strings.ToLower(id), "embed") {
		m.OutputModalities = []inferencegw.Modality{inferencegw.ModalityEmbedding}
		m.Dialects = nil
		m.Capabilities = []inferencegw.ModelCapability{inferencegw.CapabilityEmbedding}
	}
	return m
}

var familyRules = []struct{ contains, family string }{
	{"claude", "anthropic"},
	{"gpt", "openai"},
	{"llama", "meta"},
	{"mistral", "mistral"},
	{"mixtral", "mistral"},
	{"gemini", "google"},
	{"gemma", "google"},
	{"deepseek", "deepseek"},
	{"qwen", "qwen"},
}

func familyFromID(id string) string {
	lower := strings.ToLower(id)
	if vendor, _, ok := strings.Cut(lower, "/"); ok && vendor != "" {
		return vendor
	}
	if len(lower) >= 2 && lower[0] == 'o' && lower[1] >= '1' && lower[1] <= '9' {
		return "openai"
	}
	for _, r := range familyRules {
		if strings.Contains(lower, r.contains) {
			return r.family
		}
	}
	return "unknown"
}

// ChatCompletion passes a chat completion through.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*inferencegw.OpenAIResult, error) {
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, convert.MapOpenAIError(c.name, err)
	}
	return &inferencegw.OpenAIResult{Completion: resp}, nil
}

// ChatCompletionStream passes a streaming chat completion through.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		for stream.Next() {
			chunk := stream.Current()
			ch <- inferencegw.OpenAIStreamResult{Chunk: &chunk}
		}
		if err := stream.Err(); err != nil {
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapOpenAIError(c.name, err)}
		}
	}()
	return ch, nil
}

// Embeddings passes an embeddings request through.
func (c *Connector) Embeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, convert.MapOpenAIError(c.name, err)
	}
	return resp, nil
}

// Responses runs a Responses request as a chat completion.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	return c.bridge.Execute(ctx, params)
}

// ResponsesStream runs a streaming Responses request as a chat completion.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	return c.bridge.ExecuteStream(ctx, params)
}

// AnthropicMessages is not served by an OpenAI-compatible endpoint.
func (c *Connector) AnthropicMessages(_ context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return nil, c.anthropicUnsupported(params.Model)
}

// AnthropicMessagesStream is not served by an OpenAI-compatible endpoint.
func (c *Connector) AnthropicMessagesStream(_ context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	return nil, c.anthropicUnsupported(params.Model)
}

// anthropicUnsupported is retryable: a virtual model may route to another
// provider that does serve Anthropic Messages.
func (c *Connector) anthropicUnsupported(model string) error {
	return &inferencegw.ConnectorError{
		Provider:  c.name,
		Code:      "MODEL_NOT_SUPPORTED",
		Retryable: true,
		Underlying: fmt.Errorf("model %q is served by an OpenAI-compatible provider; "+
			"use POST /v1/chat/completions or /v1/responses", model),
		ProviderMessage: "this model is not available on the Anthropic Messages API; use /v1/chat/completions or /v1/responses",
		HTTPStatus:      400,
	}
}
