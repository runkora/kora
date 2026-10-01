package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared/constant"
)

const blockTypeToolUse = "tool_use"

// newFunctionCallItem constructs a ResponseOutputItemUnion for a completed
// function_call output item. It round-trips through the SDK's ResponseFunctionToolCall
// type to populate JSON.raw, which is required for the item to serialize correctly
// to wire JSON — direct struct construction produces malformed Arguments output
// because ResponseOutputItemUnion has no MarshalJSON and encoding/json cannot
// handle the inline union sub-fields.
//
// Panics if either marshal step fails: ResponseFunctionToolCall contains only
// plain string fields and a constant, so failure is a runtime fault, not a
// recoverable condition. A silent error here would leave JSON.raw empty and
// reintroduce the wire-format regression this helper exists to prevent.
func newFunctionCallItem(id, callID, name, arguments string) responses.ResponseOutputItemUnion {
	ftc := responses.ResponseFunctionToolCall{
		ID:        id,
		CallID:    callID,
		Name:      name,
		Arguments: arguments,
		Status:    responses.ResponseFunctionToolCallStatusCompleted,
	}
	b, err := json.Marshal(ftc)
	if err != nil {
		panic(fmt.Sprintf("newFunctionCallItem: marshal ResponseFunctionToolCall: %v", err))
	}
	// Strip "namespace":"" — Codex CLI builds ToolName{namespace: Some(""), name: "..."}
	// when namespace is an empty string, which fails the registry lookup (registered
	// tools use namespace: None). Remove the key when empty so Codex gets None.
	var m map[string]json.RawMessage
	err = json.Unmarshal(b, &m)
	if err != nil {
		panic(fmt.Sprintf("newFunctionCallItem: unmarshal for cleanup: %v", err))
	}
	if v, ok := m["namespace"]; ok && (string(v) == `""` || string(v) == "null") {
		delete(m, "namespace")
	}
	cleaned, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("newFunctionCallItem: re-marshal after cleanup: %v", err))
	}
	var item responses.ResponseOutputItemUnion
	err = json.Unmarshal(cleaned, &item)
	if err != nil {
		panic(fmt.Sprintf("newFunctionCallItem: unmarshal into ResponseOutputItemUnion: %v", err))
	}
	return item
}

// newInProgressFunctionCallItem constructs a ResponseOutputItemUnion for an
// in_progress function_call item (used in response.output_item.added events
// before arguments have been accumulated). Same round-trip approach as
// newFunctionCallItem to ensure correct wire serialization.
func newInProgressFunctionCallItem(id, callID, name string) responses.ResponseOutputItemUnion {
	ftc := responses.ResponseFunctionToolCall{
		ID:     id,
		CallID: callID,
		Name:   name,
		Status: responses.ResponseFunctionToolCallStatusInProgress,
	}
	b, err := json.Marshal(ftc)
	if err != nil {
		panic(fmt.Sprintf("newInProgressFunctionCallItem: marshal: %v", err))
	}
	var m map[string]json.RawMessage
	err = json.Unmarshal(b, &m)
	if err != nil {
		panic(fmt.Sprintf("newInProgressFunctionCallItem: unmarshal for cleanup: %v", err))
	}
	if v, ok := m["namespace"]; ok && (string(v) == `""` || string(v) == "null") {
		delete(m, "namespace")
	}
	cleaned, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("newInProgressFunctionCallItem: re-marshal after cleanup: %v", err))
	}
	var item responses.ResponseOutputItemUnion
	err = json.Unmarshal(cleaned, &item)
	if err != nil {
		panic(fmt.Sprintf("newInProgressFunctionCallItem: unmarshal: %v", err))
	}
	return item
}

