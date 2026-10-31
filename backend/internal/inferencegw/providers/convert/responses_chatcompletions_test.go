package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
)

// ─── ResponsesParamsToChatCompletionParams ───────────────────────────────────

func TestResponsesParamsToChatCompletionParams_PreviousResponseIDRejected(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "amazon.nova-pro-v1:0"}
	params.PreviousResponseID = openai.String("resp_abc")

	_, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err == nil {
		t.Fatal("expected TranslationError for previous_response_id")
	}
	if te := convert.AsTranslationError(err); te == nil {
		t.Fatalf("expected *TranslationError, got %T", err)
	}
}

func TestResponsesParamsToChatCompletionParams_InstructionsBecomeSystemMessage(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "amazon.nova-pro-v1:0"}
	params.Instructions = openai.String("You are terse.")
	params.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2 (system + user)", len(got.Messages))
	}
	b, _ := json.Marshal(got.Messages[0])
	if !strings.Contains(string(b), `"role":"system"`) {
		t.Errorf("first message should be system, got: %s", b)
	}
}

func TestResponsesParamsToChatCompletionParams_FunctionCallRoundTrip(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "amazon.nova-pro-v1:0"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("what's the weather in London?", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{"city":"London"}`, "call_1", "get_weather"),
		functionCallOutput("call_1", `{"temp":"15C"}`),
		responses.ResponseInputItemParamOfMessage("thanks", responses.EasyInputMessageRoleUser),
	}

	got, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: user(text) → assistant(tool_calls) → tool(result) → user(text)
	if len(got.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(got.Messages))
	}
	b, _ := json.Marshal(got.Messages)
	s := string(b)
	if !strings.Contains(s, `"tool_calls"`) {
		t.Errorf("expected tool_calls on assistant, got: %s", s)
	}
	if !strings.Contains(s, `"role":"tool"`) {
		t.Errorf("expected tool role for function output, got: %s", s)
	}
	if !strings.Contains(s, `"call_1"`) {
		t.Errorf("expected call_id pass-through, got: %s", s)
	}
}

// ─── ChatCompletionToResponsesResponse ───────────────────────────────────────

func TestChatCompletionToResponsesResponse_WithToolCalls(t *testing.T) {
	t.Parallel()
	completion := &openai.ChatCompletion{
		ID:    "chatcmpl_1",
		Model: "amazon.nova-pro-v1:0",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "I'll check.",
					ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
						{
							ID:   "call_x",
							Type: "function",
							Function: openai.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	}
	got := convert.ChatCompletionToResponsesResponse(completion, nil)
	if len(got.Output) != 2 {
		t.Fatalf("len(Output) = %d, want 2 (message + function_call)", len(got.Output))
	}
	if got.Output[1].Type != "function_call" {
		t.Errorf("Output[1].Type = %q, want function_call", got.Output[1].Type)
	}
	if got.Output[1].Name != "get_weather" {
		t.Errorf("Output[1].Name = %q, want get_weather", got.Output[1].Name)
	}
	if got.Output[1].Arguments.OfString != `{"city":"Paris"}` {
		t.Errorf("Output[1].Arguments = %q", got.Output[1].Arguments.OfString)
	}
}

// ─── Streaming / non-streaming parity ────────────────────────────────────────

// TestChatCompletionsStreaming_ToolCallEmittedInTerminalEvent asserts that
// streaming tool-call deltas are reconstructed in the terminal response.completed
// event so the streaming and non-streaming bridge outputs agree on shape.
func TestChatCompletionsStreaming_ToolCallEmittedInTerminalEvent(t *testing.T) {
	t.Parallel()
	state := convert.NewChatCompletionsStreamState(nil)

	chunks := []openai.ChatCompletionChunk{
		{
			ID:    "chatcmpl_stream",
			Model: "amazon.nova-pro-v1:0",
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Delta: openai.ChatCompletionChunkChoiceDelta{
						Role: "assistant",
						ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								ID:    "call_y",
								Type:  "function",
								Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Name:      "get_weather",
									Arguments: `{"city":`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID:    "chatcmpl_stream",
			Model: "amazon.nova-pro-v1:0",
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Delta: openai.ChatCompletionChunkChoiceDelta{
						ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
							{
								Index: 0,
								Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
									Arguments: `"Berlin"}`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID:    "chatcmpl_stream",
			Model: "amazon.nova-pro-v1:0",
			Choices: []openai.ChatCompletionChunkChoice{
				{
					Delta:        openai.ChatCompletionChunkChoiceDelta{},
					FinishReason: "tool_calls",
				},
			},
		},
	}

	var out []responses.ResponseStreamEventUnion
	for _, c := range chunks {
		out = append(out, convert.ChatCompletionChunkToResponsesEvents(c, state)...)
	}
	// Simulate the bridge flushing the deferred response.completed after the stream ends.
	if ev := state.FlushPendingCompletion(0, 0, 0); ev != nil {
		out = append(out, *ev)
	}

	var completed *responses.ResponseStreamEventUnion
	for i := range out {
		if out[i].Type == "response.completed" {
			completed = &out[i]
		}
	}
	if completed == nil {
		t.Fatal("expected response.completed event in stream output")
	}

	gotCall := false
	for _, item := range completed.Response.Output {
		if item.Type == "function_call" && item.CallID == "call_y" {
			gotCall = true
			if item.Name != "get_weather" {
				t.Errorf("Name = %q, want get_weather", item.Name)
			}
			if item.Arguments.OfString != `{"city":"Berlin"}` {
				t.Errorf("Arguments = %q, want accumulated JSON", item.Arguments.OfString)
			}
		}
	}
	if !gotCall {
		t.Errorf("function_call not found in completed output: %+v", completed.Response.Output)
	}
}

// ─── Multiple tool calls (parallel) ──────────────────────────────────────────

func TestChatCompletionToResponsesResponse_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	completion := &openai.ChatCompletion{
		ID:    "chatcmpl_multi",
		Model: "gpt-4o",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
						{ID: "call_a", Type: "function", Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "search", Arguments: `{"q":"foo"}`}},
						{ID: "call_b", Type: "function", Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "calc", Arguments: `{"expr":"1+1"}`}},
					},
				},
			},
		},
	}
	got := convert.ChatCompletionToResponsesResponse(completion, nil)
	if len(got.Output) != 3 {
		t.Fatalf("len(Output) = %d, want 3 (message + 2 function_calls)", len(got.Output))
	}
	if got.Output[1].CallID != "call_a" || got.Output[1].Name != "search" {
		t.Errorf("Output[1] = %+v, want call_a/search", got.Output[1])
	}
	if got.Output[2].CallID != "call_b" || got.Output[2].Name != "calc" {
		t.Errorf("Output[2] = %+v, want call_b/calc", got.Output[2])
	}
}

// ─── length finish reason → incomplete status ─────────────────────────────────

func TestChatCompletionToResponsesResponse_LengthIncomplete(t *testing.T) {
	t.Parallel()
	completion := &openai.ChatCompletion{
		ID:    "chatcmpl_trunc",
		Model: "gpt-4o",
		Choices: []openai.ChatCompletionChoice{
			{FinishReason: "length", Message: openai.ChatCompletionMessage{Role: "assistant", Content: "Truncated..."}},
		},
	}
	got := convert.ChatCompletionToResponsesResponse(completion, nil)
	if got.Status != "incomplete" {
		t.Errorf("Status = %q, want incomplete for finish_reason=length", got.Status)
	}
}

func TestChatCompletionsStreaming_LengthIncomplete(t *testing.T) {
	t.Parallel()
	state := convert.NewChatCompletionsStreamState(nil)

	chunks := []openai.ChatCompletionChunk{
		{
			ID:    "chatcmpl_trunc_stream",
			Model: "gpt-4o",
			Choices: []openai.ChatCompletionChunkChoice{
				{Delta: openai.ChatCompletionChunkChoiceDelta{Role: "assistant", Content: "partial"}},
			},
		},
		{
			ID:    "chatcmpl_trunc_stream",
			Model: "gpt-4o",
			Choices: []openai.ChatCompletionChunkChoice{
				{Delta: openai.ChatCompletionChunkChoiceDelta{}, FinishReason: "length"},
			},
		},
	}

	var out []responses.ResponseStreamEventUnion
	for _, c := range chunks {
		out = append(out, convert.ChatCompletionChunkToResponsesEvents(c, state)...)
	}
	if ev := state.FlushPendingCompletion(0, 0, 0); ev != nil {
		out = append(out, *ev)
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
		t.Errorf("Status = %q, want incomplete for finish_reason=length", completed.Response.Status)
	}
}

// ─── Multiple adjacent function_call items coalesced ─────────────────────────

func TestResponsesParamsToChatCompletionParams_MultipleAdjacentFunctionCalls(t *testing.T) {
	t.Parallel()
	params := responses.ResponseNewParams{Model: "gpt-4o"}
	params.Input.OfInputItemList = []responses.ResponseInputItemUnionParam{
		responses.ResponseInputItemParamOfMessage("do both", responses.EasyInputMessageRoleUser),
		responses.ResponseInputItemParamOfFunctionCall(`{"q":"foo"}`, "call_a", "search"),
		responses.ResponseInputItemParamOfFunctionCall(`{"expr":"1+1"}`, "call_b", "calc"),
		functionCallOutput("call_a", `{"result":"bar"}`),
		functionCallOutput("call_b", `{"result":"2"}`),
	}

	got, err := convert.ResponsesParamsToChatCompletionParams(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: user → assistant(2 tool_calls) → tool(call_a) → tool(call_b)
	if len(got.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(got.Messages))
	}
	b, _ := json.Marshal(got.Messages[1])
	s := string(b)
	if !strings.Contains(s, `"call_a"`) || !strings.Contains(s, `"call_b"`) {
		t.Errorf("both call IDs should be in the coalesced assistant message, got: %s", s)
	}
}

// ─── Tool choice translation ──────────────────────────────────────────────────

func TestChatCompletionToolChoice_Modes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode responses.ToolChoiceOptions
		want string
	}{
		{responses.ToolChoiceOptionsAuto, "auto"},
		{responses.ToolChoiceOptionsNone, "none"},
		{responses.ToolChoiceOptionsRequired, "required"},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			t.Parallel()
			p := responses.ResponseNewParams{Model: "gpt-4o"}
			p.Input.OfString = openai.String("hi")
			p.ToolChoice.OfToolChoiceMode = param.NewOpt(tc.mode)

			got, err := convert.ResponsesParamsToChatCompletionParams(p)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.ToolChoice.OfAuto.Valid() {
				t.Fatalf("ToolChoice.OfAuto should be set for mode %q", tc.mode)
			}
			if got.ToolChoice.OfAuto.Value != tc.want {
				t.Errorf("OfAuto.Value = %q, want %q", got.ToolChoice.OfAuto.Value, tc.want)
			}
			if got.ToolChoice.OfFunctionToolChoice != nil {
				t.Error("OfFunctionToolChoice should not be set for a mode")
			}
		})
	}
}

func TestChatCompletionToolChoice_SpecificFunction(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")
	p.ToolChoice.OfFunctionTool = &responses.ToolChoiceFunctionParam{Name: "my_fn"}

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nc := got.ToolChoice.OfFunctionToolChoice
	if nc == nil {
		t.Fatal("OfFunctionToolChoice should be set for a specific function")
	}
	if nc.Function.Name != "my_fn" {
		t.Errorf("Function.Name = %q, want my_fn", nc.Function.Name)
	}
	if got.ToolChoice.OfAuto.Valid() {
		t.Error("OfAuto should not be set when a specific function is specified")
	}
}

func TestChatCompletionToolChoice_Unset(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ToolChoice.OfAuto.Valid() || got.ToolChoice.OfFunctionToolChoice != nil {
		t.Error("ToolChoice should be zero when not set in params")
	}
}

// ─── Response format translation ─────────────────────────────────────────────

func TestChatCompletionResponseFormat_JSONSchema(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")
	p.Text.Format.OfJSONSchema = &responses.ResponseFormatTextJSONSchemaConfigParam{
		Name:        "my_schema",
		Schema:      map[string]any{"type": "object"},
		Strict:      openai.Bool(true),
		Description: openai.String("a structured reply"),
	}

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	js := got.ResponseFormat.OfJSONSchema
	if js == nil {
		t.Fatal("ResponseFormat.OfJSONSchema should be set")
	}
	if js.JSONSchema.Name != "my_schema" {
		t.Errorf("Name = %q, want my_schema", js.JSONSchema.Name)
	}
	if !js.JSONSchema.Strict.Valid() || !js.JSONSchema.Strict.Value {
		t.Error("Strict should be forwarded as true")
	}
	if !js.JSONSchema.Description.Valid() || js.JSONSchema.Description.Value != "a structured reply" {
		t.Error("Description should be forwarded")
	}
	if got.ResponseFormat.OfJSONObject != nil || got.ResponseFormat.OfText != nil {
		t.Error("only OfJSONSchema should be set")
	}
}

func TestChatCompletionResponseFormat_JSONObject(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")
	p.Text.Format.OfJSONObject = &shared.ResponseFormatJSONObjectParam{}

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ResponseFormat.OfJSONObject == nil {
		t.Error("ResponseFormat.OfJSONObject should be set")
	}
	if got.ResponseFormat.OfJSONSchema != nil || got.ResponseFormat.OfText != nil {
		t.Error("only OfJSONObject should be set")
	}
}

func TestChatCompletionResponseFormat_Text(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")
	p.Text.Format.OfText = &shared.ResponseFormatTextParam{}

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ResponseFormat.OfText == nil {
		t.Error("ResponseFormat.OfText should be set")
	}
	if got.ResponseFormat.OfJSONSchema != nil || got.ResponseFormat.OfJSONObject != nil {
		t.Error("only OfText should be set")
	}
}

func TestChatCompletionResponseFormat_Unset(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ResponseFormat.OfJSONSchema != nil || got.ResponseFormat.OfJSONObject != nil || got.ResponseFormat.OfText != nil {
		t.Error("ResponseFormat should be zero when params.Text.Format is not set")
	}
}

// ─── Scalar field forwarding ─────────────────────────────────────────────────

// TestChatCompletionScalarFields_Forwarded verifies that MaxOutputTokens,
// Temperature, TopP, and ParallelToolCalls are direct-assigned from the Responses
// API params to the Chat Completions params (both use the same param.Opt types).
func TestChatCompletionScalarFields_Forwarded(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")
	p.MaxOutputTokens = openai.Int(1234)
	p.Temperature = openai.Float(0.7)
	p.TopP = openai.Float(0.9)
	p.ParallelToolCalls = openai.Bool(false) // false is the interesting case: zero value but explicitly set

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.MaxTokens.Valid() || got.MaxTokens.Value != 1234 {
		t.Errorf("MaxTokens = {valid=%v value=%v}, want {true 1234}", got.MaxTokens.Valid(), got.MaxTokens.Value)
	}
	if !got.Temperature.Valid() || got.Temperature.Value != 0.7 {
		t.Errorf("Temperature = {valid=%v value=%v}, want {true 0.7}", got.Temperature.Valid(), got.Temperature.Value)
	}
	if !got.TopP.Valid() || got.TopP.Value != 0.9 {
		t.Errorf("TopP = {valid=%v value=%v}, want {true 0.9}", got.TopP.Valid(), got.TopP.Value)
	}
	if !got.ParallelToolCalls.Valid() || got.ParallelToolCalls.Value != false {
		t.Errorf("ParallelToolCalls = {valid=%v value=%v}, want {true false}", got.ParallelToolCalls.Valid(), got.ParallelToolCalls.Value)
	}
}

func TestChatCompletionScalarFields_Unset(t *testing.T) {
	t.Parallel()
	p := responses.ResponseNewParams{Model: "gpt-4o"}
	p.Input.OfString = openai.String("hi")

	got, err := convert.ResponsesParamsToChatCompletionParams(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxTokens.Valid() {
		t.Error("MaxTokens should be omitted when MaxOutputTokens is not set")
	}
	if got.Temperature.Valid() {
		t.Error("Temperature should be omitted when not set")
	}
	if got.TopP.Valid() {
		t.Error("TopP should be omitted when not set")
	}
	if got.ParallelToolCalls.Valid() {
		t.Error("ParallelToolCalls should be omitted when not set")
	}
}

// ─── Wire-format regression: function_call items must have RawJSON populated ──

// TestChatCompletionToResponsesResponse_FunctionCallWireFormat verifies that
// function_call output items have JSON.raw populated (via newFunctionCallItem's
// round-trip construction), so that the handler-level marshal helpers can use
// RawJSON() to produce correct wire bytes.
func TestChatCompletionToResponsesResponse_FunctionCallWireFormat(t *testing.T) {
	t.Parallel()
	completion := &openai.ChatCompletion{
		ID:    "chatcmpl_wire",
		Model: "gpt-4o",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
						{
							ID:   "call_wire",
							Type: "function",
							Function: openai.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Berlin"}`,
							},
						},
					},
				},
			},
		},
	}
	got := convert.ChatCompletionToResponsesResponse(completion, nil)
	if len(got.Output) < 2 {
		t.Fatalf("expected at least 2 output items, got %d", len(got.Output))
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
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rj), &raw); err != nil {
			t.Fatalf("unmarshal RawJSON: %v", err)
		}
		argsRaw, ok := raw["arguments"]
		if !ok {
			t.Fatal("arguments key missing from RawJSON")
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
		t.Error("no function_call item found in output")
	}
}

