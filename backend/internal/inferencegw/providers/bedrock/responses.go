package bedrock

import (
	"context"

	"github.com/openai/openai-go/responses"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Responses calls the Responses API on the appropriate Bedrock endpoint.
// Mantle models (openai.* prefix) are routed to the Bedrock Mantle endpoint.
// Anthropic/Claude models are translated via the AnthropicResponsesBridge.
// Other models fall through to the standard OpenAI-compatible endpoint, which
// will fail and trigger the router's failover to the next target.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	c.logger.Debug(ctx, "bedrock: responses start",
		logging.Field{Key: "model", Value: params.Model},
	)

	switch c.dispatchBackend(params.Model) {
	case backendMantle:
		resp, err := c.mantleClient.Responses.New(ctx, params)
		if err != nil {
			c.logger.Error(ctx, "bedrock: responses error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
		}
		return resp, nil
	case backendAnthropic:
		return c.anthropicBridge.Execute(ctx, params)
	default:
		return c.openAIBridge.Execute(ctx, params)
	}
}

// ResponsesStream opens a streaming responses request via the appropriate Bedrock endpoint.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	c.logger.Debug(ctx, "bedrock: responsesStream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	switch c.dispatchBackend(params.Model) {
	case backendMantle:
		stream := c.mantleClient.Responses.NewStreaming(ctx, params)
		ch := make(chan inferencegw.ResponsesStreamResult, 32)
		go func() {
			defer close(ch)
			defer stream.Close()
			for stream.Next() {
				ch <- inferencegw.ResponsesStreamResult{Event: stream.Current()}
			}
			if err := stream.Err(); err != nil {
				c.logger.Error(ctx, "bedrock: responsesStream error",
					logging.Field{Key: "error", Value: err.Error()},
				)
				ch <- inferencegw.ResponsesStreamResult{Err: convert.MapOpenAIError(inferencegw.ProviderBedrock, err)}
			}
		}()
		return ch, nil
	case backendAnthropic:
		return c.anthropicBridge.ExecuteStream(ctx, params)
	default:
		return c.openAIBridge.ExecuteStream(ctx, params)
	}
}
