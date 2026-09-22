package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

// ─── ResponsesParamsToAnthropicParams ────────────────────────────────────────

func TestResponsesParamsToAnthropicParams_PreviousResponseIDRejected(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.PreviousResponseID = openai.String("resp_abc")

	_, err := convert.ResponsesParamsToAnthropicParams(params)
	if err == nil {
		t.Fatal("expected TranslationError for previous_response_id, got nil")
	}
	if te := convert.AsTranslationError(err); te == nil {
		t.Fatalf("expected *TranslationError, got %T", err)
	}
}

func TestResponsesParamsToAnthropicParams_StringInput(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfString = openai.String("Hello, Claude.")

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want claude-sonnet-4-6", got.Model)
	}
	// claude-sonnet-4-6 documented max is 64K. When max_output_tokens is
	// unset, the bridge must use the model's max, not an arbitrary cap.
	if got.MaxTokens != 64_000 {
		t.Errorf("MaxTokens = %d, want 64000 (claude-sonnet-4 model max)", got.MaxTokens)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(got.Messages))
	}
}

func TestResponsesParamsToAnthropicParams_ExplicitMaxOutputTokensForwarded(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfString = openai.String("hi")
	params.MaxOutputTokens = openai.Int(12_345)

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxTokens != 12_345 {
		t.Errorf("MaxTokens = %d, want 12345 (explicit value forwarded)", got.MaxTokens)
	}
}

func TestResponsesParamsToAnthropicParams_InstructionsHoistedToSystem(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Instructions = openai.String("You are a careful assistant.")
	params.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.System) != 1 {
		t.Fatalf("len(System) = %d, want 1", len(got.System))
	}
	if got.System[0].Text != "You are a careful assistant." {
		t.Errorf("System[0].Text = %q", got.System[0].Text)
	}
	// User message must NOT carry the system instruction.
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(got.Messages))
	}
}

func TestResponsesParamsToAnthropicParams_SystemRoleInputItemHoisted(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("be terse", responses.EasyInputMessageRoleSystem),
		responses.ResponseInputItemParamOfMessage("be helpful", responses.EasyInputMessageRoleDeveloper),
		responses.ResponseInputItemParamOfMessage("hi there", responses.EasyInputMessageRoleUser),
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.System) != 2 {
		t.Fatalf("len(System) = %d, want 2 (system + developer hoisted)", len(got.System))
	}
	if got.System[0].Text != "be terse" {
		t.Errorf("System[0] = %q, want be terse", got.System[0].Text)
	}
	if got.System[1].Text != "be helpful" {
		t.Errorf("System[1] = %q, want be helpful", got.System[1].Text)
	}
	// Only the user message should appear in Messages.
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1 (only user)", len(got.Messages))
	}
}

