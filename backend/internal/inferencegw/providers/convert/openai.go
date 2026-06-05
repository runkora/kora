// Package convert provides shared type-conversion helpers used when provider
// topology requires translating between SDK wire types (e.g. routing a
// Claude model through the Anthropic Messages API while maintaining an
// OpenAI-compatible external interface).
//
// The golden rule: translation must only happen at provider topology boundaries,
// never at the HTTP handler layer. Gemini/Llama/GPT models pass SDK types
// straight through with zero translation loss.
package convert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// TranslationError is returned by translation functions when the caller
// supplied an invalid request (e.g. unsupported field). Bridges convert
// this to a ConnectorError with Code "INVALID_REQUEST" and Retryable false,
// so the gateway returns a 400 rather than a 503.
type TranslationError struct {
	Message string
}

func (e *TranslationError) Error() string { return e.Message }

// AsTranslationError reports whether err is a *TranslationError and if so
// returns it; otherwise returns nil.
func AsTranslationError(err error) *TranslationError {
	if te, ok := errors.AsType[*TranslationError](err); ok {
		return te
	}
	return nil
}

// WrapTranslationError converts a *TranslationError into a ConnectorError.
// If err is not a *TranslationError it is returned unchanged.
func WrapTranslationError(provider inferencegw.ProviderName, err error) error {
	if te := AsTranslationError(err); te != nil {
		return &inferencegw.ConnectorError{
			Provider:   provider,
			Code:       "INVALID_REQUEST",
			Retryable:  false,
			Underlying: fmt.Errorf("%s", te.Message),
		}
	}
	return err
}

// OpenAI to Anthropic (request)

// OpenAIParamsToAnthropicParams translates native openai-go chat completion params
// to anthropic-sdk-go message params. Used by connectors that must route
// Claude models through the Anthropic Messages API (Bedrock, Vertex AI) while
// maintaining an OpenAI-compatible external interface.
//
// max_tokens, when set, is forwarded verbatim. When the client omits it the
// bridge falls back to the model's documented maximum output (see
// AnthropicModelMaxOutputTokens) rather than imposing an arbitrary cap —
// silent caps truncate long-form coding-agent output and are hard to diagnose.
func OpenAIParamsToAnthropicParams(params openai.ChatCompletionNewParams) anthropic.MessageNewParams {
	maxTokens := params.MaxTokens.Value
	if !params.MaxTokens.Valid() {
		maxTokens, _ = AnthropicModelMaxOutputTokens(params.Model)
	}

	var systemBlocks []anthropic.TextBlockParam
	messages := make([]anthropic.MessageParam, 0, len(params.Messages))

	for _, m := range params.Messages {
		switch {
		case m.OfSystem != nil:
			text := systemMsgText(m.OfSystem)
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: text})

		case m.OfUser != nil:
			blocks := userMsgBlocks(m.OfUser)
			messages = append(messages, anthropic.NewUserMessage(blocks...))

		case m.OfAssistant != nil:
			blocks := assistantMsgBlocks(m.OfAssistant)
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}

		case m.OfTool != nil:
			block := toolResultBlock(m.OfTool)
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	p := anthropic.MessageNewParams{
		Model:     params.Model,
		MaxTokens: maxTokens,
		Messages:  messages,
	}
	if len(systemBlocks) > 0 {
		p.System = systemBlocks
	}
	if params.Temperature.Valid() {
		p.Temperature = anthropic.Float(params.Temperature.Value)
	}
	if len(params.Tools) > 0 {
		p.Tools = convertTools(params.Tools)
		if tc, ok := convertToolChoice(params.ToolChoice); ok {
			p.ToolChoice = tc
		}
	}
	return p
}

// systemMsgText extracts the plain-text content from a system message param.
func systemMsgText(m *openai.ChatCompletionSystemMessageParam) string {
	if m.Content.OfString.Valid() {
		return m.Content.OfString.Value
	}
	var sb strings.Builder
	for _, part := range m.Content.OfArrayOfContentParts {
		sb.WriteString(part.Text)
	}
	return sb.String()
}

// userMsgBlocks converts user message content to Anthropic content blocks.
// image_url parts are converted to Anthropic image blocks (URL or base64).
func userMsgBlocks(m *openai.ChatCompletionUserMessageParam) []anthropic.ContentBlockParamUnion {
	if m.Content.OfString.Valid() {
		return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Content.OfString.Value)}
	}
	var blocks []anthropic.ContentBlockParamUnion
	for _, part := range m.Content.OfArrayOfContentParts {
		if part.OfText != nil {
			blocks = append(blocks, anthropic.NewTextBlock(part.OfText.Text))
		} else if part.OfImageURL != nil {
			blocks = append(blocks, imageURLToAnthropicBlock(part.OfImageURL.ImageURL.URL))
		}
	}
	return blocks
}

