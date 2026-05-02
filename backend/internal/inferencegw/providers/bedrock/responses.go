package bedrock

import (
	"context"

	"github.com/openai/openai-go/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Responses calls the Responses API on the appropriate Bedrock endpoint.
// Mantle models (openai.* prefix) are routed to the Bedrock Mantle endpoint;
// other models fall through to the standard OpenAI-compatible endpoint, which
// will fail and trigger the router's failover to the next target.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	c.logger.Debug(ctx, "bedrock: responses start",
		logging.Field{Key: "model", Value: params.Model},
	)

	client := c.openaiClient
	if c.dispatchBackend(params.Model) == backendMantle {
		client = c.mantleClient
	}

	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		c.logger.Error(ctx, "bedrock: responses error",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
	}

	return resp, nil
}

// ResponsesStream opens a streaming responses request via the appropriate Bedrock endpoint.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	c.logger.Debug(ctx, "bedrock: responsesStream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	client := c.openaiClient
	if c.dispatchBackend(params.Model) == backendMantle {
		client = c.mantleClient
	}

	stream := client.Responses.NewStreaming(ctx, params)

	ch := make(chan inferencegw.ResponsesStreamResult, 32)
	go func() {
		defer close(ch)
		defer stream.Close()
		for stream.Next() {
			ev := stream.Current()
			ch <- inferencegw.ResponsesStreamResult{Event: ev}
		}
		if err := stream.Err(); err != nil {
			c.logger.Error(ctx, "bedrock: responsesStream error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			ch <- inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(inferencegw.ProviderBedrock, err)}
		}
	}()

	return ch, nil
}
