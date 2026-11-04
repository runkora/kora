package bedrock

import (
	"context"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// stripMantleUnsupportedInputItems removes input item types that Mantle's
// Responses API doesn't accept. Mantle supports basic message types but
// rejects output-specific types (reasoning) and messages with empty content
// that OpenAI allows clients to include as stateless multi-turn context.
func stripMantleUnsupportedInputItems(params responses.ResponseNewParams) responses.ResponseNewParams {
	items := params.Input.OfInputItemList
	if len(items) == 0 {
		return params
	}
	filtered := make(responses.ResponseInputParam, 0, len(items))
	for _, item := range items {
		// Drop reasoning items only when they carry no actual content.
		// A real reasoning model (e.g. o3) produces items with a non-empty ID
		// and either a summary or encrypted_content — those are worth preserving.
		// Non-reasoning models (e.g. gpt-oss-120b) produce empty stubs that
		// Mantle rejects and that contain nothing useful anyway.
		if item.OfReasoning != nil {
			r := item.OfReasoning
			emptyReasoning := r.ID == "" && len(r.Summary) == 0 && !r.EncryptedContent.Valid()
			if emptyReasoning {
				continue
			}
		}
		omitEmptyItemID(item)
		// All type="message" items deserialize as OfMessage (first discriminator).
		// Mantle rejects messages with no content, so patch empty ones to use an
		// empty string rather than dropping them — this preserves the assistant
		// turn marker in conversation history, which matters for long agentic sessions.
		if item.OfMessage != nil {
			c := item.OfMessage.Content
			if param.IsOmitted(c.OfString) && len(c.OfInputItemContentList) == 0 {
				item.OfMessage.Content.OfString = param.NewOpt("")
			}
		}
		filtered = append(filtered, item)
	}
	params.Input.OfInputItemList = filtered
	return params
}

// omitEmptyItemID drops "id" from an item whose SDK type requires one the client omitted (Codex
// omits ids when store is false). Re-encoded as "id":"", Mantle rejects two such items as duplicates.
func omitEmptyItemID(item responses.ResponseInputItemUnionParam) {
	if id := item.GetID(); id == nil || *id != "" {
		return
	}
	if v := requiredIDVariant(item); v != nil {
		v.SetExtraFields(map[string]any{"id": param.Omit})
	}
}

// requiredIDVariant returns the item's variant when its SDK param type marks the id required.
func requiredIDVariant(item responses.ResponseInputItemUnionParam) interface{ SetExtraFields(map[string]any) } {
	switch {
	case item.OfFileSearchCall != nil:
		return item.OfFileSearchCall
	case item.OfComputerCall != nil:
		return item.OfComputerCall
	case item.OfWebSearchCall != nil:
		return item.OfWebSearchCall
	case item.OfReasoning != nil:
		return item.OfReasoning
	case item.OfImageGenerationCall != nil:
		return item.OfImageGenerationCall
	case item.OfCodeInterpreterCall != nil:
		return item.OfCodeInterpreterCall
	case item.OfLocalShellCall != nil:
		return item.OfLocalShellCall
	case item.OfLocalShellCallOutput != nil:
		return item.OfLocalShellCallOutput
	case item.OfMcpListTools != nil:
		return item.OfMcpListTools
	case item.OfMcpApprovalRequest != nil:
		return item.OfMcpApprovalRequest
	case item.OfMcpCall != nil:
		return item.OfMcpCall
	case item.OfItemReference != nil:
		return item.OfItemReference
	case item.OfProgram != nil:
		return item.OfProgram
	case item.OfProgramOutput != nil:
		return item.OfProgramOutput
	}
	return nil
}

// Responses calls the Responses API on the appropriate Bedrock endpoint.
// Mantle models (openai.* prefix) are routed to the Bedrock Mantle endpoint.
// Anthropic/Claude models are translated via the AnthropicResponsesBridge.
// Other models fall through to the standard OpenAI-compatible endpoint, which
// will fail and trigger the router's failover to the next target.
func (c *Connector) Responses(ctx context.Context, params responses.ResponseNewParams) (*inferencegw.ResponsesResult, error) {
	c.logger.Debug(ctx, "bedrock: responses start",
		logging.Field{Key: "model", Value: params.Model},
	)

	switch c.dispatchBackend(params.Model) {
	case backendMantleOpenAIV1:
		// GPT-5.x and xAI Grok use /openai/v1/responses — a separate client with a different base path.
		resp, err := c.mantleOpenAIV1Client.Responses.New(ctx, stripMantleUnsupportedInputItems(params))
		if err != nil {
			c.logger.Error(ctx, "bedrock: responses error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
		}
		return &inferencegw.ResponsesResult{Response: resp}, nil
	case backendMantle:
		resp, err := c.mantleClient.Responses.New(ctx, stripMantleUnsupportedInputItems(params))
		if err != nil {
			c.logger.Error(ctx, "bedrock: responses error",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil, convert.MapOpenAIError(inferencegw.ProviderBedrock, err)
		}
		return &inferencegw.ResponsesResult{Response: resp}, nil
	case backendAnthropic:
		c.logger.Debug(ctx, "bedrock: responses dispatch via anthropic bridge",
			logging.Field{Key: "model", Value: params.Model},
		)
		return c.anthropicBridge.Execute(ctx, params)
	default:
		c.logger.Debug(ctx, "bedrock: responses dispatch via chat completions bridge",
			logging.Field{Key: "model", Value: params.Model},
		)
		return c.openAIBridge.Execute(ctx, params)
	}
}

// ResponsesStream opens a streaming responses request via the appropriate Bedrock endpoint.
func (c *Connector) ResponsesStream(ctx context.Context, params responses.ResponseNewParams) (<-chan inferencegw.ResponsesStreamResult, error) {
	c.logger.Debug(ctx, "bedrock: responsesStream start",
		logging.Field{Key: "model", Value: params.Model},
	)

	switch c.dispatchBackend(params.Model) {
	case backendMantleOpenAIV1:
		// GPT-5.x and xAI Grok use /openai/v1/responses — a separate client with a different base path.
		stream := c.mantleOpenAIV1Client.Responses.NewStreaming(ctx, stripMantleUnsupportedInputItems(params))
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
	case backendMantle:
		stream := c.mantleClient.Responses.NewStreaming(ctx, stripMantleUnsupportedInputItems(params))
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
		c.logger.Debug(ctx, "bedrock: responsesStream dispatch via anthropic bridge",
			logging.Field{Key: "model", Value: params.Model},
		)
		return c.anthropicBridge.ExecuteStream(ctx, params)
	default:
		c.logger.Debug(ctx, "bedrock: responsesStream dispatch via chat completions bridge",
			logging.Field{Key: "model", Value: params.Model},
		)
		return c.openAIBridge.ExecuteStream(ctx, params)
	}
}
