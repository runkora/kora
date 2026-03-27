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

// anthropicChatCompletion performs a non-streaming Anthropic Messages call via
// the normalised Orbit domain types. Used for failover/metrics paths.
// Bedrock requires streaming for newer models so we accumulate internally.
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

// AnthropicMessages is the zero-translation path for POST /v1/messages.
// It takes native SDK params directly (decoded by the handler from the wire)
// and returns a native *anthropic.Message, so no information is lost.
// Bedrock always streams internally; we accumulate into a single Message.
func (c *Connector) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	c.logger.Debug(ctx, "bedrock: AnthropicMessages start",
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
			c.logger.Error(ctx, "bedrock: AnthropicMessages accumulate error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "event_type", Value: event.Type},
			)
			return nil, convert.MapAnthropicError(inferencegw.ProviderBedrock, err)
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.logger.Error(ctx, "bedrock: AnthropicMessages stream error",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
		)
		return nil, convert.MapAnthropicError(inferencegw.ProviderBedrock, err)
	}

	c.logger.Debug(ctx, "bedrock: AnthropicMessages done",
		logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		logging.Field{Key: "stop_reason", Value: string(accumulated.StopReason)},
	)
	return &accumulated, nil
}

// AnthropicMessagesStream is the zero-translation streaming path for POST /v1/messages.
// Events are forwarded raw from the Bedrock SSE stream so the handler can
// re-serialize them verbatim to the client without any intermediate conversion.
// Mid-stream errors are delivered as AnthropicStreamEvent{Err: ...} before the
// channel is closed.
func (c *Connector) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	c.logger.Debug(ctx, "bedrock: AnthropicMessagesStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)

	ch := make(chan inferencegw.AnthropicStreamEvent, 32)
	go func() {
		defer close(ch)
		var eventCount int
		for stream.Next() {
			eventCount++
			ch <- inferencegw.AnthropicStreamEvent{Event: stream.Current()}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			c.logger.Error(ctx, "bedrock: AnthropicMessagesStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.AnthropicStreamEvent{Err: convert.MapAnthropicError(inferencegw.ProviderBedrock, err)}
			return
		}
		c.logger.Debug(ctx, "bedrock: AnthropicMessagesStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		)
	}()

	return ch, nil
}
