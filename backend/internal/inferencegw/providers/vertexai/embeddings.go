package vertexai

import (
	"context"

	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Embeddings calls the Vertex AI OpenAI-compatible embeddings endpoint.
func (c *Connector) Embeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	c.logger.Debug(ctx, "vertexai: embeddings start",
		logging.Field{Key: "model", Value: params.Model},
	)

	resp, err := c.openaiClient.Embeddings.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "vertexai: embeddings error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(c.name, err)
	}

	c.logger.Debug(ctx, "vertexai: embeddings done",
		logging.Field{Key: "count", Value: len(resp.Data)},
	)
	return resp, nil
}