// TestChatCompletionsStreamState_FunctionCallWireFormat verifies that streaming
// bridge function_call items have RawJSON populated correctly.
func TestChatCompletionsStreamState_FunctionCallWireFormat(t *testing.T) {
	t.Parallel()
	state := convert.NewChatCompletionsStreamState(nil)

	chunks := []openai.ChatCompletionChunk{
		{ID: "chatcmpl_ws", Model: "gpt-4o", Choices: []openai.ChatCompletionChunkChoice{
			{Delta: openai.ChatCompletionChunkChoiceDelta{ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
				{Index: 0, ID: "call_ws", Type: "function",
					Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{Name: "calc"}},
			}}},
		}},
		{ID: "chatcmpl_ws", Model: "gpt-4o", Choices: []openai.ChatCompletionChunkChoice{
			{Delta: openai.ChatCompletionChunkChoiceDelta{ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
				{Index: 0, Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{Arguments: `{"x":1}`}},
			}}},
		}},
		{ID: "chatcmpl_ws", Model: "gpt-4o", Choices: []openai.ChatCompletionChunkChoice{
			{FinishReason: "tool_calls"},
		}},
		{ID: "chatcmpl_ws", Model: "gpt-4o", Usage: openai.CompletionUsage{TotalTokens: 15}},
	}

	var completed *responses.ResponseStreamEventUnion
	for _, chunk := range chunks {
		for _, ev := range convert.ChatCompletionChunkToResponsesEvents(chunk, state) {
			if ev.Type == "response.completed" {
				cp := ev
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
			t.Error("RawJSON() is empty for streaming function_call item")
		}
	}
	if !foundFunctionCall {
		t.Error("no function_call item found in response.completed output")
	}
}

// Codex drops an output_item.added message without a content array and then
// rejects every text delta for it.
func TestChatCompletionChunkToResponsesEvents_AddedItemCarriesEmptyContent(t *testing.T) {
	s := convert.NewChatCompletionsStreamState(nil)
	var chunk openai.ChatCompletionChunk
	if err := json.Unmarshal([]byte(`{"id":"c1","model":"m","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"pong"}}]}`), &chunk); err != nil {
		t.Fatal(err)
	}
	for _, ev := range convert.ChatCompletionChunkToResponsesEvents(chunk, s) {
		if ev.Type != "response.output_item.added" {
			continue
		}
		raw, err := json.Marshal(ev.Item)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"content":[]`) {
			t.Errorf("added item = %s, want an empty content array", raw)
		}
		return
	}
	t.Fatal("no response.output_item.added event")
}
