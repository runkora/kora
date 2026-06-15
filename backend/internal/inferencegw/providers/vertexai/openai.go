package vertexai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ChatCompletion sends a non-streaming chat completion request.
//
// Vertex AI has two physically separate endpoints:
//   - OpenAI-compatible endpoint: Gemini and Llama models
//   - Anthropic Messages endpoint: Claude models only (claude- prefix)
//
// This method transparently routes Claude models to the Anthropic path and
// converts the response back to the native openai.ChatCompletion SDK type,
// preserving tool_calls and all other fields with zero loss.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if isClaudeModel(params.Model) {
		return c.claudeChatCompletion(ctx, params)
	}
	return c.openaiChatCompletion(ctx, params)
}

// ChatCompletionStream opens a streaming chat completion request.
// Claude models are transparently routed to the Anthropic path — see ChatCompletion.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	if isClaudeModel(params.Model) {
		return c.claudeChatCompletionStream(ctx, params)
	}
	return c.openaiChatCompletionStream(ctx, params)
}

// openaiChatCompletion calls the Vertex AI OpenAI-compatible endpoint directly.
// Used for Gemini and Llama models.
func (c *Connector) openaiChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	c.logger.Debug(ctx, "vertexai: openaiChatCompletion start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "vertexai: openaiChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(inferencegw.ProviderVertexAI, err)
	}

	if len(resp.Choices) > 0 {
		c.logger.Debug(ctx, "vertexai: openaiChatCompletion done",
			logging.Field{Key: "finish_reason", Value: resp.Choices[0].FinishReason},
		)
	}
	return resp, nil
}

// openaiChatCompletionStream streams from the Vertex AI OpenAI-compatible endpoint.
// Used for Gemini and Llama models.
func (c *Connector) openaiChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	c.logger.Debug(ctx, "vertexai: openaiChatCompletionStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.openaiClient.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		chunkCount := 0
		for stream.Next() {
			chunkCount++
			cur := stream.Current()
			ch <- inferencegw.OpenAIStreamResult{Chunk: &cur}
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "vertexai: openaiChatCompletionStream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "chunks_before_error", Value: fmt.Sprintf("%d", chunkCount)},
			)
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapOpenAIError(inferencegw.ProviderVertexAI, err)}
			return
		}
		c.logger.Debug(ctx, "vertexai: openaiChatCompletionStream done",
			logging.Field{Key: "chunk_count", Value: fmt.Sprintf("%d", chunkCount)},
		)
	}()

	return ch, nil
}

// claudeChatCompletion routes a Claude model request through the Anthropic Messages
// API and converts the response back to an openai.ChatCompletion SDK type.
// This is a Vertex AI topology constraint — Claude models are not available on
// the OpenAI-compatible endpoint.
func (c *Connector) claudeChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	anthropicParams := convert.OpenAIParamsToAnthropicParams(params)
	c.logger.Debug(ctx, "vertexai: claudeChatCompletion start",
		logging.Field{Key: "model", Value: anthropicParams.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", anthropicParams.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(anthropicParams.Messages))},
	)

	startedAt := time.Now().Unix()
	msg, err := c.anthropicClient.Messages.New(ctx, anthropicParams)
	if err != nil {
		c.logger.Error(ctx, "vertexai: claudeChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapAnthropicError(inferencegw.ProviderVertexAI, err)
	}

	c.logger.Debug(ctx, "vertexai: claudeChatCompletion done",
		logging.Field{Key: "stop_reason", Value: string(msg.StopReason)},
	)
	return convert.AnthropicMessageToOpenAI(msg, startedAt), nil
}

// claudeChatCompletionStream routes a Claude model streaming request through the
// Anthropic Messages API and converts events to OpenAI chat.completion.chunk objects.
func (c *Connector) claudeChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	anthropicParams := convert.OpenAIParamsToAnthropicParams(params)
	c.logger.Debug(ctx, "vertexai: claudeChatCompletionStream start",
		logging.Field{Key: "model", Value: anthropicParams.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", anthropicParams.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(anthropicParams.Messages))},
	)

	stream := c.anthropicClient.Messages.NewStreaming(ctx, anthropicParams)

	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		state := convert.NewAnthropicStreamState()
		eventCount := 0
		for stream.Next() {
			eventCount++
			event := stream.Current()
			if chunk, ok := convert.AnthropicEventToOpenAIChunk(event, state); ok {
				ch <- inferencegw.OpenAIStreamResult{Chunk: chunk}
			}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			c.logger.Error(ctx, "vertexai: claudeChatCompletionStream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapAnthropicError(inferencegw.ProviderVertexAI, err)}
			return
		}
		c.logger.Debug(ctx, "vertexai: claudeChatCompletionStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		)
	}()

	return ch, nil
}
