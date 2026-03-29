package azure

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

// AnthropicMessages is the zero-translation path for POST /v1/messages.
// Azure supports the Anthropic Messages API natively via its anthropicClient.
func (c *Connector) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	c.logger.Debug(ctx, "azure: AnthropicMessages start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	msg, err := c.anthropicClient.Messages.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "azure: AnthropicMessages error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapAnthropicError(inferencegw.ProviderAzure, err)
	}

	c.logger.Debug(ctx, "azure: AnthropicMessages done",
		logging.Field{Key: "stop_reason", Value: string(msg.StopReason)},
	)
	return msg, nil
}

// AnthropicMessagesStream is the zero-translation streaming path for POST /v1/messages.
// Mid-stream errors are delivered as AnthropicStreamEvent{Err: ...} before the
// channel is closed.
func (c *Connector) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	c.logger.Debug(ctx, "azure: AnthropicMessagesStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)

	ch := make(chan inferencegw.AnthropicStreamEvent, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		var eventCount int
		for stream.Next() {
			eventCount++
			ch <- inferencegw.AnthropicStreamEvent{Event: stream.Current()}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			c.logger.Error(ctx, "azure: AnthropicMessagesStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.AnthropicStreamEvent{Err: convert.MapAnthropicError(inferencegw.ProviderAzure, err)}
			return
		}
		c.logger.Debug(ctx, "azure: AnthropicMessagesStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		)
	}()

	return ch, nil
}

func (c *Connector) anthropicChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	params := convert.ToAnthropicParams(req)
	c.logger.Debug(ctx, "azure: anthropicChatCompletion start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	msg, err := c.anthropicClient.Messages.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "azure: anthropicChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return inferencegw.ChatCompletionResponse{}, convert.MapAnthropicError(inferencegw.ProviderAzure, err)
	}

	c.logger.Debug(ctx, "azure: anthropicChatCompletion done",
		logging.Field{Key: "stop_reason", Value: string(msg.StopReason)},
	)
	return convert.FromAnthropicMessage(msg), nil
}

func (c *Connector) anthropicChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	params := convert.ToAnthropicParams(req)
	c.logger.Debug(ctx, "azure: anthropicChatCompletionStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		var (
			accumulated anthropic.Message
			eventCount  int
		)
		for stream.Next() {
			eventCount++
			event := stream.Current()
			if err := accumulated.Accumulate(event); err != nil {
				c.logger.Error(ctx, "azure: anthropicChatCompletionStream accumulate error",
					logging.Field{Key: "error", Value: err.Error()},
					logging.Field{Key: "event_type", Value: event.Type},
				)
				ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderAzure, err)}
				return
			}
			if chunk, ok := convert.AnthropicEventToChunk(event); ok {
				ch <- chunk
			}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			c.logger.Error(ctx, "azure: anthropicChatCompletionStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderAzure, err)}
			return
		}
		c.logger.Debug(ctx, "azure: anthropicChatCompletionStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
			logging.Field{Key: "stop_reason", Value: string(accumulated.StopReason)},
		)
	}()

	return ch, nil
}
