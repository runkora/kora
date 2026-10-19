package bridge

import (
	"context"
	"errors"
	"io"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// ChatCompletionsResponsesBridge translates Responses API calls to the OpenAI
// Chat Completions API. Used for models that support Chat Completions but lack
// native Responses API support (e.g. non-Anthropic, non-Mantle Bedrock models).
// It is provider-agnostic — any connector holding an *openai.Client can use it.
type ChatCompletionsResponsesBridge struct {
	client   *openai.Client
	provider inferencegw.ProviderName
	logger   logging.Logger
}

// NewChatCompletionsResponsesBridge constructs a bridge for the given OpenAI-compatible client.
func NewChatCompletionsResponsesBridge(
	client *openai.Client,
	provider inferencegw.ProviderName,
	logger logging.Logger,
) *ChatCompletionsResponsesBridge {
	return &ChatCompletionsResponsesBridge{
		client:   client,
		provider: provider,
		logger:   logger,
	}
}

// Execute translates a Responses API request to Chat Completions, executes it,
// and translates the result back to a Responses API response.
func (b *ChatCompletionsResponsesBridge) Execute(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	b.logger.Debug(ctx, "bridge: chat completions responses execute start",
		logging.Field{Key: "model", Value: params.Model},
	)

	ccParams, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err != nil {
		return nil, convert.WrapTranslationError(b.provider, err)
	}

	completion, err := b.client.Chat.Completions.New(ctx, ccParams)
	if err != nil {
		return nil, convert.MapOpenAIError(b.provider, err)
	}

	resp := convert.ChatCompletionToResponsesResponse(completion)
	b.logger.Debug(ctx, "bridge: chat completions responses execute done",
		logging.Field{Key: "status", Value: string(resp.Status)},
	)
	return &inferencegw.ResponsesResult{Response: resp}, nil
}

// ExecuteStream translates a Responses API streaming request to Chat Completions
// streaming, translating each chunk back to Responses API stream events forwarded
// on the returned channel.
func (b *ChatCompletionsResponsesBridge) ExecuteStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	b.logger.Debug(ctx, "bridge: chat completions responses stream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	ccParams, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err != nil {
		return nil, convert.WrapTranslationError(b.provider, err)
	}

	// Request usage in the trailing chunk so token counts are available for billing.
	ccParams.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	stream := b.client.Chat.Completions.NewStreaming(ctx, ccParams)

	ch := make(chan inferencegw.ResponsesStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()

		state := convert.NewChatCompletionsStreamState()
		var completed bool
		for stream.Next() {
			for _, ev := range convert.ChatCompletionChunkToResponsesEvents(stream.Current(), state) {
				if ev.Type == "response.completed" {
					completed = true
				}
				ch <- inferencegw.ResponsesStreamResult{Event: ev}
			}
		}
		// Flush the deferred response.completed if the usage chunk never arrived
		// (provider doesn't support stream_options) — ensures the terminal event
		// is always emitted with whatever token counts are available.
		if ev := state.FlushPendingCompletion(0, 0, 0); ev != nil {
			completed = true
			ch <- inferencegw.ResponsesStreamResult{Event: *ev}
		}
		// Suppress trailing errors after a terminal response.completed event —
		// the client has the final response and emitting an error here confuses
		// SDKs that latch on the first terminal event.
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) && !completed {
			b.logger.Error(ctx, "bridge: chat completions responses stream error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			ch <- inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(b.provider, err)}
		}
	}()

	return ch, nil
}