// imageURLToAnthropicBlock converts an OpenAI image URL (either a plain URL or a
// data URI) to an Anthropic ContentBlockParamUnion image block.
func imageURLToAnthropicBlock(url string) anthropic.ContentBlockParamUnion {
	// data URIs have the form: data:<mediaType>;base64,<data>
	if strings.HasPrefix(url, "data:") {
		// Strip the "data:" prefix
		rest := url[5:]
		before, after, ok := strings.Cut(rest, ";")
		if !ok {
			// Malformed data URI — fall back to URL block
			return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url})
		}
		mediaType := before
		remainder := after
		// Expect "base64,<data>"
		const prefix = "base64,"
		if !strings.HasPrefix(remainder, prefix) {
			return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url})
		}
		encoded := remainder[len(prefix):]
		return anthropic.NewImageBlockBase64(mediaType, encoded)
	}
	return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url})
}

// assistantMsgBlocks converts assistant message content to Anthropic content blocks.
func assistantMsgBlocks(m *openai.ChatCompletionAssistantMessageParam) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	// Text content
	if m.Content.OfString.Valid() && m.Content.OfString.Value != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content.OfString.Value))
	}
	for _, part := range m.Content.OfArrayOfContentParts {
		if part.OfText != nil && part.OfText.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(part.OfText.Text))
		}
	}

	// Tool calls
	for _, tc := range m.ToolCalls {
		var input any = map[string]any{}
		if tc.Function.Arguments != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err == nil {
				input = parsed
			}
			// If arguments is not valid JSON, leave input as empty map rather than
			// forwarding an unparseable string — the Anthropic API requires an object.
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
	}

	return blocks
}

// toolResultBlock converts a tool message param to an Anthropic tool_result block.
func toolResultBlock(m *openai.ChatCompletionToolMessageParam) anthropic.ContentBlockParamUnion {
	var content string
	if m.Content.OfString.Valid() {
		content = m.Content.OfString.Value
	} else {
		var sb strings.Builder
		for _, part := range m.Content.OfArrayOfContentParts {
			sb.WriteString(part.Text)
		}
		content = sb.String()
	}
	return anthropic.NewToolResultBlock(m.ToolCallID, content, false)
}

// convertTools translates OpenAI tool definitions to Anthropic tool params.
func convertTools(tools []openai.ChatCompletionToolParam) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		fn := t.Function
		var desc string
		if fn.Description.Valid() {
			desc = fn.Description.Value
		}
		schema := openAISchemaToAnthropic(fn.Parameters)
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        fn.Name,
				Description: anthropic.String(desc),
				InputSchema: schema,
			},
		})
	}
	return out
}

// openAISchemaToAnthropic converts an OpenAI function parameters map (a full JSON
// Schema object: {"type":"object","properties":{...},"required":[...]}) into an
// anthropic.ToolInputSchemaParam by unpacking the top-level keys into the typed
// struct fields. Unknown top-level keys are forwarded via ExtraFields so that
// constructs like $defs survive the translation.
func openAISchemaToAnthropic(params map[string]any) anthropic.ToolInputSchemaParam {
	if params == nil {
		return anthropic.ToolInputSchemaParam{}
	}

	schema := anthropic.ToolInputSchemaParam{}

	if props, ok := params["properties"]; ok {
		schema.Properties = props
	}

	// "required" arrives as []any when decoded from JSON; normalise to []string.
	if req, ok := params["required"]; ok {
		switch v := req.(type) {
		case []string:
			schema.Required = v
		case []any:
			strs := make([]string, 0, len(v))
			for _, r := range v {
				if s, ok := r.(string); ok {
					strs = append(strs, s)
				}
			}
			schema.Required = strs
		}
	}

	// Forward any remaining top-level keys (e.g. $defs, $ref, description)
	// as extra JSON fields so they survive the translation.
	skip := map[string]bool{"type": true, "properties": true, "required": true}
	for k, v := range params {
		if skip[k] {
			continue
		}
		if schema.ExtraFields == nil {
			schema.ExtraFields = make(map[string]any)
		}
		schema.ExtraFields[k] = v
	}

	return schema
}

// convertToolChoice translates an OpenAI tool_choice to Anthropic ToolChoiceUnionParam.
// Returns (zero, false) when no tool choice is set.
func convertToolChoice(tc openai.ChatCompletionToolChoiceOptionUnionParam) (anthropic.ToolChoiceUnionParam, bool) {
	if tc.OfChatCompletionNamedToolChoice != nil {
		return anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: tc.OfChatCompletionNamedToolChoice.Function.Name,
			},
		}, true
	}
	if tc.OfAuto.Valid() {
		switch tc.OfAuto.Value {
		case "auto":
			return anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{},
			}, true
		case "required":
			return anthropic.ToolChoiceUnionParam{
				OfAny: &anthropic.ToolChoiceAnyParam{},
			}, true
		case "none":
			return anthropic.ToolChoiceUnionParam{}, false
		}
	}
	return anthropic.ToolChoiceUnionParam{}, false
}