// newMessageItem constructs a ResponseOutputItemUnion for a message output item.
// It round-trips through the SDK's ResponseOutputMessage type to populate JSON.raw,
// which is required for correct wire-format serialization. ResponseOutputItemUnion
// has no MarshalJSON, so direct struct construction produces extraneous zero-value
// fields that break Codex CLI's parsing of the response.completed event.
//
// content may be nil or empty for in_progress items that have no text yet.
// Panics on marshal failure — plain fields only; failure is a runtime fault.
func newMessageItem(id, status string, content []responses.ResponseOutputMessageContentUnion) responses.ResponseOutputItemUnion {
	msg := responses.ResponseOutputMessage{
		ID:      id,
		Status:  responses.ResponseOutputMessageStatus(status),
		Content: content,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		panic(fmt.Sprintf("newMessageItem: marshal ResponseOutputMessage: %v", err))
	}
	// ResponseOutputMessage always emits "phase":"" and "content":null for
	// zero-value fields. Codex CLI deserializes ResponseItem::Message with
	// phase as Option<MessagePhase> (an enum) — an empty string fails to parse
	// as either None or a valid variant and silently drops the entire item.
	// Strip "phase" when empty. A null "content" becomes [], the shape OpenAI
	// streams on response.output_item.added; Codex drops an item without it and
	// then rejects the text deltas that follow.
	var m map[string]json.RawMessage
	err = json.Unmarshal(b, &m)
	if err != nil {
		panic(fmt.Sprintf("newMessageItem: unmarshal for cleanup: %v", err))
	}
	if v, ok := m["phase"]; ok && (string(v) == `""` || string(v) == "null") {
		delete(m, "phase")
	}
	if v, ok := m["content"]; !ok || string(v) == "null" {
		m["content"] = json.RawMessage("[]")
	}
	cleaned, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("newMessageItem: re-marshal after cleanup: %v", err))
	}
	var item responses.ResponseOutputItemUnion
	if err := json.Unmarshal(cleaned, &item); err != nil {
		panic(fmt.Sprintf("newMessageItem: unmarshal into ResponseOutputItemUnion: %v", err))
	}
	return item
}

// newOutputTextContent constructs a ResponseOutputMessageContentUnion for a plain
// output_text content block, round-tripping through ResponseOutputText to populate
// JSON.raw for correct wire serialization.
func newOutputTextContent(text string) responses.ResponseOutputMessageContentUnion {
	ot := responses.ResponseOutputText{
		Text:        text,
		Annotations: []responses.ResponseOutputTextAnnotationUnion{},
	}
	b, err := json.Marshal(ot)
	if err != nil {
		panic(fmt.Sprintf("newOutputTextContent: marshal ResponseOutputText: %v", err))
	}
	var cu responses.ResponseOutputMessageContentUnion
	if err := json.Unmarshal(b, &cu); err != nil {
		panic(fmt.Sprintf("newOutputTextContent: unmarshal into ResponseOutputMessageContentUnion: %v", err))
	}
	return cu
}

// ResponsesParamsToAnthropicParams translates a Responses API request to an
// Anthropic MessageNewParams. Used by the AnthropicResponsesBridge to route
// Claude models through the Anthropic Messages API when the client is using
// the Responses API wire format.
//
// Translation contract (see docs/adr/2026-05-03-embeddings-and-responses-api-endpoints.md "Bridge Limitations"):
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
	applyPromptCaching(&out)
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
			// Output is a string|[]content union in v3. Orbit only produces string
			// outputs; reject the content-array variant explicitly to avoid silently
			// forwarding an empty string to the model.
			// TODO: handle array variant (images/files) if a future client sends content-array outputs.
			if !fco.Output.OfString.Valid() {
				return nil, nil, fmt.Errorf("responses: function_call_output item %q has unsupported content-array output; only string outputs are supported", fco.CallID)
			}
			pendingResults = append(pendingResults, anthropic.NewToolResultBlock(fco.CallID.Value, fco.Output.OfString.Value, false))
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
	blocks := extractEasyMessageBlocks(m)
	if len(blocks) == 0 {
		return nil
	}
	switch m.Role {
	case responses.EasyInputMessageRoleUser:
		msg := anthropic.NewUserMessage(blocks...)
		return &msg
	case responses.EasyInputMessageRoleAssistant:
		msg := anthropic.NewAssistantMessage(blocks...)
		return &msg
	}
	return nil
}

// convertInputMessageParam converts a non-system ResponseInputItemMessageParam
// to an Anthropic MessageParam.
func convertInputMessageParam(m *responses.ResponseInputItemMessageParam) *anthropic.MessageParam {
	blocks := extractContentBlocks(m.Content)
	if len(blocks) == 0 {
		return nil
	}
	switch m.Role {
	case "user":
		msg := anthropic.NewUserMessage(blocks...)
		return &msg
	case "assistant":
		msg := anthropic.NewAssistantMessage(blocks...)
		return &msg
	}
	return nil
}

