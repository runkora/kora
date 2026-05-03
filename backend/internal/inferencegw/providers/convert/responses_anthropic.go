package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

const blockTypeToolUse = "tool_use"

// ResponsesParamsToAnthropicParams translates a Responses API request to an
// Anthropic MessageNewParams. Used by the AnthropicResponsesBridge to route
// Claude models through the Anthropic Messages API when the client is using
// the Responses API wire format.
//
// Translation contract (see ADR-0045 "Bridge Limitations"):
//   - PreviousResponseID is not supported and is rejected as INVALID_REQUEST.
//   - max_output_tokens, when set, is forwarded verbatim. When the client
//     omits it, the bridge falls back to the model's documented maximum
//     output (see AnthropicModelMaxOutputTokens) rather than imposing an
//     arbitrary cap — coding agents routinely emit long structured outputs
//     and a silent cap is a debugging trap.
//   - system / developer role messages are hoisted into params.System (the
//     Anthropic native location) rather than appended as user turns.
//   - function-call and function-call-output input items are translated to
//     Anthropic tool_use / tool_result blocks so multi-turn agentic flows
//     (e.g. Codex) work end-to-end.
//   - Custom function tools (params.Tools.OfFunction) are translated; built-in
//     Responses tools (file_search, web_search, code_interpreter, mcp, …) are
//     dropped — Anthropic does not host them.
//   - Input items of types other than easy_input_message / message /
//     function_call / function_call_output are skipped.
func ResponsesParamsToAnthropicParams(params responses.ResponseNewParams) (anthropic.MessageNewParams, error) {
	if params.PreviousResponseID.Valid() {
		return anthropic.MessageNewParams{}, &TranslationError{
			Message: "previous_response_id is not supported via the Anthropic bridge; " +
				"inline conversation history in the Input field instead",
		}
	}

	maxTokens := params.MaxOutputTokens.Value
	if !params.MaxOutputTokens.Valid() {
		maxTokens, _ = AnthropicModelMaxOutputTokens(params.Model)
	}

	msgs, systemFromInput, err := responsesInputToAnthropicMessages(params.Input)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	// Instructions always precede any system blocks extracted from the input.
	var system []anthropic.TextBlockParam
	if params.Instructions.Valid() {
		system = append(system, anthropic.TextBlockParam{Text: params.Instructions.Value})
	}
	system = append(system, systemFromInput...)

	out := anthropic.MessageNewParams{
		Model:      params.Model,
		MaxTokens:  maxTokens,
		Messages:   msgs,
		System:     system,
		Tools:      convertResponsesToolsToAnthropic(params.Tools),
		ToolChoice: anthropicToolChoice(params.ToolChoice),
	}
	if params.Temperature.Valid() {
		out.Temperature = anthropic.Float(params.Temperature.Value)
	}
	if params.TopP.Valid() {
		out.TopP = anthropic.Float(params.TopP.Value)
	}
	return out, nil
}

// anthropicToolChoice translates a Responses API tool_choice to the Anthropic
// ToolChoiceUnionParam. "required" maps to Anthropic's "any" — both mean "must
// call some tool". Hosted/MCP variants are not representable and are ignored.
func anthropicToolChoice(tc responses.ResponseNewParamsToolChoiceUnion) anthropic.ToolChoiceUnionParam {
	if fn := tc.OfFunctionTool; fn != nil {
		return anthropic.ToolChoiceUnionParam{OfTool: &anthropic.ToolChoiceToolParam{Name: fn.Name}}
	}
	switch tc.OfToolChoiceMode.Value {
	case responses.ToolChoiceOptionsNone:
		return anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
	case responses.ToolChoiceOptionsRequired:
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
	case responses.ToolChoiceOptionsAuto:
		return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
	}
	return anthropic.ToolChoiceUnionParam{}
}

// responsesInputToAnthropicMessages translates the Responses API Input union
// into Anthropic MessageParams plus any system blocks extracted from
// system/developer-role input items. System blocks are returned separately so
// the caller can merge them with the system blocks derived from
// params.Instructions.
func responsesInputToAnthropicMessages(input responses.ResponseNewParamsInputUnion) ([]anthropic.MessageParam, []anthropic.TextBlockParam, error) {
	if input.OfString.Valid() {
		return []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(input.OfString.Value)),
		}, nil, nil
	}
	if len(input.OfInputItemList) > 0 {
		return convertResponsesItems(input.OfInputItemList)
	}
	return nil, nil, nil
}