// Anthropic to OpenAI (response)

// AnthropicMessageToOpenAI converts a native anthropic.Message to an
// openai.ChatCompletion. Used by connectors routing Claude models through
// the Anthropic Messages API while returning an OpenAI-compatible response.
// Tool use blocks in the Anthropic response are mapped to OpenAI tool_calls.
//
// createdAt is the Unix timestamp (seconds) at which the provider call was
// initiated — callers should capture time.Now().Unix() immediately before
// calling the Anthropic API so the field is accurate rather than reflecting
// the conversion time.
func AnthropicMessageToOpenAI(msg *anthropic.Message, createdAt int64) *openai.ChatCompletion {
	var textContent strings.Builder
	var toolCalls []openai.ChatCompletionMessageToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textContent.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openai.ChatCompletionMessageToolCallFunction{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	message := openai.ChatCompletionMessage{
		Role: "assistant",
	}
	if text := textContent.String(); text != "" {
		message.Content = text
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	return &openai.ChatCompletion{
		ID:      msg.ID,
		Object:  "chat.completion",
		Model:   msg.Model,
		Created: createdAt,
		Choices: []openai.ChatCompletionChoice{
			{
				Index:        0,
				FinishReason: mapAnthropicStopReason(msg.StopReason),
				Message:      message,
			},
		},
		Usage: openai.CompletionUsage{
			PromptTokens:     msg.Usage.InputTokens,
			CompletionTokens: msg.Usage.OutputTokens,
			TotalTokens:      msg.Usage.InputTokens + msg.Usage.OutputTokens,
		},
	}
}

// Anthropic streaming to OpenAI streaming

// AnthropicStreamState tracks inter-event state needed to convert a sequence of
// Anthropic SSE events into a sequence of OpenAI chat.completion.chunk objects.
// Create one per stream with NewAnthropicStreamState.
type AnthropicStreamState struct {
	MessageID   string
	Model       string
	toolCallIdx map[int64]int64 // Anthropic content-block index -> OpenAI tool_calls slot index
	nextTCIdx   int64
}

// NewAnthropicStreamState initializes an AnthropicStreamState for a new stream.
func NewAnthropicStreamState() *AnthropicStreamState {
	return &AnthropicStreamState{toolCallIdx: make(map[int64]int64)}
}

// AnthropicEventToOpenAIChunk converts one Anthropic SSE event to an OpenAI
// chat.completion.chunk. Returns (chunk, true) when a chunk should be forwarded;
// (nil, false) for events that carry no client-visible data (e.g. content_block_stop).
//
// Tool call streaming: content_block_start events with type="tool_use" emit the
// tool call header (id, name). Subsequent input_json_delta events emit argument
// fragments. The caller must process events in order.
func AnthropicEventToOpenAIChunk(event anthropic.MessageStreamEventUnion, s *AnthropicStreamState) (*openai.ChatCompletionChunk, bool) {
	switch event.Type {
	case "message_start":
		s.MessageID = event.Message.ID
		s.Model = event.Message.Model
		return &openai.ChatCompletionChunk{
			ID:      s.MessageID,
			Object:  "chat.completion.chunk",
			Model:   s.Model,
			Choices: []openai.ChatCompletionChunkChoice{},
			Usage: openai.CompletionUsage{
				PromptTokens: event.Message.Usage.InputTokens,
			},
		}, true

	case "content_block_start":
		if event.ContentBlock.Type != "tool_use" {
			return nil, false
		}
		// Assign a stable slot index for this tool call within the choices delta.
		tcIdx := s.nextTCIdx
		s.toolCallIdx[event.Index] = tcIdx
		s.nextTCIdx++
		return &openai.ChatCompletionChunk{
			ID:    s.MessageID,
			Model: s.Model,
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Index: 0,
					Delta: openai.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: tcIdx,
								ID:    event.ContentBlock.ID,
								Type:  "function",
								Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Name:      event.ContentBlock.Name,
									Arguments: "",
								},
							},
						},
					},
				},
			},
		}, true

	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
			return &openai.ChatCompletionChunk{
				ID:    s.MessageID,
				Model: s.Model,
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: openai.ChatCompletionChunkChoiceDelta{
							Role:    "assistant",
							Content: event.Delta.Text,
						},
					},
				},
			}, true

		case "input_json_delta":
			tcIdx, ok := s.toolCallIdx[event.Index]
			if !ok {
				return nil, false
			}
			return &openai.ChatCompletionChunk{
				ID:    s.MessageID,
				Model: s.Model,
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: openai.ChatCompletionChunkChoiceDelta{
							ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
								{
									Index: tcIdx,
									Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
										Arguments: event.Delta.PartialJSON,
									},
								},
							},
						},
					},
				},
			}, true
		}

	case "message_delta":
		return &openai.ChatCompletionChunk{
			ID:    s.MessageID,
			Model: s.Model,
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Index:        0,
					FinishReason: mapAnthropicStopReason(event.Delta.StopReason),
					Delta:        openai.ChatCompletionChunkChoiceDelta{},
				},
			},
			Usage: openai.CompletionUsage{
				CompletionTokens: event.Usage.OutputTokens,
			},
		}, true
	}

	return nil, false
}

