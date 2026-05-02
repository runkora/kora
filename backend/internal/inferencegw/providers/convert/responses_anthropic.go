package convert

import (
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

// ResponsesParamsToAnthropicParams translates a Responses API request to an
// Anthropic MessageNewParams. Used by the AnthropicResponsesBridge to route
// Claude models through the Anthropic Messages API when the client is using
// the Responses API wire format.
//
// Limitations:
//   - PreviousResponseID is not supported; inline conversation history in Input.
//   - Tool definitions in params.Tools are silently ignored (text-only responses).
//   - Input items of types other than easy_input_message / message are skipped.
func ResponsesParamsToAnthropicParams(params responses.ResponseNewParams) (anthropic.MessageNewParams, error) {
	if params.PreviousResponseID.Valid() {
		return anthropic.MessageNewParams{}, &TranslationError{
			Message: "previous_response_id is not supported via the Anthropic bridge; " +
				"inline conversation history in the Input field instead",
		}
	}

	out := anthropic.MessageNewParams{
		Model:     params.Model,
		MaxTokens: 4096,
	}

	if params.Instructions.Valid() {
		out.System = []anthropic.TextBlockParam{{Text: params.Instructions.Value}}
	}
	if params.MaxOutputTokens.Valid() {
		out.MaxTokens = params.MaxOutputTokens.Value
	}
	if params.Temperature.Valid() {
		out.Temperature = anthropic.Float(params.Temperature.Value)
	}
	if params.TopP.Valid() {
		out.TopP = anthropic.Float(params.TopP.Value)
	}

	msgs, err := responsesInputToAnthropicMessages(params.Input)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}
	out.Messages = msgs
	return out, nil
}

func responsesInputToAnthropicMessages(input responses.ResponseNewParamsInputUnion) ([]anthropic.MessageParam, error) {
	if input.OfString.Valid() {
		return []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(input.OfString.Value)),
		}, nil
	}
	if len(input.OfInputItemList) > 0 {
		return convertResponsesItems(input.OfInputItemList)
	}
	return nil, nil
}

func convertResponsesItems(items []responses.ResponseInputItemUnionParam) ([]anthropic.MessageParam, error) {
	var msgs []anthropic.MessageParam
	for _, item := range items {
		switch {
		case item.OfMessage != nil:
			msg := convertEasyMessage(item.OfMessage)
			if msg != nil {
				msgs = append(msgs, *msg)
			}
		case item.OfInputMessage != nil:
			msg := convertInputMessageParam(item.OfInputMessage)
			if msg != nil {
				msgs = append(msgs, *msg)
			}
			// Other item types (tool calls, tool outputs, etc.) are skipped.
		}
	}
	return msgs, nil
}

func convertEasyMessage(m *responses.EasyInputMessageParam) *anthropic.MessageParam {
	text := extractEasyMessageText(m)
	switch m.Role {
	case responses.EasyInputMessageRoleUser, responses.EasyInputMessageRoleSystem, responses.EasyInputMessageRoleDeveloper:
		msg := anthropic.NewUserMessage(anthropic.NewTextBlock(text))
		return &msg
	case responses.EasyInputMessageRoleAssistant:
		msg := anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
		return &msg
	}
	return nil
}

func convertInputMessageParam(m *responses.ResponseInputItemMessageParam) *anthropic.MessageParam {
	text := extractContentListText(m.Content)
	switch m.Role {
	case "user", "system", "developer":
		msg := anthropic.NewUserMessage(anthropic.NewTextBlock(text))
		return &msg
	case "assistant":
		msg := anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
		return &msg
	}
	return nil
}

func extractEasyMessageText(m *responses.EasyInputMessageParam) string {
	if m.Content.OfString.Valid() {
		return m.Content.OfString.Value
	}
	return extractContentListText(m.Content.OfInputItemContentList)
}

func extractContentListText(parts responses.ResponseInputMessageContentListParam) string {
	var sb strings.Builder
	for _, part := range parts {
		if part.OfInputText != nil {
			sb.WriteString(part.OfInputText.Text)
		}
	}
	return sb.String()
}

