package vertexai

import (
	"context"
	"fmt"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func (c *Connector) openaiChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	c.logger.Debug(ctx, "vertexai: openaiChatCompletion start",
		logging.Field{Key: "model", Value: req.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(req.Messages))},
	)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, convert.ToOpenAIParams(req))
	if err != nil {
		c.logger.Error(ctx, "vertexai: openaiChatCompletion error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return inferencegw.ChatCompletionResponse{}, convert.MapOpenAIError(inferencegw.ProviderVertexAI, err)
	}

	c.logger.Debug(ctx, "vertexai: openaiChatCompletion done",
		logging.Field{Key: "finish_reason", Value: resp.Choices[0].FinishReason},
	)
	return convert.FromOpenAIResponse(resp), nil
}

func (c *Connector) openaiChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	c.logger.Debug(ctx, "vertexai: openaiChatCompletionStream start",
		logging.Field{Key: "model", Value: req.Model},
		logging.Field{Key: "msg_count", Value: fmt.Sprintf("%d", len(req.Messages))},
	)

	stream := c.openaiClient.Chat.Completions.NewStreaming(ctx, convert.ToOpenAIParams(req))

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		chunkCount := 0
		for stream.Next() {
			chunkCount++
			cur := stream.Current()
			ch <- convert.FromOpenAIChunk(&cur)
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "vertexai: openaiChatCompletionStream stream error",
				logging.Field{Key: "error", Value: err.Error()},
				logging.Field{Key: "chunks_before_error", Value: fmt.Sprintf("%d", chunkCount)},
			)
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapOpenAIError(inferencegw.ProviderVertexAI, err)}
			return
		}
		c.logger.Debug(ctx, "vertexai: openaiChatCompletionStream done",
			logging.Field{Key: "chunk_count", Value: fmt.Sprintf("%d", chunkCount)},
		)
	}()

	return ch, nil
}
