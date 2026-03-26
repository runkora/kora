package vertexai

import (
	"context"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

func (c *Connector) anthropicChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	msg, err := c.anthropicClient.Messages.New(ctx, convert.ToAnthropicParams(req))
	if err != nil {
		return inferencegw.ChatCompletionResponse{}, convert.MapAnthropicError(inferencegw.ProviderVertexAI, err)
	}
	return convert.FromAnthropicMessage(msg), nil
}

func (c *Connector) anthropicChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	stream := c.anthropicClient.Messages.NewStreaming(ctx, convert.ToAnthropicParams(req))

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		var accumulated anthropic.Message
		for stream.Next() {
			event := stream.Current()
			if err := accumulated.Accumulate(event); err != nil {
				ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderVertexAI, err)}
				return
			}
			if chunk, ok := convert.AnthropicEventToChunk(event); ok {
				ch <- chunk
			}
		}
		if err := stream.Err(); err != nil {
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapAnthropicError(inferencegw.ProviderVertexAI, err)}
		}
	}()

	return ch, nil
}
