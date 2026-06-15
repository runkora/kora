package convert

import (
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
)

// ResponsesParamsToChatCompletionParams translates a Responses API request to an
// OpenAI ChatCompletionNewParams. Used by the ChatCompletionsResponsesBridge to
// route models through Chat Completions when the client is using the Responses
// API wire format.
//
// Translation contract (see docs/adr/2026-05-03-embeddings-and-responses-api-endpoints.md "Bridge Limitations"):
//   - PreviousResponseID is rejected as INVALID_REQUEST.
//   - system / developer role messages are emitted as system Chat Completions
//     messages.
//   - function-call and function-call-output input items are translated to
//     assistant tool-call messages and tool-role messages so multi-turn agentic
//     flows work end-to-end.
//   - Custom function tools (params.Tools.OfFunction) are translated; built-in
//     Responses tools (file_search, web_search, code_interpreter, mcp, …) are
//     dropped — Chat Completions does not host them in the same shape.
//   - Input items of types other than easy_input_message / message /
//     function_call / function_call_output are skipped.
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
	msgs = append(msgs, responsesInputToChatMessages(params.Input)...)

	// MaxOutputTokens, Temperature, TopP, and ParallelToolCalls are all
	// param.Opt[T] from the same openai-go/packages/param package, so they
	// assign directly — no value extraction needed.
	return openai.ChatCompletionNewParams{
		Model:             params.Model,
		Messages:          msgs,
		MaxTokens:         params.MaxOutputTokens,
		Temperature:       params.Temperature,
		TopP:              params.TopP,
		Tools:             convertResponsesToolsToChatCompletion(params.Tools),
		ToolChoice:        chatCompletionToolChoice(params.ToolChoice),
		ParallelToolCalls: params.ParallelToolCalls,
		ResponseFormat:    chatCompletionResponseFormat(params.Text.Format),
	}, nil
}

// chatCompletionToolChoice translates a Responses API tool_choice to the Chat
// Completions union. Hosted and MCP tool choices are not representable in Chat
// Completions and are ignored (they have no function equivalent).
func chatCompletionToolChoice(tc responses.ResponseNewParamsToolChoiceUnion) openai.ChatCompletionToolChoiceOptionUnionParam {
	if tc.OfToolChoiceMode.Valid() {
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: openai.String(string(tc.OfToolChoiceMode.Value)),
		}
	}
	if fn := tc.OfFunctionTool; fn != nil {
		return openai.ChatCompletionToolChoiceOptionUnionParam{
			OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{Name: fn.Name},
			},
		}
	}
	return openai.ChatCompletionToolChoiceOptionUnionParam{}
}

// chatCompletionResponseFormat translates a Responses API text.format to the
// Chat Completions response_format union. The Strict and Description fields on
// both JSON schema types are param.Opt[T] from the same package and assign directly.
func chatCompletionResponseFormat(f responses.ResponseFormatTextConfigUnionParam) openai.ChatCompletionNewParamsResponseFormatUnion {
	if js := f.OfJSONSchema; js != nil {
		return openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        js.Name,
					Schema:      js.Schema,
					Strict:      js.Strict,
					Description: js.Description,
				},
			},
		}
	}
	if f.OfJSONObject != nil {
		return openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}}
	}
	if f.OfText != nil {
		return openai.ChatCompletionNewParamsResponseFormatUnion{OfText: &shared.ResponseFormatTextParam{}}
	}
	return openai.ChatCompletionNewParamsResponseFormatUnion{}
}

func responsesInputToChatMessages(input responses.ResponseNewParamsInputUnion) []openai.ChatCompletionMessageParamUnion {
	if input.OfString.Valid() {
		return []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(input.OfString.Value),
		}
	}
	if len(input.OfInputItemList) > 0 {
		return convertResponsesItemsToChatMessages(input.OfInputItemList)
	}
	return nil
}