func TestResponsesParamsToAnthropicParams_FunctionCallRoundTrip(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("what's the weather in London?", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{"city":"London"}`, "call_1", "get_weather"),
		functionCallOutput("call_1", `{"temp":"15C"}`),
		responses.ResponseInputItemParamOfMessage("thanks", responses.EasyInputMessageRoleUser),
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: user(text) → assistant(tool_use) → user(tool_result) → user(text)
	if len(got.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4 — got %+v", len(got.Messages), got.Messages)
	}

	// Marshal to JSON to inspect the produced structure independently of SDK
	// internal field names.
	b, _ := json.Marshal(got.Messages)
	s := string(b)
	if !strings.Contains(s, `"tool_use"`) {
		t.Errorf("expected tool_use block in messages, got: %s", s)
	}
	if !strings.Contains(s, `"tool_result"`) {
		t.Errorf("expected tool_result block in messages, got: %s", s)
	}
	if !strings.Contains(s, `"call_1"`) {
		t.Errorf("expected call_id pass-through, got: %s", s)
	}
}

func TestResponsesParamsToAnthropicParams_ToolDefsTranslated(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfString = openai.String("hi")
	params.Tools = []responses.ToolUnionParam{
		{
			OfFunction: &responses.FunctionToolParam{
				Name: "get_weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		},
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(got.Tools))
	}
	if got.Tools[0].OfTool == nil || got.Tools[0].OfTool.Name != "get_weather" {
		t.Errorf("expected get_weather tool, got %+v", got.Tools[0])
	}
}

// ─── AnthropicMessageToResponsesResponse ─────────────────────────────────────

func TestAnthropicMessageToResponsesResponse_TextOnly(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_1",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello!"},
		},
		StopReason: anthropic.StopReasonEndTurn,
		Usage:      anthropic.Usage{InputTokens: 5, OutputTokens: 2},
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)

	if got.Status != "completed" {
		t.Errorf("Status = %q, want completed", got.Status)
	}
	if len(got.Output) != 1 {
		t.Fatalf("len(Output) = %d, want 1 (message only)", len(got.Output))
	}
	if got.Output[0].Type != "message" {
		t.Errorf("Output[0].Type = %q", got.Output[0].Type)
	}
	if got.Usage.TotalTokens != 7 {
		t.Errorf("TotalTokens = %d, want 7", got.Usage.TotalTokens)
	}
}

func TestAnthropicMessageToResponsesResponse_WithToolUse(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_2",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "I'll check."},
			{Type: "tool_use", ID: "toolu_x", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris"}`)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)
	if len(got.Output) != 2 {
		t.Fatalf("len(Output) = %d, want 2 (message + function_call)", len(got.Output))
	}
	if got.Output[1].Type != "function_call" {
		t.Errorf("Output[1].Type = %q, want function_call", got.Output[1].Type)
	}
	if got.Output[1].CallID != "toolu_x" {
		t.Errorf("Output[1].CallID = %q, want toolu_x", got.Output[1].CallID)
	}
	if got.Output[1].Name != "get_weather" {
		t.Errorf("Output[1].Name = %q, want get_weather", got.Output[1].Name)
	}
}

// ─── Streaming / non-streaming parity ────────────────────────────────────────

// TestAnthropicStreaming_FunctionCallEmittedInTerminalEvent asserts that a
// pure-tool-call stream (no text) emits inline output_item.added /
// arguments.delta / arguments.done / output_item.done events AND that the
// terminal response.completed carries the same function_call item.
func TestAnthropicStreaming_FunctionCallEmittedInTerminalEvent(t *testing.T) {
	t.Parallel()
	state := convert.NewAnthropicResponsesStreamState()

	events := []anthropic.MessageStreamEventUnion{
		{
			Type: "message_start",
			Message: anthropic.Message{
				ID:    "msg_stream",
				Model: "claude-sonnet-4-6",
			},
		},
		{
			Type:  "content_block_start",
			Index: 0,
			ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{
				Type: "tool_use",
				ID:   "toolu_y",
				Name: "get_weather",
			},
		},
		{
			Type:  "content_block_delta",
			Index: 0,
			Delta: anthropic.MessageStreamEventUnionDelta{
				Type:        "input_json_delta",
				PartialJSON: `{"city":"Berlin"}`,
			},
		},
		{Type: "content_block_stop", Index: 0},
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonToolUse}},
		{Type: "message_stop"},
	}

	var out []responses.ResponseStreamEventUnion
	for _, ev := range events {
		out = append(out, convert.AnthropicEventToResponsesEvents(ev, state)...)
	}

	// output_item.added (function_call) must be emitted inline at content_block_start.
	var addedItem *responses.ResponseOutputItemUnion
	for i := range out {
		if out[i].Type == "response.output_item.added" && out[i].Item.Type == "function_call" {
			addedItem = &out[i].Item
		}
	}
	if addedItem == nil {
		t.Fatal("expected inline response.output_item.added for function_call")
	}
	if addedItem.Name != "get_weather" {
		t.Errorf("output_item.added Name = %q, want get_weather", addedItem.Name)
	}

	// response.function_call_arguments.delta must carry partial JSON.
	var argsDelta *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.function_call_arguments.delta" {
			argsDelta = &out[i]
		}
	}
	if argsDelta == nil {
		t.Fatal("expected response.function_call_arguments.delta in stream")
	}
	if argsDelta.Arguments != `{"city":"Berlin"}` {
		t.Errorf("arguments.delta = %q, want JSON fragment", argsDelta.Arguments)
	}

	// response.function_call_arguments.done must carry full accumulated args.
	var argsDone *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.function_call_arguments.done" {
			argsDone = &out[i]
		}
	}
	if argsDone == nil {
		t.Fatal("expected response.function_call_arguments.done in stream")
	}
	if argsDone.Arguments != `{"city":"Berlin"}` {
		t.Errorf("arguments.done = %q, want full args", argsDone.Arguments)
	}

	// No message output item — pure tool call with no text.
	for _, ev := range out {
		if ev.Type == "response.output_item.added" && ev.Item.Type == "message" {
			t.Error("unexpected message output_item.added for pure tool call stream")
		}
	}

	// The terminal response.completed must include the function_call item.
	var completed *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.completed" {
			completed = &out[i]
		}
	}
	if completed == nil {
		t.Fatal("expected a response.completed event in stream output")
	}

	gotFuncCall := false
	for _, item := range completed.Response.Output {
		if item.Type == "function_call" && item.CallID == "toolu_y" {
			gotFuncCall = true
			if item.Arguments.OfString != `{"city":"Berlin"}` {
				t.Errorf("Arguments = %q, want accumulated JSON", item.Arguments.OfString)
			}
			if item.Name != "get_weather" {
				t.Errorf("Name = %q, want get_weather", item.Name)
			}
		}
	}
	if !gotFuncCall {
		t.Errorf("function_call output item not found in completed response: %+v", completed.Response.Output)
	}
}