// mapAnthropicStopReason translates Anthropic stop reasons to OpenAI finish_reason values.
func mapAnthropicStopReason[T ~string](reason T) string {
	switch string(reason) {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

// Error mapping

// MapOpenAIError converts an openai-go error to a ConnectorError.
func MapOpenAIError(provider inferencegw.ProviderName, err error) error {
	if apiErr, ok := errors.AsType[*openai.Error](err); ok {
		code, retryable := classifyOpenAIStatus(apiErr.StatusCode)
		return &inferencegw.ConnectorError{
			Provider:        provider,
			Code:            code,
			Retryable:       retryable,
			HTTPStatus:      apiErr.StatusCode,
			Underlying:      fmt.Errorf("status %d: %s", apiErr.StatusCode, apiErr.Message),
			ProviderMessage: apiErr.Message,
		}
	}
	// Context cancellation means the caller gave up (client disconnect, upstream
	// timeout). Retrying is pointless — the context is already done.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &inferencegw.ConnectorError{
			Provider:   provider,
			Code:       "CANCELED",
			Retryable:  false,
			Underlying: err,
		}
	}
	return &inferencegw.ConnectorError{
		Provider:   provider,
		Code:       "UNKNOWN",
		Retryable:  true,
		Underlying: err,
	}
}

func classifyOpenAIStatus(status int) (code string, retryable bool) {
	switch status {
	case http.StatusTooManyRequests:
		return "THROTTLED", true
	case http.StatusRequestEntityTooLarge:
		return "CONTEXT_WINDOW_EXCEEDED", false
	case http.StatusBadRequest:
		return "INVALID_REQUEST", false
	case http.StatusUnauthorized, http.StatusForbidden:
		return "AUTH_FAILED", false
	case http.StatusNotFound:
		return "MODEL_NOT_FOUND", false
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return "PROVIDER_UNAVAILABLE", true
	default:
		return "PROVIDER_ERROR", status >= 500
	}
}

// MapAnthropicError converts an anthropic-sdk-go error to a ConnectorError.
func MapAnthropicError(provider inferencegw.ProviderName, err error) error {
	if apiErr, ok := errors.AsType[*anthropic.Error](err); ok {
		code, retryable := classifyAnthropicStatus(apiErr.StatusCode)
		return &inferencegw.ConnectorError{
			Provider:        provider,
			Code:            code,
			Retryable:       retryable,
			HTTPStatus:      apiErr.StatusCode,
			Underlying:      fmt.Errorf("status %d: %s", apiErr.StatusCode, apiErr.Error()),
			ProviderMessage: extractAnthropicMessage(apiErr.RawJSON()),
		}
	}
	// Context cancellation means the caller gave up (client disconnect, upstream
	// timeout). Retrying is pointless — the context is already done.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &inferencegw.ConnectorError{
			Provider:   provider,
			Code:       "CANCELED",
			Retryable:  false,
			Underlying: err,
		}
	}
	return &inferencegw.ConnectorError{
		Provider:   provider,
		Code:       "UNKNOWN",
		Retryable:  true,
		Underlying: err,
	}
}

// extractAnthropicMessage parses the raw Anthropic API error body and returns
// the inner error message. Returns "" if the body is not parseable or empty.
func extractAnthropicMessage(raw string) string {
	if raw == "" {
		return ""
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err == nil {
		return body.Error.Message
	}
	return ""
}

func classifyAnthropicStatus(status int) (code string, retryable bool) {
	switch status {
	case http.StatusTooManyRequests:
		return "THROTTLED", true
	case http.StatusBadRequest:
		return "INVALID_REQUEST", false
	case http.StatusUnauthorized, http.StatusForbidden:
		return "AUTH_FAILED", false
	case http.StatusNotFound:
		return "MODEL_NOT_FOUND", false
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return "PROVIDER_UNAVAILABLE", true
	default:
		return "PROVIDER_ERROR", status >= 500
	}
}
