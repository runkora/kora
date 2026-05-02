package convert

import (
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

// ResponsesParamsToChatCompletionParams translates a Responses API request to an
// OpenAI ChatCompletionNewParams. Used by the ChatCompletionsResponsesBridge to
// route models through Chat Completions when the client is using the Responses
// API wire format.
//
// Limitations:
//   - PreviousResponseID is not supported; inline conversation history in Input.
//   - Input items of types other than easy_input_message / message are skipped.
//   - Only function tools are translated; built-in tools (file_search, web_search,
//     etc.) are silently dropped.
func ResponsesParamsToChatCompletionParams(params responses.ResponseNewParams) (openai.ChatCompletionNewParams, error) {
	if params.PreviousResponseID.Valid() {
		return openai.ChatCompletionNewParams{}, &TranslationError{
			Message: "previous_response_id is not supported via the Chat Completions bridge; " +
				"inline conversation history in the Input field instead",
		}
	}

	var msgs []openai.ChatCompletionMessageParamUnion

	if params.Instructions.Valid() {
		msgs = append(msgs, openai.SystemMessage(params.Instructions.Value))
	}

	inputMsgs, err := responsesInputToChatMessages(params.Input)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	msgs = append(msgs, inputMsgs...)

	out := openai.ChatCompletionNewParams{
		Model:    params.Model,
		Messages: msgs,
	}

	if params.MaxOutputTokens.Valid() {
		out.MaxTokens = openai.Int(params.MaxOutputTokens.Value)
	}
	if params.Temperature.Valid() {
		out.Temperature = openai.Float(params.Temperature.Value)
	}
	if params.TopP.Valid() {
		out.TopP = openai.Float(params.TopP.Value)
	}
	if len(params.Tools) > 0 {
		out.Tools = convertResponsesToolsToChatCompletion(params.Tools)
	}

	return out, nil
}

func responsesInputToChatMessages(input responses.ResponseNewParamsInputUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
	if input.OfString.Valid() {
		return []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(input.OfString.Value),
		}, nil
	}
	if len(input.OfInputItemList) > 0 {
		return convertResponsesItemsToChatMessages(input.OfInputItemList)
	}
	return nil, nil
}

func convertResponsesItemsToChatMessages(items []responses.ResponseInputItemUnionParam) ([]openai.ChatCompletionMessageParamUnion, error) {
	var msgs []openai.ChatCompletionMessageParamUnion
	for _, item := range items {
		switch {
		case item.OfMessage != nil:
			msg := convertEasyMessageToChat(item.OfMessage)
			if msg != nil {
				msgs = append(msgs, *msg)
			}
		case item.OfInputMessage != nil:
			msg := convertInputMessageParamToChat(item.OfInputMessage)
			if msg != nil {
				msgs = append(msgs, *msg)
			}
			// Other item types (tool calls, tool outputs, etc.) are skipped.
		}
	}
	return msgs, nil
}

func convertEasyMessageToChat(m *responses.EasyInputMessageParam) *openai.ChatCompletionMessageParamUnion {
	text := extractEasyMessageText(m)
	switch m.Role {
	case responses.EasyInputMessageRoleUser:
		msg := openai.UserMessage(text)
		return &msg
	case responses.EasyInputMessageRoleSystem, responses.EasyInputMessageRoleDeveloper:
		msg := openai.SystemMessage(text)
		return &msg
	case responses.EasyInputMessageRoleAssistant:
		msg := openai.AssistantMessage(text)
		return &msg
	}
	return nil
}

func convertInputMessageParamToChat(m *responses.ResponseInputItemMessageParam) *openai.ChatCompletionMessageParamUnion {
	text := extractContentListText(m.Content)
	switch m.Role {
	case "user":
		msg := openai.UserMessage(text)
		return &msg
	case "system", "developer":
		msg := openai.SystemMessage(text)
		return &msg
	case "assistant":
		msg := openai.AssistantMessage(text)
		return &msg
	}
	return nil
}

// convertResponsesToolsToChatCompletion translates Responses API function tools
// to ChatCompletion tool params. Built-in tools (file_search, web_search, etc.)
// are silently dropped — they are not supported via Chat Completions.
func convertResponsesToolsToChatCompletion(tools []responses.ToolUnionParam) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		if t.OfFunction == nil {
			continue
		}
		fn := t.OfFunction
		def := openai.FunctionDefinitionParam{
			Name:       fn.Name,
			Parameters: fn.Parameters,
		}
		if fn.Description.Valid() {
			def.Description = openai.String(fn.Description.Value)
		}
		out = append(out, openai.ChatCompletionToolParam{Function: def})
	}
	return out
}