// ─── Tool choice translation ──────────────────────────────────────────────────

// TestResponsesParamsToAnthropicParams_ToolChoice exercises every branch of
// anthropicToolChoice. The "required"→OfAny mapping is the non-obvious one:
// Responses API "required" means "call any tool", which is exactly Anthropic's
// "any" mode — not OfTool, which would mean a specific function.
func TestResponsesParamsToAnthropicParams_ToolChoice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    responses.ToolChoiceOptions
		fnName  string
		checkFn func(*testing.T, anthropic.ToolChoiceUnionParam)
	}{
		{
			name: "auto → OfAuto",
			mode: responses.ToolChoiceOptionsAuto,
			checkFn: func(t *testing.T, tc anthropic.ToolChoiceUnionParam) {
				if tc.OfAuto == nil {
					t.Fatal("expected OfAuto for auto mode")
				}
				if tc.OfAny != nil || tc.OfNone != nil || tc.OfTool != nil {
					t.Error("only OfAuto should be set")
				}
			},
		},
		{
			name: "required → OfAny",
			mode: responses.ToolChoiceOptionsRequired,
			checkFn: func(t *testing.T, tc anthropic.ToolChoiceUnionParam) {
				if tc.OfAny == nil {
					t.Fatal("expected OfAny for required mode (Anthropic equivalent of required)")
				}
				if tc.OfAuto != nil || tc.OfNone != nil || tc.OfTool != nil {
					t.Error("only OfAny should be set")
				}
			},
		},
		{
			name: "none → OfNone",
			mode: responses.ToolChoiceOptionsNone,
			checkFn: func(t *testing.T, tc anthropic.ToolChoiceUnionParam) {
				if tc.OfNone == nil {
					t.Fatal("expected OfNone for none mode")
				}
				if tc.OfAuto != nil || tc.OfAny != nil || tc.OfTool != nil {
					t.Error("only OfNone should be set")
				}
			},
		},
		{
			name:   "specific function → OfTool",
			fnName: "my_fn",
			checkFn: func(t *testing.T, tc anthropic.ToolChoiceUnionParam) {
				if tc.OfTool == nil {
					t.Fatal("expected OfTool for specific function")
				}
				if tc.OfTool.Name != "my_fn" {
					t.Errorf("OfTool.Name = %q, want my_fn", tc.OfTool.Name)
				}
				if tc.OfAuto != nil || tc.OfAny != nil || tc.OfNone != nil {
					t.Error("only OfTool should be set")
				}
			},
		},
		{
			name: "unset → zero value",
			checkFn: func(t *testing.T, tc anthropic.ToolChoiceUnionParam) {
				if tc.OfAuto != nil || tc.OfAny != nil || tc.OfNone != nil || tc.OfTool != nil {
					t.Error("expected zero ToolChoiceUnionParam when not set in params")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
			p.Input.OfString = openai.String("hi")
			if tc.mode != "" {
				p.ToolChoice.OfToolChoiceMode = param.NewOpt(tc.mode)
			}
			if tc.fnName != "" {
				p.ToolChoice.OfFunctionTool = &responses.ToolChoiceFunctionParam{Name: tc.fnName}
			}

			got, err := convert.ResponsesParamsToAnthropicParams(p)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.checkFn(t, got.ToolChoice)
		})
	}
}

