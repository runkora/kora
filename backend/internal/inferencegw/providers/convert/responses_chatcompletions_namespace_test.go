package convert_test

import (
	"encoding/json"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
)

const chatNamespaceRequest = `{
  "model": "openai/gpt-5",
  "input": [
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "provision a sandbox"}]},
    {"type": "function_call", "call_id": "call_1", "namespace": "mcp__runtime", "name": "list_environments", "arguments": "{}"},
    {"type": "function_call_output", "call_id": "call_1", "output": "gitops-base"}
  ],
  "tools": [
    {"type": "function", "name": "shell_command", "parameters": {"type": "object", "properties": {}}},
    {"type": "namespace", "name": "mcp__runtime", "description": "Tools in the mcp__runtime namespace.", "tools": [
      {"type": "function", "name": "provision_sandbox", "description": "Starts a sandbox.", "parameters": {"type": "object", "properties": {}}}
    ]}
  ]
}`

func chatNamespaceParams(t *testing.T) responses.ResponseNewParams {
	t.Helper()
	var params responses.ResponseNewParams
	if err := json.Unmarshal([]byte(chatNamespaceRequest), &params); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return params
}

// A namespace's functions reach Chat Completions under their joined name, and
// a namespaced call in the history keeps that name.
func TestResponsesParamsToChatCompletionParams_NamespaceTools(t *testing.T) {
	t.Parallel()
	got, err := convert.ResponsesParamsToChatCompletionParams(chatNamespaceParams(t))
	if err != nil {
		t.Fatalf("ResponsesParamsToChatCompletionParams: %v", err)
	}
	var names []string
	for _, tool := range got.Tools {
		names = append(names, tool.OfFunction.Function.Name)
	}
	if len(names) != 2 || names[0] != "shell_command" || names[1] != "mcp__runtime__provision_sandbox" {
		t.Errorf("tools = %v, want [shell_command mcp__runtime__provision_sandbox]", names)
	}
	var called string
	for _, msg := range got.Messages {
		if msg.OfAssistant != nil {
			called = msg.OfAssistant.ToolCalls[0].OfFunction.Function.Name
		}
	}
	if called != "mcp__runtime__list_environments" {
		t.Errorf("history tool call = %q, want mcp__runtime__list_environments", called)
	}
}

// A tool call for a namespaced function returns as a function_call with its
// namespace and name, which is how Codex finds the tool.
func TestChatCompletionToolCall_NamespaceRestored(t *testing.T) {
	t.Parallel()
	names := convert.ResponsesToolNames(chatNamespaceParams(t).Tools)

	var completion openai.ChatCompletion
	raw := `{"id":"cc_1","model":"m","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[
	  {"id":"call_ns","type":"function","function":{"name":"mcp__runtime__provision_sandbox","arguments":"{}"}},
	  {"id":"call_sh","type":"function","function":{"name":"shell_command","arguments":"{}"}}]}}]}`
	if err := json.Unmarshal([]byte(raw), &completion); err != nil {
		t.Fatal(err)
	}
	got := convert.ChatCompletionToResponsesResponse(&completion, names)
	if ns, name := chatFunctionCallNames(t, got.Output[1]); ns != "mcp__runtime" || name != "provision_sandbox" {
		t.Errorf("namespaced call = (%q, %q), want (mcp__runtime, provision_sandbox)", ns, name)
	}
	if ns, name := chatFunctionCallNames(t, got.Output[2]); ns != "" || name != "shell_command" {
		t.Errorf("plain call = (%q, %q), want (\"\", shell_command)", ns, name)
	}

	state := convert.NewChatCompletionsStreamState(names)
	var chunk openai.ChatCompletionChunk
	chunkRaw := `{"id":"cc_1","model":"m","choices":[{"finish_reason":"tool_calls","delta":{"tool_calls":[
	  {"index":0,"id":"call_ns","function":{"name":"mcp__runtime__provision_sandbox","arguments":"{}"}}]}}]}`
	if err := json.Unmarshal([]byte(chunkRaw), &chunk); err != nil {
		t.Fatal(err)
	}
	convert.ChatCompletionChunkToResponsesEvents(chunk, state)
	done := state.FlushPendingCompletion(1, 1, 2)
	if done == nil || len(done.Response.Output) < 2 {
		t.Fatalf("no completed tool call: %+v", done)
	}
	if ns, name := chatFunctionCallNames(t, done.Response.Output[1]); ns != "mcp__runtime" || name != "provision_sandbox" {
		t.Errorf("streamed call = (%q, %q), want (mcp__runtime, provision_sandbox)", ns, name)
	}
}

// chatFunctionCallNames returns the namespace and name a function_call item carries on the wire.
func chatFunctionCallNames(t *testing.T, item responses.ResponseOutputItemUnion) (namespace, name string) {
	t.Helper()
	var wire struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal([]byte(item.RawJSON()), &wire); err != nil {
		t.Fatalf("unmarshal function_call: %v", err)
	}
	return wire.Namespace, wire.Name
}
