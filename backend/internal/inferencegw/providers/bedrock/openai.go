package bedrock

import (
	"context"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// openaiChatCompletion sends a non-streaming request to Bedrock's
// OpenAI-compatible endpoint. The openaiClient holds a SigV4-signing
// RoundTripper so AWS credentials are applied transparently.
func (c *Connector) openaiChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	params := convert.ToOpenAIParams(req)

	resp, err := c.openaiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "bedrock: openai completion failed", logging.Field{Key: "error", Value: err.Error()})
		return inferencegw.ChatCompletionResponse{}, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
	}

	return convert.FromOpenAIResponse(resp), nil
}

// openaiChatCompletionStream opens a streaming request to Bedrock's
// OpenAI-compatible endpoint, again using the SigV4-signed client.
func (c *Connector) openaiChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	params := convert.ToOpenAIParams(req)

	stream := c.openaiClient.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan inferencegw.ChatCompletionChunk, 32)
	go func() {
		defer close(ch)
		for stream.Next() {
			chunk := stream.Current()
			ch <- convert.FromOpenAIChunk(&chunk)
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "bedrock: openai stream error", logging.Field{Key: "error", Value: err.Error()})
			ch <- inferencegw.ChatCompletionChunk{Err: convert.MapOpenAIError(inferencegw.ProviderBedrock, err)}
		}
	}()

	return ch, nil
}
