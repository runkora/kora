package bedrock

import (
	"context"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

func (c *Connector) openaiChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	resp, err := c.openaiClient.Chat.Completions.New(ctx, convert.ToOpenAIParams(req))
	if err != nil {
		return inferencegw.ChatCompletionResponse{}, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
	}
	return convert.FromOpenAIResponse(resp), nil
}

func (c *Connector) openaiChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	stream := c.openaiClient.Chat.Completions.NewStreaming(ctx, convert.ToOpenAIParams(req))

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		for stream.Next() {
			cur := stream.Current()
			ch <- convert.FromOpenAIChunk(&cur)
		}
		if err := stream.Err(); err != nil {
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapOpenAIError(inferencegw.ProviderBedrock, err)}
		}
	}()

	return ch, nil
}