// ChatCompletionToResponsesResponse converts an openai.ChatCompletion to a
// responses.Response. Used by the ChatCompletionsResponsesBridge to return
// Chat Completions model responses in the Responses API wire format.
func ChatCompletionToResponsesResponse(completion *openai.ChatCompletion) *responses.Response {
	var outputItems []responses.ResponseOutputItemUnion

	if len(completion.Choices) > 0 {
		choice := completion.Choices[0]
		msg := choice.Message

		msgItem := responses.ResponseOutputItemUnion{
			ID:     completion.ID,
			Type:   "message",
			Status: "completed",
			Role:   constant.Assistant("assistant"),
		}
		if msg.Content != "" {
			msgItem.Content = []responses.ResponseOutputMessageContentUnion{
				{Type: "output_text", Text: msg.Content},
			}
		}
		outputItems = append(outputItems, msgItem)

		for _, tc := range msg.ToolCalls {
			outputItems = append(outputItems, responses.ResponseOutputItemUnion{
				ID:        tc.ID,
				Type:      "function_call",
				Status:    "completed",
				Name:      tc.Function.Name,
				CallID:    tc.ID,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	status := responses.ResponseStatus("completed")
	if len(completion.Choices) > 0 && completion.Choices[0].FinishReason == "length" {
		status = "incomplete"
	}

	return &responses.Response{
		ID:     completion.ID,
		Object: constant.Response("response"),
		Model:  completion.Model,
		Status: status,
		Output: outputItems,
		Usage: responses.ResponseUsage{
			InputTokens:  completion.Usage.PromptTokens,
			OutputTokens: completion.Usage.CompletionTokens,
			TotalTokens:  completion.Usage.TotalTokens,
		},
	}
}

// ChatCompletionsStreamState holds inter-event state for translating a
// sequence of OpenAI ChatCompletionChunk events into Responses API stream events.
// Create one per stream with NewChatCompletionsStreamState.
type ChatCompletionsStreamState struct {
	CompletionID string
	Model        string
	textBuf      strings.Builder
	seqNum       int64
	started      bool
}

// NewChatCompletionsStreamState initializes state for a new stream.
func NewChatCompletionsStreamState() *ChatCompletionsStreamState {
	return &ChatCompletionsStreamState{}
}

func (s *ChatCompletionsStreamState) nextSeq() int64 {
	s.seqNum++
	return s.seqNum
}

func (s *ChatCompletionsStreamState) partialResponse() responses.Response {
	return responses.Response{
		ID:     s.CompletionID,
		Object: constant.Response("response"),
		Model:  s.Model,
		Status: "in_progress",
	}
}

// ChatCompletionChunkToResponsesEvents translates one OpenAI ChatCompletionChunk
// into zero or more Responses API stream events. The caller must process chunks
// in order; state carries context across chunks within the same stream.
//
// Only text content is streamed. Tool call deltas are accumulated internally
// and appear in the final response.completed event.
func ChatCompletionChunkToResponsesEvents(chunk openai.ChatCompletionChunk, s *ChatCompletionsStreamState) []responses.ResponseStreamEventUnion {
	var events []responses.ResponseStreamEventUnion

	// Capture ID and model from first chunk that carries them.
	if !s.started && chunk.ID != "" {
		s.CompletionID = chunk.ID
		s.Model = chunk.Model
		s.started = true
		r := s.partialResponse()
		msgItem := responses.ResponseOutputItemUnion{
			ID:     s.CompletionID,
			Type:   "message",
			Status: "in_progress",
			Role:   constant.Assistant("assistant"),
		}
		events = append(events,
			responses.ResponseStreamEventUnion{Type: "response.created", SequenceNumber: s.nextSeq(), Response: r},
			responses.ResponseStreamEventUnion{Type: "response.in_progress", SequenceNumber: s.nextSeq(), Response: r},
			responses.ResponseStreamEventUnion{
				Type:           "response.output_item.added",
				SequenceNumber: s.nextSeq(),
				OutputIndex:    0,
				Item:           msgItem,
			},
			responses.ResponseStreamEventUnion{
				Type:           "response.content_part.added",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.CompletionID,
				OutputIndex:    0,
				ContentIndex:   0,
			},
		)
	}

	// Emit text deltas.
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			s.textBuf.WriteString(choice.Delta.Content)
			events = append(events, responses.ResponseStreamEventUnion{
				Type:           "response.output_text.delta",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.CompletionID,
				OutputIndex:    0,
				ContentIndex:   0,
				Text:           choice.Delta.Content,
			})
		}

		// Close out the stream on finish.
		if choice.FinishReason != "" {
			fullText := s.textBuf.String()
			status := responses.ResponseStatus("completed")
			if choice.FinishReason == "length" {
				status = "incomplete"
			}
			completedMsg := responses.ResponseOutputItemUnion{
				ID:     s.CompletionID,
				Type:   "message",
				Status: "completed",
				Role:   constant.Assistant("assistant"),
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: fullText},
				},
			}
			completedResp := responses.Response{
				ID:     s.CompletionID,
				Object: constant.Response("response"),
				Model:  s.Model,
				Status: status,
				Output: []responses.ResponseOutputItemUnion{completedMsg},
			}
			events = append(events,
				responses.ResponseStreamEventUnion{
					Type:           "response.output_text.done",
					SequenceNumber: s.nextSeq(),
					ItemID:         s.CompletionID,
					OutputIndex:    0,
					ContentIndex:   0,
					Text:           fullText,
				},
				responses.ResponseStreamEventUnion{
					Type:           "response.content_part.done",
					SequenceNumber: s.nextSeq(),
					ItemID:         s.CompletionID,
					OutputIndex:    0,
					ContentIndex:   0,
				},
				responses.ResponseStreamEventUnion{
					Type:           "response.output_item.done",
					SequenceNumber: s.nextSeq(),
					OutputIndex:    0,
					Item:           completedMsg,
				},
				responses.ResponseStreamEventUnion{
					Type:           "response.completed",
					SequenceNumber: s.nextSeq(),
					Response:       completedResp,
				},
			)
		}
	}

	return events
}