// extractEasyMessageBlocks returns Anthropic content blocks for a
// EasyInputMessageParam, preserving text and image parts.
func extractEasyMessageBlocks(m *responses.EasyInputMessageParam) []anthropic.ContentBlockParamUnion {
	if m.Content.OfString.Valid() {
		return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Content.OfString.Value)}
	}
	return extractContentBlocks(m.Content.OfInputItemContentList)
}

// extractContentBlocks converts a Responses API content list to Anthropic
// content blocks. Text parts become text blocks; image parts become image
// blocks (URL or base64 data URI). File-ID-only images and file attachments
// have no Anthropic equivalent and are silently skipped.
func extractContentBlocks(parts responses.ResponseInputMessageContentListParam) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.OfInputText != nil:
			blocks = append(blocks, anthropic.NewTextBlock(part.OfInputText.Text))
		case part.OfInputImage != nil:
			if b := responseImageToAnthropicBlock(part.OfInputImage); b != nil {
				blocks = append(blocks, *b)
			}
		}
	}
	return blocks
}

// responseImageToAnthropicBlock converts a Responses API image input to an
// Anthropic image block. Returns nil for file-ID-only images (no Anthropic
// equivalent) and for non-base64 data URIs.
func responseImageToAnthropicBlock(img *responses.ResponseInputImageParam) *anthropic.ContentBlockParamUnion {
	if !img.ImageURL.Valid() {
		return nil
	}
	url := img.ImageURL.Value
	if strings.HasPrefix(url, "data:") {
		mediaType, data, ok := parseDataURI(url)
		if !ok {
			return nil
		}
		b := anthropic.NewImageBlockBase64(mediaType, data)
		return &b
	}
	b := anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url})
	return &b
}

// parseDataURI parses a data URI of the form data:[mediatype];base64,[data].
// Only base64-encoded payloads are supported; non-base64 URIs return ok=false.
func parseDataURI(s string) (mediaType, data string, ok bool) {
	s = strings.TrimPrefix(s, "data:")
	meta, data, found := strings.Cut(s, ",")
	if !found {
		return "", "", false
	}
	mimeType, encoding, hasSemi := strings.Cut(meta, ";")
	if !hasSemi || encoding != "base64" {
		return "", "", false
	}
	return mimeType, data, true
}

// extractEasyMessageText returns the plain text of an EasyInputMessageParam.
// Used for system/developer role extraction and the Chat Completions bridge.
func extractEasyMessageText(m *responses.EasyInputMessageParam) string {
	if m.Content.OfString.Valid() {
		return m.Content.OfString.Value
	}
	return extractContentListText(m.Content.OfInputItemContentList)
}

// extractContentListText concatenates only the input_text parts of a content
// list. Used for system/developer role extraction where only text is valid,
// and by the Chat Completions bridge.
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

// responsesUsage builds Responses usage from Anthropic's counts. Anthropic
// reports uncached input, cache reads and cache writes as three separate
// totals; the Responses dialect follows OpenAI, where input_tokens covers every
// prompt token and input_tokens_details.cached_tokens names the cached subset
// of it. Responses has no field for cache writes, and those tokens are counted
// as ordinary input rather than dropped: the model read them either way, and a
// turn that writes its whole prompt to cache would otherwise report a handful
// of tokens. This is the client-facing shape only. Billing takes the write
// count from ResponsesResult.CacheCreationTokens and prices it separately.
func responsesUsage(uncachedInput, cacheRead, cacheWrite, output int64) responses.ResponseUsage {
	input := uncachedInput + cacheRead + cacheWrite
	return responses.ResponseUsage{
		InputTokens: input,
		InputTokensDetails: responses.ResponseUsageInputTokensDetails{
			CachedTokens: cacheRead,
		},
		OutputTokens: output,
		TotalTokens:  input + output,
	}
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
			toolItems = append(toolItems, newFunctionCallItem(
				block.ID, block.ID, block.Name, string(block.Input),
			))
		}
	}

	var output []responses.ResponseOutputItemUnion
	if textBuf.Len() > 0 {
		output = append(output, newMessageItem(msg.ID, "completed", []responses.ResponseOutputMessageContentUnion{
			newOutputTextContent(textBuf.String()),
		}))
	}
	output = append(output, toolItems...)

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
		Usage:  responsesUsage(msg.Usage.InputTokens, msg.Usage.CacheReadInputTokens, msg.Usage.CacheCreationInputTokens, msg.Usage.OutputTokens),
	}
}

