package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"

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
		responses.ResponseInputItemParamOfFunctionCallOutput("call_1", `{"temp":"15C"}`),
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
// streaming tool_use block is reconstructed in the terminal response.completed
// event so the streaming output shape matches AnthropicMessageToResponsesResponse.
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
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonToolUse}},
		{Type: "message_stop"},
	}

	var out []responses.ResponseStreamEventUnion
	for _, ev := range events {
		out = append(out, convert.AnthropicEventToResponsesEvents(ev, state)...)
	}

	// Find the terminal response.completed event.
	var completed *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.completed" {
			completed = &out[i]
		}
	}
	if completed == nil {
		t.Fatal("expected a response.completed event in stream output")
	}

	// The completed response must include the function_call output item with
	// the accumulated arguments — same shape as the non-streaming bridge.
	gotFuncCall := false
	for _, item := range completed.Response.Output {
		if item.Type == "function_call" && item.CallID == "toolu_y" {
			gotFuncCall = true
			if item.Arguments != `{"city":"Berlin"}` {
				t.Errorf("Arguments = %q, want accumulated JSON", item.Arguments)
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
