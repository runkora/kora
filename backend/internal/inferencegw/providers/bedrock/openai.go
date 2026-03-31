package bedrock

import (
	"context"
	"errors"
	"fmt"
	"io"

	openai "github.com/openai/openai-go"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ChatCompletion sends a non-streaming request via the appropriate Bedrock endpoint.
//
// Bedrock has two physically separate endpoints:
//   - OpenAI-compatible endpoint (/openai/v1): Titan, Nova, Llama, Mistral, etc.
//   - Anthropic Messages endpoint: Claude models only (anthropic. prefix)
//
// This method transparently routes Anthropic/Claude models to the Anthropic Messages
// API and converts the response back to the native openai.ChatCompletion SDK type,
// preserving tool_calls and all other fields with zero loss.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if isAnthropicModel(params.Model) {
		return c.claudeChatCompletion(ctx, params)
	}
	return c.openaiChatCompletion(ctx, params)
}

// ChatCompletionStream opens a streaming request via the appropriate Bedrock endpoint.
// Anthropic/Claude models are transparently routed to the Anthropic Messages streaming
// API — see ChatCompletion for the routing rationale.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	if isAnthropicModel(params.Model) {
		return c.claudeChatCompletionStream(ctx, params)
	}
	return c.openaiChatCompletionStream(ctx, params)
}

// openaiChatCompletion calls the Bedrock OpenAI-compatible endpoint.
// Used for non-Anthropic models (Titan, Nova, Llama, Mistral, etc.).
func (c *Connector) openaiChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	c.logger.Debug(ctx, "bedrock: openaiChatCompletion start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "bedrock: openaiChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
	}

	if len(resp.Choices) > 0 {
		c.logger.Debug(ctx, "bedrock: openaiChatCompletion done",
			logging.Field{Key: "finish_reason", Value: resp.Choices[0].FinishReason},
		)
	}
	return resp, nil
}

// openaiChatCompletionStream streams from the Bedrock OpenAI-compatible endpoint.
// Used for non-Anthropic models (Titan, Nova, Llama, Mistral, etc.).
func (c *Connector) openaiChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	c.logger.Debug(ctx, "bedrock: openaiChatCompletionStream start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	stream := c.openaiClient.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan inferencegw.OpenAIStreamResult, 32)
	go func() {
		defer close(ch)
		chunkCount := 0
		for stream.Next() {
			chunkCount++
			cur := stream.Current()
			ch <- inferencegw.OpenAIStreamResult{Chunk: &cur}
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "bedrock: openaiChatCompletionStream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "chunks_before_error", Value: fmt.Sprintf("%d", chunkCount)},
			)
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapOpenAIError(inferencegw.ProviderBedrock, err)}
			return
		}
		c.logger.Debug(ctx, "bedrock: openaiChatCompletionStream done",
			logging.Field{Key: "chunk_count", Value: fmt.Sprintf("%d", chunkCount)},
		)
	}()

	return ch, nil
}

// claudeChatCompletion routes a Claude/Anthropic model request through the Bedrock
// Anthropic Messages API and converts the response back to an openai.ChatCompletion
// SDK type. This is a Bedrock topology constraint — Anthropic models are not
// available on the OpenAI-compatible Bedrock endpoint.
func (c *Connector) claudeChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	anthropicParams := convert.OpenAIParamsToAnthropicParams(params)
	c.logger.Debug(ctx, "bedrock: claudeChatCompletion start",
		logging.Field{Key: "model", Value: anthropicParams.Model},
		logging.Field{Key: "max_tokens", Value: fmt.Sprintf("%d", anthropicParams.MaxTokens)},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(anthropicParams.Messages))},
	)

	msg, err := c.anthropicClient.Messages.New(ctx, anthropicParams)
	if err != nil {
		c.logger.Error(ctx, "bedrock: claudeChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapAnthropicError(inferencegw.ProviderBedrock, err)
	}

	c.logger.Debug(ctx, "bedrock: claudeChatCompletion done",
		logging.Field{Key: "stop_reason", Value: string(msg.StopReason)},
	)
	return convert.AnthropicMessageToOpenAI(msg), nil
}

// claudeChatCompletionStream routes a Claude/Anthropic model streaming request through
// the Bedrock Anthropic Messages API and converts events to OpenAI chat.completion.chunk
// objects.
func (c *Connector) claudeChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	anthropicParams := convert.OpenAIParamsToAnthropicParams(params)
	c.logger.Debug(ctx, "bedrock: claudeChatCompletionStream start",
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
			c.logger.Error(ctx, "bedrock: claudeChatCompletionStream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "events_before_error", Value: fmt.Sprintf("%d", eventCount)},
			)
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapAnthropicError(inferencegw.ProviderBedrock, err)}
			return
		}
		c.logger.Debug(ctx, "bedrock: claudeChatCompletionStream done",
			logging.Field{Key: "event_count", Value: fmt.Sprintf("%d", eventCount)},
		)
	}()

	return ch, nil
}