// ─── Cache token attribution ──────────────────────────────────────────────────

// The Responses dialect is OpenAI's: input_tokens counts every prompt token and
// input_tokens_details.cached_tokens names the cached subset. Anthropic reports
// the two disjoint, so the bridge folds cache reads back into input_tokens.
func TestAnthropicMessageToResponsesResponse_CacheReadTokens(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_cache",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Answer."},
		},
		StopReason: anthropic.StopReasonEndTurn,
		Usage: anthropic.Usage{
			InputTokens:              100,
			OutputTokens:             20,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 0,
		},
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)

	if got.Usage.InputTokens != 130 {
		t.Errorf("InputTokens = %d, want 130 (uncached 100 + cache read 30)", got.Usage.InputTokens)
	}
	if got.Usage.InputTokensDetails.CachedTokens != 30 {
		t.Errorf("InputTokensDetails.CachedTokens = %d, want 30 (cache_read_input_tokens)", got.Usage.InputTokensDetails.CachedTokens)
	}
	if got.Usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150 (input+output)", got.Usage.TotalTokens)
	}
}

// TestAnthropicStreaming_CacheTokensInCompletedEvent verifies that cache_read_input_tokens
// from the message_start event are surfaced in the terminal response.completed
// Usage.InputTokensDetails.CachedTokens field.
// ─── Image input translation ──────────────────────────────────────────────────

func TestResponsesParamsToAnthropicParams_ImageURLInput(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfInputItemContentList: responses.ResponseInputMessageContentListParam{
						{OfInputText: &responses.ResponseInputTextParam{Text: "What is in this image?"}},
						{OfInputImage: &responses.ResponseInputImageParam{
							Detail:   responses.ResponseInputImageDetailAuto,
							ImageURL: openai.String("https://example.com/photo.jpg"),
						}},
					},
				},
			},
		},
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(got.Messages))
	}
	b, _ := json.Marshal(got.Messages[0])
	s := string(b)
	if !strings.Contains(s, `"url"`) {
		t.Errorf("expected URL image source in message, got: %s", s)
	}
	if !strings.Contains(s, "example.com/photo.jpg") {
		t.Errorf("expected image URL preserved, got: %s", s)
	}
}

func TestResponsesParamsToAnthropicParams_ImageBase64Input(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfInputItemContentList: responses.ResponseInputMessageContentListParam{
						{OfInputText: &responses.ResponseInputTextParam{Text: "Describe this."}},
						{OfInputImage: &responses.ResponseInputImageParam{
							Detail:   responses.ResponseInputImageDetailHigh,
							ImageURL: openai.String("data:image/png;base64,iVBORw0KGgo="),
						}},
					},
				},
			},
		},
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(got.Messages[0])
	s := string(b)
	if !strings.Contains(s, `"base64"`) {
		t.Errorf("expected base64 image source, got: %s", s)
	}
	if !strings.Contains(s, "image/png") {
		t.Errorf("expected media type preserved, got: %s", s)
	}
	if !strings.Contains(s, "iVBORw0KGgo=") {
		t.Errorf("expected base64 data preserved, got: %s", s)
	}
}