// AnthropicResponsesStreamState holds inter-event state for translating a
// sequence of Anthropic SSE events into Responses API stream events.
// Create one per stream with NewAnthropicResponsesStreamState.
type AnthropicResponsesStreamState struct {
	MessageID         string
	Model             string
	textBuf           strings.Builder
	toolUses          map[int64]*pendingToolUse // anthropic block index → accumulated tool_use
	toolOrder         []int64                   // block indices in the order they appeared
	seqNum            int64
	inputTokens       int64
	outputTokens      int64
	cacheReadTokens   int64
	cacheWriteTokens  int64
	stopReason        string
	sentMsgOutputItem bool // whether response.output_item.added (message) was emitted
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

// CacheWriteTokens reports what the turn wrote to cache. Responses usage has no
// field for it, so the bridge hands it to billing beside the stream.
func (s *AnthropicResponsesStreamState) CacheWriteTokens() int64 {
	return s.cacheWriteTokens
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
		out = append(out, newFunctionCallItem(t.id, t.id, t.name, args))
	}
	return out
}

// AnthropicEventToResponsesEvents translates one Anthropic stream event into
// zero or more Responses API stream events. The caller must process events in
// order; state carries context across events within the same stream.
//
// Event mapping mirrors the OpenAI Responses API spec:
//   - message output items are emitted lazily — only when a text block appears.
//   - each tool_use block emits its own output_item.added / arguments.delta /
//     arguments.done / output_item.done events inline.
func AnthropicEventToResponsesEvents(event anthropic.MessageStreamEventUnion, s *AnthropicResponsesStreamState) []responses.ResponseStreamEventUnion {
	switch event.Type {
	case "message_start":
		s.MessageID = event.Message.ID
		s.Model = event.Message.Model
		s.inputTokens = event.Message.Usage.InputTokens
		s.cacheReadTokens = event.Message.Usage.CacheReadInputTokens
		s.cacheWriteTokens = event.Message.Usage.CacheCreationInputTokens
		r := s.partialResponse()
		return []responses.ResponseStreamEventUnion{
			{Type: "response.created", SequenceNumber: s.nextSeq(), Response: r},
			{Type: "response.in_progress", SequenceNumber: s.nextSeq(), Response: r},
		}
	case "content_block_start":
		return s.handleContentBlockStart(event)
	case "content_block_delta":
		return s.handleContentBlockDelta(event)
	case "content_block_stop":
		return s.handleContentBlockStop(event)
	case "message_delta":
		s.outputTokens = event.Usage.OutputTokens
		s.stopReason = string(event.Delta.StopReason)
		return nil
	case "message_stop":
		return s.handleMessageStop()
	}
	return nil
}

func (s *AnthropicResponsesStreamState) handleContentBlockStart(event anthropic.MessageStreamEventUnion) []responses.ResponseStreamEventUnion {
	switch event.ContentBlock.Type {
	case "text":
		if !s.sentMsgOutputItem {
			s.sentMsgOutputItem = true
			return []responses.ResponseStreamEventUnion{
				{
					Type:           "response.output_item.added",
					SequenceNumber: s.nextSeq(),
					OutputIndex:    0,
					Item:           newMessageItem(s.MessageID, "in_progress", nil),
				},
				{
					Type:           "response.content_part.added",
					SequenceNumber: s.nextSeq(),
					ItemID:         s.MessageID,
					OutputIndex:    0,
					ContentIndex:   0,
				},
			}
		}
	case blockTypeToolUse:
		if _, ok := s.toolUses[event.Index]; !ok {
			s.toolUses[event.Index] = &pendingToolUse{
				id:   event.ContentBlock.ID,
				name: event.ContentBlock.Name,
			}
			s.toolOrder = append(s.toolOrder, event.Index)
		}
		outputIdx := s.toolOutputIndex(event.Index)
		return []responses.ResponseStreamEventUnion{
			{
				Type:           "response.output_item.added",
				SequenceNumber: s.nextSeq(),
				OutputIndex:    outputIdx,
				Item:           newInProgressFunctionCallItem(event.ContentBlock.ID, event.ContentBlock.ID, event.ContentBlock.Name),
			},
		}
	}
	return nil
}