// convertResponsesItemsToChatMessages walks the Responses input item list and
// produces a Chat Completions message sequence. Adjacent function_call items
// are coalesced into a single assistant message carrying multiple tool_calls,
// matching the canonical Chat Completions tool-use shape.
func convertResponsesItemsToChatMessages(items []responses.ResponseInputItemUnionParam) []openai.ChatCompletionMessageParamUnion {
	var (
		msgs        []openai.ChatCompletionMessageParamUnion
		pendingTool []openai.ChatCompletionMessageToolCallUnionParam
	)
	flushPendingTool := func() {
		if len(pendingTool) == 0 {
			return
		}
		assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: pendingTool}
		msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		pendingTool = nil
	}

	for _, item := range items {
		switch {
		case item.OfMessage != nil:
			flushPendingTool()
			if msg := convertEasyMessageToChat(item.OfMessage); msg != nil {
				msgs = append(msgs, *msg)
			}

		case item.OfInputMessage != nil:
			flushPendingTool()
			if msg := convertInputMessageParamToChat(item.OfInputMessage); msg != nil {
				msgs = append(msgs, *msg)
			}

		case item.OfFunctionCall != nil:
			fc := item.OfFunctionCall
			pendingTool = append(pendingTool, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: fc.CallID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      fc.Name,
						Arguments: fc.Arguments,
					},
				},
			})

		case item.OfFunctionCallOutput != nil:
			flushPendingTool()
			fco := item.OfFunctionCallOutput
			// Output is a string|[]content union in v3. Orbit only produces string
			// outputs; access .OfString.Value directly.
			// TODO: handle array variant (images/files) if a future client sends content-array outputs.
			msgs = append(msgs, openai.ToolMessage(fco.Output.OfString.Value, fco.CallID))
		}
	}
	flushPendingTool()
	return msgs
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
func convertResponsesToolsToChatCompletion(tools []responses.ToolUnionParam) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
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
		out = append(out, openai.ChatCompletionFunctionTool(def))
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

		var content []responses.ResponseOutputMessageContentUnion
		if msg.Content != "" {
			content = []responses.ResponseOutputMessageContentUnion{newOutputTextContent(msg.Content)}
		}
		outputItems = append(outputItems, newMessageItem(completion.ID, "completed", content))

		for _, tc := range msg.ToolCalls {
			outputItems = append(outputItems, newFunctionCallItem(
				tc.ID, tc.ID, tc.Function.Name, tc.Function.Arguments,
			))
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

// pendingChatToolCall accumulates a streaming tool_call across deltas.
type pendingChatToolCall struct {
	id      string
	name    string
	argsBuf strings.Builder
}

// ChatCompletionsStreamState holds inter-event state for translating a
// sequence of OpenAI ChatCompletionChunk events into Responses API stream events.
// Create one per stream with NewChatCompletionsStreamState.
type ChatCompletionsStreamState struct {
	CompletionID         string
	Model                string
	textBuf              strings.Builder
	toolCalls            map[int64]*pendingChatToolCall
	toolOrder            []int64
	seqNum               int64
	started              bool
	pendingCompletedResp *responses.Response // deferred until the usage chunk arrives
}

// NewChatCompletionsStreamState initializes state for a new stream.
func NewChatCompletionsStreamState() *ChatCompletionsStreamState {
	return &ChatCompletionsStreamState{toolCalls: map[int64]*pendingChatToolCall{}}
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

func (s *ChatCompletionsStreamState) completedToolItems() []responses.ResponseOutputItemUnion {
	out := make([]responses.ResponseOutputItemUnion, 0, len(s.toolOrder))
	for _, idx := range s.toolOrder {
		t := s.toolCalls[idx]
		args := t.argsBuf.String()
		if args == "" {
			args = "{}"
		}
		out = append(out, newFunctionCallItem(t.id, t.id, t.name, args))
	}
	return out
}

// FlushPendingCompletion emits the deferred response.completed event, populating
// usage from the provided token counts. Must be called after the upstream stream
// ends to guarantee the terminal event is always sent even when the provider
// does not return a usage chunk (e.g. stream_options not supported).
func (s *ChatCompletionsStreamState) FlushPendingCompletion(inputTokens, outputTokens, totalTokens int64) *responses.ResponseStreamEventUnion {
	if s.pendingCompletedResp == nil {
		return nil
	}
	s.pendingCompletedResp.Usage = responses.ResponseUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
	ev := responses.ResponseStreamEventUnion{
		Type:           "response.completed",
		SequenceNumber: s.nextSeq(),
		Response:       *s.pendingCompletedResp,
	}
	s.pendingCompletedResp = nil
	return &ev
}

// finishEvents builds the terminal pre-completion events on FinishReason and
// stores the pending completed response for deferred emission by FlushPendingCompletion.
// response.completed itself is NOT included — it is deferred until usage is available.
func (s *ChatCompletionsStreamState) finishEvents(finishReason string) []responses.ResponseStreamEventUnion {
	fullText := s.textBuf.String()
	toolItems := s.completedToolItems()
	status := responses.ResponseStatus("completed")
	if finishReason == "length" {
		status = "incomplete"
	}
	var content []responses.ResponseOutputMessageContentUnion
	if fullText != "" {
		content = []responses.ResponseOutputMessageContentUnion{newOutputTextContent(fullText)}
	}
	completedMsg := newMessageItem(s.CompletionID, "completed", content)
	output := append([]responses.ResponseOutputItemUnion{completedMsg}, toolItems...)
	s.pendingCompletedResp = &responses.Response{
		ID:     s.CompletionID,
		Object: constant.Response("response"),
		Model:  s.Model,
		Status: status,
		Output: output,
	}
	return []responses.ResponseStreamEventUnion{
		{
			Type:           "response.output_text.done",
			SequenceNumber: s.nextSeq(),
			ItemID:         s.CompletionID,
			OutputIndex:    0,
			ContentIndex:   0,
			Text:           fullText,
		},
		{
			Type:           "response.content_part.done",
			SequenceNumber: s.nextSeq(),
			ItemID:         s.CompletionID,
			OutputIndex:    0,
			ContentIndex:   0,
		},
		{
			Type:           "response.output_item.done",
			SequenceNumber: s.nextSeq(),
			OutputIndex:    0,
			Item:           completedMsg,
		},
	}
}

// ChatCompletionChunkToResponsesEvents translates one OpenAI ChatCompletionChunk
// into zero or more Responses API stream events. The caller must process chunks
// in order; state carries context across chunks within the same stream.
//
// Tool-call deltas are accumulated and emitted in the terminal response.completed
// event, matching the non-streaming bridge's output shape.
func ChatCompletionChunkToResponsesEvents(chunk openai.ChatCompletionChunk, s *ChatCompletionsStreamState) []responses.ResponseStreamEventUnion {
	var events []responses.ResponseStreamEventUnion

	if !s.started && chunk.ID != "" {
		s.CompletionID = chunk.ID
		s.Model = chunk.Model
		s.started = true
		r := s.partialResponse()
		msgItem := newMessageItem(s.CompletionID, "in_progress", nil)
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

	for _, choice := range chunk.Choices {
		// Accumulate text deltas and stream them through.
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

		// Accumulate tool-call deltas. They are emitted in the final
		// response.completed event so the streaming and non-streaming bridge
		// outputs agree on shape for tool calls.
		for _, tc := range choice.Delta.ToolCalls {
			pc, ok := s.toolCalls[tc.Index]
			if !ok {
				pc = &pendingChatToolCall{}
				s.toolCalls[tc.Index] = pc
				s.toolOrder = append(s.toolOrder, tc.Index)
			}
			if tc.ID != "" {
				pc.id = tc.ID
			}
			if tc.Function.Name != "" {
				pc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				pc.argsBuf.WriteString(tc.Function.Arguments)
			}
		}

		if choice.FinishReason != "" {
			events = append(events, s.finishEvents(choice.FinishReason)...)
		}
	}

	// Handle the trailing usage chunk sent by providers that support
	// stream_options.include_usage. This chunk has empty Choices and
	// non-zero Usage; flush the deferred response.completed with real token counts.
	if chunk.Usage.TotalTokens > 0 && s.pendingCompletedResp != nil {
		if ev := s.FlushPendingCompletion(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens); ev != nil {
			events = append(events, *ev)
		}
	}

	return events
}