func TestResponsesParamsToAnthropicParams_FileIDImageSkipped(t *testing.T) {
	t.Parallel()
	// File-ID-only images have no Anthropic equivalent — the image block is
	// silently skipped but text content in the same message is preserved.
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfInputItemContentList: responses.ResponseInputMessageContentListParam{
						{OfInputText: &responses.ResponseInputTextParam{Text: "hello"}},
						{OfInputImage: &responses.ResponseInputImageParam{
							Detail: responses.ResponseInputImageDetailAuto,
							FileID: openai.String("file-abc123"),
							// no ImageURL
						}},
					},
				},
			},
		},
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1 (text preserved, file-id image dropped)", len(got.Messages))
	}
	b, _ := json.Marshal(got.Messages[0])
	if !strings.Contains(string(b), "hello") {
		t.Errorf("text content should be preserved even when file-id image is skipped")
	}
}

// ─── Multiple tool-use blocks (parallel tool calls) ──────────────────────────

func TestAnthropicMessageToResponsesResponse_MultipleToolUse(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_multi",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "I'll call both tools."},
			{Type: "tool_use", ID: "toolu_1", Name: "search", Input: json.RawMessage(`{"q":"foo"}`)},
			{Type: "tool_use", ID: "toolu_2", Name: "calc", Input: json.RawMessage(`{"expr":"1+1"}`)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)
	if len(got.Output) != 3 {
		t.Fatalf("len(Output) = %d, want 3 (message + 2 function_calls)", len(got.Output))
	}
	if got.Output[1].CallID != "toolu_1" || got.Output[1].Name != "search" {
		t.Errorf("Output[1] = %+v, want toolu_1/search", got.Output[1])
	}
	if got.Output[2].CallID != "toolu_2" || got.Output[2].Name != "calc" {
		t.Errorf("Output[2] = %+v, want toolu_2/calc", got.Output[2])
	}
}

// ─── max_tokens → incomplete status ──────────────────────────────────────────

func TestAnthropicMessageToResponsesResponse_MaxTokensIncomplete(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:         "msg_trunc",
		Model:      "claude-sonnet-4-6",
		Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "Truncated..."}},
		StopReason: anthropic.StopReasonMaxTokens,
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)
	if got.Status != "incomplete" {
		t.Errorf("Status = %q, want incomplete for max_tokens stop reason", got.Status)
	}
}

func TestAnthropicStreaming_MaxTokensIncomplete(t *testing.T) {
	t.Parallel()
	state := convert.NewAnthropicResponsesStreamState()

	events := []anthropic.MessageStreamEventUnion{
		{Type: "message_start", Message: anthropic.Message{ID: "msg_trunc_stream", Model: "claude-sonnet-4-6"}},
		{Type: "content_block_start", Index: 0, ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "text"}},
		{Type: "content_block_delta", Index: 0, Delta: anthropic.MessageStreamEventUnionDelta{Type: "text_delta", Text: "partial"}},
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonMaxTokens}},
		{Type: "message_stop"},
	}

	var out []responses.ResponseStreamEventUnion
	for _, ev := range events {
		out = append(out, convert.AnthropicEventToResponsesEvents(ev, state)...)
	}

	var completed *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.completed" {
			completed = &out[i]
		}
	}
	if completed == nil {
		t.Fatal("expected response.completed event")
	}
	if completed.Response.Status != "incomplete" {
		t.Errorf("Status = %q, want incomplete for max_tokens", completed.Response.Status)
	}
}

// ─── Instructions + system input items ordering ───────────────────────────────

func TestResponsesParamsToAnthropicParams_InstructionsPrecedeSystemInputItems(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Instructions = openai.String("top-level instruction")
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("input system block", responses.EasyInputMessageRoleSystem),
		responses.ResponseInputItemParamOfMessage("hi", responses.EasyInputMessageRoleUser),
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.System) != 2 {
		t.Fatalf("len(System) = %d, want 2", len(got.System))
	}
	if got.System[0].Text != "top-level instruction" {
		t.Errorf("System[0] = %q, want instructions first", got.System[0].Text)
	}
	if got.System[1].Text != "input system block" {
		t.Errorf("System[1] = %q, want input system block second", got.System[1].Text)
	}
}