func (s *AnthropicResponsesStreamState) handleContentBlockDelta(event anthropic.MessageStreamEventUnion) []responses.ResponseStreamEventUnion {
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
			outputIdx := s.toolOutputIndex(event.Index)
			return []responses.ResponseStreamEventUnion{
				{
					Type:           "response.function_call_arguments.delta",
					SequenceNumber: s.nextSeq(),
					ItemID:         t.id,
					OutputIndex:    outputIdx,
					Arguments:      event.Delta.PartialJSON,
				},
			}
		}
	}
	return nil
}

func (s *AnthropicResponsesStreamState) handleContentBlockStop(event anthropic.MessageStreamEventUnion) []responses.ResponseStreamEventUnion {
	t, ok := s.toolUses[event.Index]
	if !ok {
		return nil
	}
	args := t.inputBuf.String()
	if args == "" {
		args = "{}"
	}
	outputIdx := s.toolOutputIndex(event.Index)
	completed := newFunctionCallItem(t.id, t.id, t.name, args)
	return []responses.ResponseStreamEventUnion{
		{
			Type:           "response.function_call_arguments.done",
			SequenceNumber: s.nextSeq(),
			ItemID:         t.id,
			OutputIndex:    outputIdx,
			Arguments:      args,
		},
		{
			Type:           "response.output_item.done",
			SequenceNumber: s.nextSeq(),
			OutputIndex:    outputIdx,
			Item:           completed,
		},
	}
}

func (s *AnthropicResponsesStreamState) handleMessageStop() []responses.ResponseStreamEventUnion {
	fullText := s.textBuf.String()
	toolItems := s.completedToolItems()
	status := responses.ResponseStatus("completed")
	if s.stopReason == "max_tokens" {
		status = "incomplete"
	}

	var output []responses.ResponseOutputItemUnion
	var events []responses.ResponseStreamEventUnion

	if s.sentMsgOutputItem {
		var content []responses.ResponseOutputMessageContentUnion
		if fullText != "" {
			content = []responses.ResponseOutputMessageContentUnion{newOutputTextContent(fullText)}
		}
		msgItem := newMessageItem(s.MessageID, "completed", content)
		output = append(output, msgItem)
		events = append(events,
			responses.ResponseStreamEventUnion{
				Type:           "response.output_text.done",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
				Text:           fullText,
			},
			responses.ResponseStreamEventUnion{
				Type:           "response.content_part.done",
				SequenceNumber: s.nextSeq(),
				ItemID:         s.MessageID,
				OutputIndex:    0,
				ContentIndex:   0,
			},
			responses.ResponseStreamEventUnion{
				Type:           "response.output_item.done",
				SequenceNumber: s.nextSeq(),
				OutputIndex:    0,
				Item:           msgItem,
			},
		)
	}
	output = append(output, toolItems...)

	completedResp := responses.Response{
		ID:     s.MessageID,
		Object: constant.Response("response"),
		Model:  s.Model,
		Status: status,
		Output: output,
		Usage:  responsesUsage(s.inputTokens, s.cacheReadTokens, s.cacheWriteTokens, s.outputTokens),
	}
	events = append(events, responses.ResponseStreamEventUnion{
		Type:           "response.completed",
		SequenceNumber: s.nextSeq(),
		Response:       completedResp,
	})
	return events
}

// toolOutputIndex returns the output-array index for the tool_use block at
// the given Anthropic content block index. Text message item (if present)
// always occupies index 0, so tools are offset by 1.
func (s *AnthropicResponsesStreamState) toolOutputIndex(blockIdx int64) int64 {
	base := int64(0)
	if s.sentMsgOutputItem {
		base = 1
	}
	for i, idx := range s.toolOrder {
		if idx == blockIdx {
			return base + int64(i)
		}
	}
	return base + int64(len(s.toolOrder)) - 1
}