// AnthropicMessageToResponsesResponse converts a native anthropic.Message to a
// responses.Response. Used by the AnthropicResponsesBridge to return Claude
// responses in the Responses API wire format.
func AnthropicMessageToResponsesResponse(msg *anthropic.Message) *responses.Response {
	var textBuf strings.Builder
	var outputItems []responses.ResponseOutputItemUnion

	// Accumulate text blocks; each tool_use block becomes a function_call item.
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
		case "tool_use":
			outputItems = append(outputItems, responses.ResponseOutputItemUnion{
				ID:        block.ID,
				Type:      "function_call",
				Status:    "completed",
				Name:      block.Name,
				CallID:    block.ID,
				Arguments: string(block.Input),
			})
		}
	}

	// Build a single message output item containing all text.
	msgItem := responses.ResponseOutputItemUnion{
		ID:     msg.ID,
		Type:   "message",
		Status: "completed",
		Role:   constant.Assistant("assistant"),
	}
	if textBuf.Len() > 0 {
		msgItem.Content = []responses.ResponseOutputMessageContentUnion{
			{Type: "output_text", Text: textBuf.String()},
		}
	}
	// Prepend the message item; function call items follow.
	output := append([]responses.ResponseOutputItemUnion{msgItem}, outputItems...)

	status := responses.ResponseStatus("completed")
	if msg.StopReason == "max_tokens" {
		status = "incomplete"
	}

	return &responses.Response{
		ID:     msg.ID,
		Object: constant.Response("response"),
		Model:  msg.Model,
		Status: status,
		Output: output,
		Usage: responses.ResponseUsage{
			InputTokens:  msg.Usage.InputTokens,
			OutputTokens: msg.Usage.OutputTokens,
			TotalTokens:  msg.Usage.InputTokens + msg.Usage.OutputTokens,
		},
	}
}

// AnthropicResponsesStreamState holds inter-event state for translating a
// sequence of Anthropic SSE events into Responses API stream events.
// Create one per stream with NewAnthropicResponsesStreamState.
type AnthropicResponsesStreamState struct {
	MessageID    string
	Model        string
	textBuf      strings.Builder
	seqNum       int64
	outputTokens int64
	stopReason   string
}

// NewAnthropicResponsesStreamState initializes state for a new stream.
func NewAnthropicResponsesStreamState() *AnthropicResponsesStreamState {
	return &AnthropicResponsesStreamState{}
}

func (s *AnthropicResponsesStreamState) nextSeq() int64 {
	s.seqNum++
	return s.seqNum
}

// partialResponse builds an in-progress Response snapshot for emitting in
// response.created / response.in_progress events.
func (s *AnthropicResponsesStreamState) partialResponse() responses.Response {
	return responses.Response{
		ID:     s.MessageID,
		Object: constant.Response("response"),
		Model:  s.Model,
		Status: "in_progress",
	}
}

// AnthropicEventToResponsesEvents translates one Anthropic stream event into
// zero or more Responses API stream events. The caller must process events in
// order; state carries context across events within the same stream.
//
// Only text content is handled in the streaming path. Tool-use blocks are
// omitted from the stream (they appear in the accumulated response on done).
func AnthropicEventToResponsesEvents(event anthropic.MessageStreamEventUnion, s *AnthropicResponsesStreamState) []responses.ResponseStreamEventUnion {
	switch event.Type {
	case "message_start":
		s.MessageID = event.Message.ID
		s.Model = event.Message.Model
		r := s.partialResponse()
		return []responses.ResponseStreamEventUnion{
			{Type: "response.created", SequenceNumber: s.nextSeq(), Response: r},
			{Type: "response.in_progress", SequenceNumber: s.nextSeq(), Response: r},
			{
				Type:           "response.output_item.added",
				SequenceNumber: s.nextSeq(),
				OutputIndex:    0,
				Item: responses.ResponseOutputItemUnion{
					ID:     s.MessageID,
					Type:   "message",
					Status: "in_progress",
					Role:   constant.Assistant("assistant"),
				},
			},
			{
				Type:           "response.content_part.added",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
			},
		}

	case "content_block_delta":
		if event.Delta.Type != "text_delta" {
			return nil
		}
		s.textBuf.WriteString(event.Delta.Text)
		return []responses.ResponseStreamEventUnion{
			{
				Type:           "response.output_text.delta",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
				Text:           event.Delta.Text,
			},
		}

	case "message_delta":
		s.outputTokens = event.Usage.OutputTokens
		s.stopReason = string(event.Delta.StopReason)
		return nil

	case "message_stop":
		fullText := s.textBuf.String()
		completedMsg := responses.ResponseOutputItemUnion{
			ID:     s.MessageID,
			Type:   "message",
			Status: "completed",
			Role:   constant.Assistant("assistant"),
			Content: []responses.ResponseOutputMessageContentUnion{
				{Type: "output_text", Text: fullText},
			},
		}
		status := responses.ResponseStatus("completed")
		if s.stopReason == "max_tokens" {
			status = "incomplete"
		}
		completedResp := responses.Response{
			ID:     s.MessageID,
			Object: constant.Response("response"),
			Model:  s.Model,
			Status: status,
			Output: []responses.ResponseOutputItemUnion{completedMsg},
		}
		return []responses.ResponseStreamEventUnion{
			{
				Type:           "response.output_text.done",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
				Text:           fullText,
			},
			{
				Type:           "response.content_part.done",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
			},
			{
				Type:           "response.output_item.done",
				SequenceNumber: s.nextSeq(),
				OutputIndex:    0,
				Item:           completedMsg,
			},
			{
				Type:           "response.completed",
				SequenceNumber: s.nextSeq(),
				Response:       completedResp,
			},
		}
	}

	return nil
}
