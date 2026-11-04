package azure

import (
	"context"
	"fmt"

	openai "github.com/openai/openai-go/v3"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// openaiChatCompletion sends a non-streaming request to Azure OpenAI.
// The SDK wire type is passed through and returned with zero translation
// loss — tool definitions, tool_calls, logprobs, and all other fields are preserved.
func (c *Connector) openaiChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	c.logger.Debug(ctx, "azure: openaiChatCompletion start",
		logging.Field{Key: "model", Value: params.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(params.Messages))},
	)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "azure: openaiChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(c.name, err)
	}

	if len(resp.Choices) > 0 {
		c.logger.Debug(ctx, "azure: openaiChatCompletion done",
			logging.Field{Key: "finish_reason", Value: resp.Choices[0].FinishReason},
		)
	}
	return resp, nil
}

// openaiChatCompletionStream opens a streaming request to Azure OpenAI.
// Raw SDK chunks are forwarded through the channel with no translation.
func (c *Connector) openaiChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	c.logger.Debug(ctx, "azure: openaiChatCompletionStream start",
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
			c.logger.Error(ctx, "azure: openaiChatCompletionStream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "chunks_before_error", Value: fmt.Sprintf("%d", chunkCount)},
			)
			ch <- inferencegw.OpenAIStreamResult{Err: convert.MapOpenAIError(c.name, err)}
			return
		}
		c.logger.Debug(ctx, "azure: openaiChatCompletionStream done",
			logging.Field{Key: "chunk_count", Value: fmt.Sprintf("%d", chunkCount)},
		)
	}()

	return ch, nil
}