// ─── Temperature / TopP forwarding ───────────────────────────────────────────

func TestResponsesParamsToAnthropicParams_TemperatureTopPForwarded(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfString = openai.String("hi")
	params.Temperature = openai.Float(0.4)
	params.TopP = openai.Float(0.85)

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Temperature.Valid() || got.Temperature.Value != 0.4 {
		t.Errorf("Temperature = {valid=%v value=%v}, want {true 0.4}", got.Temperature.Valid(), got.Temperature.Value)
	}
	if !got.TopP.Valid() || got.TopP.Value != 0.85 {
		t.Errorf("TopP = {valid=%v value=%v}, want {true 0.85}", got.TopP.Valid(), got.TopP.Value)
	}
}

func TestResponsesParamsToAnthropicParams_TemperatureTopPUnset(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "claude-sonnet-4-6"}
	params.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Temperature.Valid() {
		t.Error("Temperature should be omitted when not set")
	}
	if got.TopP.Valid() {
		t.Error("TopP should be omitted when not set")
	}
}

// ─── Cache token attribution ──────────────────────────────────────────────────

func TestAnthropicStreaming_CacheTokensInCompletedEvent(t *testing.T) {
	t.Parallel()
	state := convert.NewAnthropicResponsesStreamState()

	events := []anthropic.MessageStreamEventUnion{
		{
			Type: "message_start",
			Message: anthropic.Message{
				ID:    "msg_cache_stream",
				Model: "claude-sonnet-4-6",
				Usage: anthropic.Usage{
					InputTokens:          50,
					CacheReadInputTokens: 15,
				},
			},
		},
		{
			Type:         "content_block_start",
			Index:        0,
			ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "text"},
		},
		{
			Type:  "content_block_delta",
			Index: 0,
			Delta: anthropic.MessageStreamEventUnionDelta{Type: "text_delta", Text: "Hi"},
		},
		{
			Type:  "message_delta",
			Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonEndTurn},
			Usage: anthropic.MessageDeltaUsage{OutputTokens: 5},
		},
		{Type: "message_stop"},
	}

	var out []responses.ResponseStreamEventUnion
	for _, ev := range events {
		out = append(out, convert.AnthropicEventToResponsesEvents(ev, state)...)
	}

	var completed *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.completed" {
			completed = &out[i]
		}
	}
	if completed == nil {
		t.Fatal("expected response.completed event")
	}
	if completed.Response.Usage.InputTokensDetails.CachedTokens != 15 {
		t.Errorf("CachedTokens = %d, want 15", completed.Response.Usage.InputTokensDetails.CachedTokens)
	}
	if completed.Response.Usage.InputTokens != 65 {
		t.Errorf("InputTokens = %d, want 65 (uncached 50 + cache read 15)", completed.Response.Usage.InputTokens)
	}
	if completed.Response.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", completed.Response.Usage.OutputTokens)
	}
	if completed.Response.Usage.TotalTokens != 70 {
		t.Errorf("TotalTokens = %d, want 70 (input+output)", completed.Response.Usage.TotalTokens)
	}
}

// ─── Wire-format regression: function_call items must have RawJSON populated ──

