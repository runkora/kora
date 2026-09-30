package vertexai

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
//
// Only Claude models are supported on this endpoint. Non-Claude models (Gemini,
// Llama, etc.) must use POST /v1/chat/completions via the OpenAI dialect.
//
// Vertex AI requires streaming for long-running operations so we always use
// NewStreaming internally and accumulate for the non-streaming caller.
func (c *Connector) AnthropicMessages(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	c.logger.Debug(ctx, "vertexai: AnthropicMessages start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	if !isClaudeModel(params.Model) {
		return nil, &inferencegw.ConnectorError{
			Provider:  c.name,
			Code:      "MODEL_NOT_SUPPORTED",
			Retryable: false,
			Underlying: fmt.Errorf(
				"model %q is not supported on the Anthropic Messages API endpoint; "+
					"only Claude models are supported — use POST /v1/chat/completions for this model",
				params.Model,
			),
		}
	}

	// Claude path: Anthropic Vertex AI client (streaming + accumulate).
	stream := c.anthropicClient.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var (
		accumulated anthropic.Message
		eventCount  int
	)
	for stream.Next() {
		eventCount++
		event := stream.Current()
		if err := accumulated.Accumulate(event); err != nil {
			c.logger.Error(ctx, "vertexai: AnthropicMessages accumulate error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "event_type", Value: event.Type},
			)
			return nil, convert.MapAnthropicError(c.name, err)
		}
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		c.logger.Error(ctx, "vertexai: AnthropicMessages error",
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
		)
		return nil, convert.MapAnthropicError(c.name, err)
	}

	c.logger.Debug(ctx, "vertexai: AnthropicMessages done",
		logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		logging.Field{Key: "stop_reason", Value: string(accumulated.StopReason)},
	)
	return &accumulated, nil
}

// AnthropicMessagesStream is the zero-translation streaming path for POST /v1/messages.
//
// Only Claude models are supported on this endpoint. Mid-stream errors are
// delivered as AnthropicStreamEvent{Err: ...} before the channel is closed.
func (c *Connector) AnthropicMessagesStream(ctx context.Context, params anthropic.MessageNewParams) (<-chan inferencegw.AnthropicStreamEvent, error) {
	c.logger.Debug(ctx, "vertexai: AnthropicMessagesStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", params.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	if !isClaudeModel(params.Model) {
		return nil, &inferencegw.ConnectorError{
			Provider:  c.name,
			Code:      "MODEL_NOT_SUPPORTED",
			Retryable: false,
			Underlying: fmt.Errorf(
				"model %q is not supported on the Anthropic Messages API endpoint; "+
					"only Claude models are supported — use POST /v1/chat/completions for this model",
				params.Model,
			),
		}
	}

	// Claude path: Anthropic Vertex AI client.
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
			c.logger.Error(ctx, "vertexai: AnthropicMessagesStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.AnthropicStreamEvent{Err: convert.MapAnthropicError(c.name, err)}
			return
		}
		c.logger.Debug(ctx, "vertexai: AnthropicMessagesStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		)
	}()

	return ch, nil
}
