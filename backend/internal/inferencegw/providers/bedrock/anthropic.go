package bedrock

import (
	"context"
	"errors"
	"fmt"
	"io"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// anthropicChatCompletion performs a non-streaming Anthropic Messages call.
// Bedrock requires streaming for newer models (e.g. Haiku 4.5, Sonnet 4.6), so
// we always use the streaming API internally and accumulate the full response.
// This is transparent to the caller — the response shape is identical.
func (c *Connector) anthropicChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	params := convert.ToAnthropicParams(req)
	c.logger.Debug(ctx, "bedrock: anthropicChatCompletion start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)

	var (
		accumulated anthropic.Message
		eventCount  int
	)
	for stream.Next() {
		eventCount++
		event := stream.Current()
		if err := accumulated.Accumulate(event); err != nil {
			c.logger.Error(ctx, "bedrock: anthropicChatCompletion accumulate error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "event_type", Value: event.Type},
			)
			return inferencegw.ChatCompletionResponse{}, convert.MapAnthropicError(inferencegw.ProviderBedrock, err)
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.logger.Error(ctx, "bedrock: anthropicChatCompletion stream error",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
		)
		return inferencegw.ChatCompletionResponse{}, convert.MapAnthropicError(inferencegw.ProviderBedrock, err)
	}

	c.logger.Debug(ctx, "bedrock: anthropicChatCompletion done",
		logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		logging.Field{Key: "stop_reason", Value: string(accumulated.StopReason)},
	)
	return convert.FromAnthropicMessage(&accumulated), nil
}

func (c *Connector) anthropicChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	params := convert.ToAnthropicParams(req)
	c.logger.Debug(ctx, "bedrock: anthropicChatCompletionStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		var (
			accumulated anthropic.Message
			eventCount  int
		)
		for stream.Next() {
			eventCount++
			event := stream.Current()
			if err := accumulated.Accumulate(event); err != nil {
				c.logger.Error(ctx, "bedrock: anthropicChatCompletionStream accumulate error",
					logging.Field{Key: "error", Value: err.Error()},
					logging.Field{Key: "event_type", Value: event.Type},
				)
				ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderBedrock, err)}
				return
			}
			if chunk, ok := convert.AnthropicEventToChunk(event); ok {
				ch <- chunk
			}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			c.logger.Error(ctx, "bedrock: anthropicChatCompletionStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderBedrock, err)}
			return
		}
		c.logger.Debug(ctx, "bedrock: anthropicChatCompletionStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
			logging.Field{Key: "stop_reason", Value: string(accumulated.StopReason)},
		)
	}()

	return ch, nil
}
