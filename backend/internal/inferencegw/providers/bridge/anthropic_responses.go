package bridge

import (
	"context"
	"errors"
	"io"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// AnthropicResponsesBridge translates Responses API calls to the Anthropic
// Messages API. It is provider-agnostic — the same bridge works for any
// connector that holds an *anthropic.Client (Bedrock, Vertex AI, etc.).
type AnthropicResponsesBridge struct {
	client   *anthropic.Client
	provider inferencegw.ProviderName
	logger   logging.Logger
}

// NewAnthropicResponsesBridge constructs a bridge for the given Anthropic client.
func NewAnthropicResponsesBridge(
	client *anthropic.Client,
	provider inferencegw.ProviderName,
	logger logging.Logger,
) *AnthropicResponsesBridge {
	return &AnthropicResponsesBridge{
		client:   client,
		provider: provider,
		logger:   logger,
	}
}

// Execute translates a Responses API request to Anthropic Messages API,
// executes it, and translates the result back to a Responses API response.
func (b *AnthropicResponsesBridge) Execute(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	b.logger.Debug(ctx, "bridge: anthropic responses execute start",
		logging.Field{Key: "model", Value: params.Model},
	)

	anthropicParams, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		return nil, convert.WrapTranslationError(b.provider, err)
	}

	stream := b.client.Messages.NewStreaming(ctx, anthropicParams)
	defer stream.Close()

	var accumulated anthropic.Message
	for stream.Next() {
		if err := accumulated.Accumulate(stream.Current()); err != nil {
			return nil, convert.MapAnthropicError(b.provider, err)
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, convert.MapAnthropicError(b.provider, err)
	}

	resp := convert.AnthropicMessageToResponsesResponse(&accumulated)
	b.logger.Debug(ctx, "bridge: anthropic responses execute done",
		logging.Field{Key: "status", Value: string(resp.Status)},
	)
	return resp, nil
}

// ExecuteStream translates a Responses API streaming request to Anthropic
// Messages API streaming events, translating each event back to Responses API
// stream events forwarded on the returned channel.
func (b *AnthropicResponsesBridge) ExecuteStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	b.logger.Debug(ctx, "bridge: anthropic responses stream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	anthropicParams, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		return nil, convert.WrapTranslationError(b.provider, err)
	}

	stream := b.client.Messages.NewStreaming(ctx, anthropicParams)

	ch := make(chan inferencegw.ResponsesStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()

		state := convert.NewAnthropicResponsesStreamState()
		var completed bool
		for stream.Next() {
			for _, ev := range convert.AnthropicEventToResponsesEvents(stream.Current(), state) {
				if ev.Type == "response.completed" {
					completed = true
				}
				ch <- inferencegw.ResponsesStreamResult{Event: ev}
			}
		}
		// Suppress trailing errors after the upstream has already produced a
		// terminal response.completed event. The client has the final response;
		// reporting an error here just confuses SDKs that latch on completion.
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) && !completed {
			b.logger.Error(ctx, "bridge: anthropic responses stream error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			ch <- inferencegw.ResponsesStreamResult{Err: convert.MapAnthropicError(b.provider, err)}
		}
	}()

	return ch, nil
}