// convertResponsesItems walks the Responses input item list and produces an
// Anthropic message sequence. Adjacent assistant function calls and the
// following user function-call-outputs are coalesced into single assistant /
// user messages so the Anthropic API sees the canonical
// (assistant{tool_use}, user{tool_result}, …) shape.
func convertResponsesItems(items []responses.ResponseInputItemUnionParam) ([]anthropic.MessageParam, []anthropic.TextBlockParam, error) {
	var (
		msgs           []anthropic.MessageParam
		systemBlocks   []anthropic.TextBlockParam
		pendingTool    []anthropic.ContentBlockParamUnion // assistant tool_use blocks awaiting flush
		pendingResults []anthropic.ContentBlockParamUnion // user tool_result blocks awaiting flush
	)
	flushPendingTool := func() {
		if len(pendingTool) > 0 {
			msgs = append(msgs, anthropic.NewAssistantMessage(pendingTool...))
			pendingTool = nil
		}
	}
	flushPendingResults := func() {
		if len(pendingResults) > 0 {
			msgs = append(msgs, anthropic.NewUserMessage(pendingResults...))
			pendingResults = nil
		}
	}

	for _, item := range items {
		switch {
		case item.OfMessage != nil:
			flushPendingTool()
			flushPendingResults()
			if sys := easyMessageSystemText(item.OfMessage); sys != "" {
				systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: sys})
				continue
			}
			if msg := convertEasyMessage(item.OfMessage); msg != nil {
				msgs = append(msgs, *msg)
			}

		case item.OfInputMessage != nil:
			flushPendingTool()
			flushPendingResults()
			if sys := inputMessageSystemText(item.OfInputMessage); sys != "" {
				systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: sys})
				continue
			}
			if msg := convertInputMessageParam(item.OfInputMessage); msg != nil {
				msgs = append(msgs, *msg)
			}

		case item.OfFunctionCall != nil:
			flushPendingResults()
			fc := item.OfFunctionCall
			var input any = map[string]any{}
			if fc.Arguments != "" {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(fc.Arguments), &parsed); err != nil {
					return nil, nil, &TranslationError{
						Message: fmt.Sprintf("function_call %q has invalid JSON arguments: %s", fc.Name, err.Error()),
					}
				}
				input = parsed
			}
			pendingTool = append(pendingTool, anthropic.NewToolUseBlock(fc.CallID, input, fc.Name))

		case item.OfFunctionCallOutput != nil:
			flushPendingTool()
			fco := item.OfFunctionCallOutput
			pendingResults = append(pendingResults, anthropic.NewToolResultBlock(fco.CallID, fco.Output, false))
		}
	}
	flushPendingTool()
	flushPendingResults()
	return msgs, systemBlocks, nil
}

// easyMessageSystemText returns the text content of an EasyInputMessageParam
// when its role is system or developer; otherwise returns "".
func easyMessageSystemText(m *responses.EasyInputMessageParam) string {
	if m.Role != responses.EasyInputMessageRoleSystem && m.Role != responses.EasyInputMessageRoleDeveloper {
		return ""
	}
	return extractEasyMessageText(m)
}

// inputMessageSystemText returns the text content of a
// ResponseInputItemMessageParam when its role is system or developer.
func inputMessageSystemText(m *responses.ResponseInputItemMessageParam) string {
	if m.Role != "system" && m.Role != "developer" {
		return ""
	}
	return extractContentListText(m.Content)
}

// convertEasyMessage converts a non-system EasyInputMessageParam to an
// Anthropic MessageParam. System / developer messages are handled separately
// by the caller (hoisted into params.System).
func convertEasyMessage(m *responses.EasyInputMessageParam) *anthropic.MessageParam {
	text := extractEasyMessageText(m)
	switch m.Role {
	case responses.EasyInputMessageRoleUser:
		msg := anthropic.NewUserMessage(anthropic.NewTextBlock(text))
		return &msg
	case responses.EasyInputMessageRoleAssistant:
		msg := anthropic.NewAssistantMessage(anthropic.NewTextBlock(text))
		return &msg
	}
	return nil
}

// convertInputMessageParam converts a non-system ResponseInputItemMessageParam
// to an Anthropic MessageParam.
func convertInputMessageParam(m *responses.ResponseInputItemMessageParam) *anthropic.MessageParam {
	text := extractContentListText(m.Content)
	switch m.Role {
	case "user":
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

// convertResponsesToolsToAnthropic translates Responses API function tools to
// Anthropic ToolUnionParams. Built-in tools (file_search, web_search,
// code_interpreter, mcp, …) are silently dropped — Anthropic does not host them.
func convertResponsesToolsToAnthropic(tools []responses.ToolUnionParam) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.OfFunction == nil {
			continue
		}
		fn := t.OfFunction
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

// AnthropicMessageToResponsesResponse converts a native anthropic.Message to a
// responses.Response. Used by the AnthropicResponsesBridge to return Claude
// responses in the Responses API wire format. Tool-use blocks are emitted as
// function_call output items so the streaming and non-streaming paths produce
// equivalent shapes for tool calls.
func AnthropicMessageToResponsesResponse(msg *anthropic.Message) *responses.Response {
	var textBuf strings.Builder
	var toolItems []responses.ResponseOutputItemUnion

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
		case blockTypeToolUse:
			toolItems = append(toolItems, responses.ResponseOutputItemUnion{
				ID:        block.ID,
				Type:      "function_call",
				Status:    "completed",
				Name:      block.Name,
				CallID:    block.ID,
				Arguments: string(block.Input),
			})
		}
	}

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
	output := append([]responses.ResponseOutputItemUnion{msgItem}, toolItems...)

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
			InputTokens: msg.Usage.InputTokens,
			InputTokensDetails: responses.ResponseUsageInputTokensDetails{
				CachedTokens: msg.Usage.CacheReadInputTokens,
			},
			OutputTokens: msg.Usage.OutputTokens,
			TotalTokens:  msg.Usage.InputTokens + msg.Usage.OutputTokens,
		},
	}
}