// TestAnthropicMessageToResponsesResponse_FunctionCallWireFormat verifies that
// function_call output items have JSON.raw populated (via newFunctionCallItem's
// round-trip construction), so that the handler-level marshal helpers can use
// RawJSON() to produce correct wire bytes. This is a regression guard for the
// v3 SDK upgrade: direct ResponseOutputItemUnion struct construction produces
// malformed arguments JSON because the type has no MarshalJSON.
func TestAnthropicMessageToResponsesResponse_FunctionCallWireFormat(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_wire",
		Model: "claude-3-5-sonnet-20241022",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "toolu_wire", Name: "get_weather", Input: json.RawMessage(`{"city":"Berlin"}`)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToResponsesResponse(msg)
	if len(got.Output) == 0 {
		t.Fatal("expected at least 1 output item")
	}

	var foundFunctionCall bool
	for _, item := range got.Output {
		if item.Type != "function_call" {
			continue
		}
		foundFunctionCall = true
		rj := item.RawJSON()
		if rj == "" {
			t.Error("RawJSON() is empty — newFunctionCallItem did not round-trip correctly")
			continue
		}
		// Verify the raw JSON contains "arguments" as a plain string value.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rj), &raw); err != nil {
			t.Fatalf("unmarshal RawJSON: %v", err)
		}
		argsRaw, ok := raw["arguments"]
		if !ok {
			t.Error("arguments key missing from RawJSON")
			continue
		}
		if len(argsRaw) == 0 || argsRaw[0] != '"' {
			t.Errorf("arguments in RawJSON = %s, want a JSON string", argsRaw)
		}
		var argsStr string
		if err := json.Unmarshal(argsRaw, &argsStr); err != nil {
			t.Errorf("arguments is not a valid JSON string: %v — raw: %s", err, argsRaw)
		}
		if argsStr != `{"city":"Berlin"}` {
			t.Errorf("arguments string = %q, want {\"city\":\"Berlin\"}", argsStr)
		}
	}
	if !foundFunctionCall {
		t.Error("no function_call item found in response output")
	}
}

// TestAnthropicStreaming_FunctionCallWireFormat verifies that function_call items
// in the streaming bridge have RawJSON populated correctly.
func TestAnthropicStreaming_FunctionCallWireFormat(t *testing.T) {
	t.Parallel()
	state := convert.NewAnthropicResponsesStreamState()

	events := []anthropic.MessageStreamEventUnion{
		{Type: "message_start", Message: anthropic.Message{ID: "msg_ws", Model: "claude-3-5-sonnet-20241022"}},
		{Type: "content_block_start", Index: 0, ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "tool_use", ID: "toolu_ws", Name: "calc"}},
		{Type: "content_block_delta", Index: 0, Delta: anthropic.MessageStreamEventUnionDelta{Type: "input_json_delta", PartialJSON: `{"x":1}`}},
		{Type: "content_block_stop", Index: 0},
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonToolUse}},
		{Type: "message_stop"},
	}

	var completed *responses.ResponseStreamEventUnion
	for _, ev := range events {
		for _, out := range convert.AnthropicEventToResponsesEvents(ev, state) {
			if out.Type == "response.completed" {
				cp := out
				completed = &cp
			}
		}
	}
	if completed == nil {
		t.Fatal("no response.completed event emitted")
	}

	var foundFunctionCall bool
	for _, item := range completed.Response.Output {
		if item.Type != "function_call" {
			continue
		}
		foundFunctionCall = true
		rj := item.RawJSON()
		if rj == "" {
			t.Error("RawJSON() is empty — newFunctionCallItem did not round-trip correctly")
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rj), &raw); err != nil {
			t.Fatalf("unmarshal RawJSON: %v", err)
		}
		argsRaw, ok := raw["arguments"]
		if !ok {
			t.Error("arguments key missing from RawJSON in stream event")
			continue
		}
		if len(argsRaw) == 0 || argsRaw[0] != '"' {
			t.Errorf("stream event arguments in RawJSON = %s, want a JSON string", argsRaw)
		}
	}
	if !foundFunctionCall {
		t.Error("no function_call item found in response.completed output")
	}
}

// functionCallOutput builds a function_call_output input item. openai-go's own
// constructor takes only the output since v3.55, so the call id is set here.
func functionCallOutput(callID, output string) responses.ResponseInputItemUnionParam {
	item := responses.ResponseInputItemFunctionCallOutputParam{CallID: param.NewOpt(callID)}
	item.Output.OfString = param.NewOpt(output)
	return responses.ResponseInputItemUnionParam{OfFunctionCallOutput: &item}
}
