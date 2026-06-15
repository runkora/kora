package azure

import (
	"context"

	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Responses calls the Azure OpenAI-compatible responses endpoint.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	c.logger.Debug(ctx, "azure: responses start",
		logging.Field{Key: "model", Value: params.Model},
	)

	resp, err := c.openaiClient.Responses.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "azure: responses error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(inferencegw.ProviderAzure, err)
	}

	return resp, nil
}

// ResponsesStream opens a streaming responses request via the Azure OpenAI-compatible endpoint.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	c.logger.Debug(ctx, "azure: responsesStream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	stream := c.openaiClient.Responses.NewStreaming(ctx, params)

	ch := make(chan inferencegw.ResponsesStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		for stream.Next() {
			ev := stream.Current()
			ch <- inferencegw.ResponsesStreamResult{Event: ev}
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "azure: responsesStream error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			ch <- inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(inferencegw.ProviderAzure, err)}
		}
	}()

	return ch, nil
}
