package vertexai

import (
	"context"

	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Responses calls the Vertex AI OpenAI-compatible responses endpoint.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	c.logger.Debug(ctx, "vertexai: responses start",
		logging.Field{Key: "model", Value: params.Model},
	)

	resp, err := c.openaiClient.Responses.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "vertexai: responses error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(c.name, err)
	}

	return &inferencegw.ResponsesResult{Response: resp}, nil
}

// ResponsesStream opens a streaming responses request via the Vertex AI OpenAI-compatible endpoint.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	c.logger.Debug(ctx, "vertexai: responsesStream start",
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
			c.logger.Error(ctx, "vertexai: responsesStream error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			ch <- inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(c.name, err)}
		}
	}()

	return ch, nil
}
