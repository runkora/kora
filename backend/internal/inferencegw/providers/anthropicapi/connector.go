// Package anthropicapi connects the gateway to the Anthropic API or any
// Anthropic Messages endpoint. Messages pass through. Chat completions and
// Responses are converted with the shared Anthropic translations.
package anthropicapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bridge"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds one Anthropic Messages endpoint.
type Config struct {
	BaseURL string
	APIKey  string
}

// Connector implements inferencegw.ProviderConnector for the Anthropic API.
type Connector struct {
	name   inferencegw.ProviderName
	client *anthropic.Client
	bridge *bridge.AnthropicResponsesBridge
	logger logging.Logger
}

// New builds a connector. The SDK's environment defaults (ANTHROPIC_API_KEY,
// ANTHROPIC_BASE_URL) are overridden; only cfg reaches the upstream.
func New(cfg Config, name inferencegw.ProviderName, logger logging.Logger) (*Connector, error) {
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropicapi: base URL and API key are required")
	}
	client := anthropic.NewClient(
		anthropicopt.WithBaseURL(cfg.BaseURL),
		anthropicopt.WithAPIKey(cfg.APIKey),
		anthropicopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.anthropic", nil),
		)),
	)
	return &Connector{
		name:   name,
		client: &client,
		bridge: bridge.NewAnthropicResponsesBridge(&client, name, logger),
		logger: logger,
	}, nil
}

// Name returns the provider name.
func (c *Connector) Name() inferencegw.ProviderName { return c.name }

// ListModels lists the endpoint's models.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	iter := c.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	var models []inferencegw.ProviderModel
	for iter.Next() {
		info := iter.Current()
		m := ModelFromID(c.name, info.ID)
		if info.DisplayName != "" {
			m.DisplayName = info.DisplayName
		}
		models = append(models, m)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("%s: list models: %w", c.name, err)
	}
	return models, nil
}

// ModelFromID describes a Claude model known only by its id.
func ModelFromID(provider inferencegw.ProviderName, id string) inferencegw.ProviderModel {
	return inferencegw.ProviderModel{
		Provider:         provider,
		ModelID:          id,
		Family:           "anthropic",
		DisplayName:      id,
		InputModalities:  []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
		OutputModalities: []inferencegw.Modality{inferencegw.ModalityText},
		Dialects:         []inferencegw.APIDialect{inferencegw.DialectAnthropic, inferencegw.DialectOpenAI, inferencegw.DialectResponses},
		Capabilities:     []inferencegw.ModelCapability{inferencegw.CapabilityChat},
	}
}

// AnthropicMessages passes a Messages request through.
func (c *Connector) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	return c.message(ctx, params)
}

// message streams and accumulates one Messages call. The SDK refuses a
// non-streaming call whose max_tokens could run past ten minutes, and agent
// runtimes send such values.
func (c *Connector) message(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, convert.MapAnthropicError(c.name, err)
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, convert.MapAnthropicError(c.name, err)
	}
	return &msg, nil
}

// AnthropicMessagesStream passes a streaming Messages request through.
func (c *Connector) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	ch := make(chan inferencegw.AnthropicStreamEvent, 32)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		for stream.Next() {
			ch <- inferencegw.AnthropicStreamEvent{Event: stream.Current()}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			ch <- inferencegw.AnthropicStreamEvent{Err: convert.MapAnthropicError(c.name, err)}
		}
	}()
	return ch, nil
}

// ChatCompletion converts a chat completion to Messages and back.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*inferencegw.OpenAIResult, error) {
	startedAt := time.Now().Unix()
	msg, err := c.message(ctx, convert.OpenAIParamsToAnthropicParams(params))
	if err != nil {
		return nil, err
	}
	return &inferencegw.OpenAIResult{
		Completion:          convert.AnthropicMessageToOpenAI(msg, startedAt),
		CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
	}, nil
}

// ChatCompletionStream converts a streaming chat completion to Messages and
// streams chunks back.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	stream := c.client.Messages.NewStreaming(ctx, convert.OpenAIParamsToAnthropicParams(params))
	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		state := convert.NewAnthropicStreamState()
		for stream.Next() {
			event := stream.Current()
			if chunk, ok := convert.AnthropicEventToOpenAIChunk(event, state); ok {
				res := inferencegw.OpenAIStreamResult{Chunk: chunk}
				if event.Type == "message_delta" {
					res.CacheCreationTokens = state.CacheCreationTokens
				}
				ch <- res
			}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapAnthropicError(c.name, err)}
		}
	}()
	return ch, nil
}

// Responses runs a Responses request as Messages.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	return c.bridge.Execute(ctx, params)
}

// ResponsesStream runs a streaming Responses request as Messages.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	return c.bridge.ExecuteStream(ctx, params)
}

// Embeddings is not offered by the Anthropic API.
func (c *Connector) Embeddings(_ context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	return nil, &inferencegw.ConnectorError{
		Provider:        c.name,
		Code:            "MODEL_NOT_SUPPORTED",
		Underlying:      fmt.Errorf("model %q: the Anthropic API has no embeddings endpoint", params.Model),
		ProviderMessage: "the Anthropic API has no embeddings endpoint",
		HTTPStatus:      400,
	}
}
