package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"

	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
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
		responses.ResponseInputItemParamOfFunctionCallOutput("call_1", `{"temp":"15C"}`),
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
					ToolCalls: []openai.ChatCompletionMessageToolCall{
						{
							ID:   "call_x",
							Type: "function",
							Function: openai.ChatCompletionMessageToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
	}
	got := convert.ChatCompletionToResponsesResponse(completion)
	if len(got.Output) != 2 {
		t.Fatalf("len(Output) = %d, want 2 (message + function_call)", len(got.Output))
	}
	if got.Output[1].Type != "function_call" {
		t.Errorf("Output[1].Type = %q, want function_call", got.Output[1].Type)
	}
	if got.Output[1].Name != "get_weather" {
		t.Errorf("Output[1].Name = %q, want get_weather", got.Output[1].Name)
	}
	if got.Output[1].Arguments != `{"city":"Paris"}` {
		t.Errorf("Output[1].Arguments = %q", got.Output[1].Arguments)
	}
}

// ─── Streaming / non-streaming parity ────────────────────────────────────────

// TestChatCompletionsStreaming_ToolCallEmittedInTerminalEvent asserts that
// streaming tool-call deltas are reconstructed in the terminal response.completed
// event so the streaming and non-streaming bridge outputs agree on shape.
func TestChatCompletionsStreaming_ToolCallEmittedInTerminalEvent(t *testing.T) {
	t.Parallel()
	state := convert.NewChatCompletionsStreamState()

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
			if item.Arguments != `{"city":"Berlin"}` {
				t.Errorf("Arguments = %q, want accumulated JSON", item.Arguments)
			}
		}
	}
	if !gotCall {
		t.Errorf("function_call not found in completed output: %+v", completed.Response.Output)
	}
}