// AnthropicResponsesStreamState holds inter-event state for translating a
// sequence of Anthropic SSE events into Responses API stream events.
// Create one per stream with NewAnthropicResponsesStreamState.
type AnthropicResponsesStreamState struct {
	MessageID       string
	Model           string
	textBuf         strings.Builder
	toolUses        map[int64]*pendingToolUse // anthropic block index → accumulated tool_use
	toolOrder       []int64                   // block indices in the order they appeared
	seqNum          int64
	inputTokens     int64
	outputTokens    int64
	cacheReadTokens int64
	stopReason      string
}

// pendingToolUse accumulates a tool_use block across content_block_start /
// content_block_delta events.
type pendingToolUse struct {
	id       string
	name     string
	inputBuf strings.Builder
}

// NewAnthropicResponsesStreamState initializes state for a new stream.
func NewAnthropicResponsesStreamState() *AnthropicResponsesStreamState {
	return &AnthropicResponsesStreamState{toolUses: map[int64]*pendingToolUse{}}
}

func (s *AnthropicResponsesStreamState) nextSeq() int64 {
	s.seqNum++
	return s.seqNum
}

func (s *AnthropicResponsesStreamState) partialResponse() responses.Response {
	return responses.Response{
		ID:     s.MessageID,
		Object: constant.Response("response"),
		Model:  s.Model,
		Status: "in_progress",
	}
}

// completedToolItems produces a stable-ordered slice of function_call output
// items from the accumulated tool_use blocks.
func (s *AnthropicResponsesStreamState) completedToolItems() []responses.ResponseOutputItemUnion {
	out := make([]responses.ResponseOutputItemUnion, 0, len(s.toolOrder))
	for _, idx := range s.toolOrder {
		t := s.toolUses[idx]
		args := t.inputBuf.String()
		if args == "" {
			args = "{}"
		}
		out = append(out, responses.ResponseOutputItemUnion{
			ID:        t.id,
			Type:      "function_call",
			Status:    "completed",
			Name:      t.name,
			CallID:    t.id,
			Arguments: args,
		})
	}
	return out
}

// AnthropicEventToResponsesEvents translates one Anthropic stream event into
// zero or more Responses API stream events. The caller must process events in
// order; state carries context across events within the same stream.
//
// Tool-use blocks are accumulated across events and emitted in the terminal
// response.completed event so the streaming and non-streaming bridge outputs
// agree on shape.
func AnthropicEventToResponsesEvents(event anthropic.MessageStreamEventUnion, s *AnthropicResponsesStreamState) []responses.ResponseStreamEventUnion {
	switch event.Type {
	case "message_start":
		s.MessageID = event.Message.ID
		s.Model = event.Message.Model
		s.inputTokens = event.Message.Usage.InputTokens
		s.cacheReadTokens = event.Message.Usage.CacheReadInputTokens
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

	case "content_block_start":
		if event.ContentBlock.Type == blockTypeToolUse {
			if _, ok := s.toolUses[event.Index]; !ok {
				s.toolUses[event.Index] = &pendingToolUse{
					id:   event.ContentBlock.ID,
					name: event.ContentBlock.Name,
				}
				s.toolOrder = append(s.toolOrder, event.Index)
			}
		}
		return nil

	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
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
		case "input_json_delta":
			if t, ok := s.toolUses[event.Index]; ok {
				t.inputBuf.WriteString(event.Delta.PartialJSON)
			}
			return nil
		}
		return nil

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
		}
		if fullText != "" {
			completedMsg.Content = []responses.ResponseOutputMessageContentUnion{
				{Type: "output_text", Text: fullText},
			}
		}
		toolItems := s.completedToolItems()
		status := responses.ResponseStatus("completed")
		if s.stopReason == "max_tokens" {
			status = "incomplete"
		}
		output := append([]responses.ResponseOutputItemUnion{completedMsg}, toolItems...)
		completedResp := responses.Response{
			ID:     s.MessageID,
			Object: constant.Response("response"),
			Model:  s.Model,
			Status: status,
			Output: output,
			Usage: responses.ResponseUsage{
				InputTokens: s.inputTokens,
				InputTokensDetails: responses.ResponseUsageInputTokensDetails{
					CachedTokens: s.cacheReadTokens,
				},
				OutputTokens: s.outputTokens,
				TotalTokens:  s.inputTokens + s.outputTokens,
			},
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
