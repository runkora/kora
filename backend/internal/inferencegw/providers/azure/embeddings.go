package azure

import (
	"context"

	openai "github.com/openai/openai-go/v3"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Embeddings calls the Azure OpenAI-compatible embeddings endpoint.
func (c *Connector) Embeddings(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	c.logger.Debug(ctx, "azure: embeddings start",
		logging.Field{Key: "model", Value: params.Model},
	)

	resp, err := c.openaiClient.Embeddings.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "azure: embeddings error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(c.name, err)
	}

	c.logger.Debug(ctx, "azure: embeddings done",
		logging.Field{Key: "count", Value: len(resp.Data)},
	)
	return resp, nil
}
