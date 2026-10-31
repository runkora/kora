package convert_test

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
)

// codexNamespaceRequest is the shape Codex sends for an MCP server's tools: a
// namespace tool holding function tools, beside plain function tools.
const codexNamespaceRequest = `{
  "model": "anthropic/claude-sonnet-5",
  "input": [
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "provision a sandbox"}]},
    {"type": "function_call", "call_id": "toolu_1", "namespace": "mcp__runtime", "name": "list_environments", "arguments": "{}"},
    {"type": "function_call_output", "call_id": "toolu_1", "output": "gitops-base"}
  ],
  "tools": [
    {"type": "function", "name": "shell_command", "parameters": {"type": "object", "properties": {}}},
    {"type": "namespace", "name": "mcp__runtime", "description": "Tools in the mcp__runtime namespace.", "tools": [
      {"type": "function", "name": "provision_sandbox", "description": "Starts a sandbox.", "parameters": {"type": "object", "properties": {"environment": {"type": "string"}}}}
    ]}
  ]
}`

func codexParams(t *testing.T) responses.ResponseNewParams {
	t.Helper()
	var params responses.ResponseNewParams
	if err := json.Unmarshal([]byte(codexNamespaceRequest), &params); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return params
}

// A namespace's functions reach Anthropic under their joined name, and a
// namespaced call in the history keeps that name.
func TestResponsesParamsToAnthropicParams_NamespaceTools(t *testing.T) {
	t.Parallel()
	got, err := convert.ResponsesParamsToAnthropicParams(codexParams(t))
	if err != nil {
		t.Fatalf("ResponsesParamsToAnthropicParams: %v", err)
	}
	var names []string
	for _, tool := range got.Tools {
		names = append(names, tool.OfTool.Name)
	}
	if len(names) != 2 || names[0] != "shell_command" || names[1] != "mcp__runtime__provision_sandbox" {
		t.Errorf("tools = %v, want [shell_command mcp__runtime__provision_sandbox]", names)
	}
	var toolUse string
	for _, msg := range got.Messages {
		for _, block := range msg.Content {
			if block.OfToolUse != nil {
				toolUse = block.OfToolUse.Name
			}
		}
	}
	if toolUse != "mcp__runtime__list_environments" {
		t.Errorf("history tool_use name = %q, want mcp__runtime__list_environments", toolUse)
	}
}

// functionCallNames returns the namespace and name a function_call item carries on the wire.
func functionCallNames(t *testing.T, item responses.ResponseOutputItemUnion) (namespace, name string) {
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

// A tool_use for a namespaced function returns to the client as a function_call
// with its namespace and name, which is how Codex finds the tool.
func TestAnthropicToolUse_NamespaceRestored(t *testing.T) {
	t.Parallel()
	names := convert.ResponsesToolNames(codexParams(t).Tools)

	msg := &anthropic.Message{
		ID: "msg_ns",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "toolu_ns", Name: "mcp__runtime__provision_sandbox", Input: json.RawMessage(`{}`)},
			{Type: "tool_use", ID: "toolu_sh", Name: "shell_command", Input: json.RawMessage(`{}`)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToResponsesResponse(msg, names)
	if ns, name := functionCallNames(t, got.Output[0]); ns != "mcp__runtime" || name != "provision_sandbox" {
		t.Errorf("namespaced call = (%q, %q), want (mcp__runtime, provision_sandbox)", ns, name)
	}
	if ns, name := functionCallNames(t, got.Output[1]); ns != "" || name != "shell_command" {
		t.Errorf("plain call = (%q, %q), want (\"\", shell_command)", ns, name)
	}

	state := convert.NewAnthropicResponsesStreamState(names)
	events := []anthropic.MessageStreamEventUnion{
		{Type: "message_start", Message: anthropic.Message{ID: "msg_ns"}},
		{Type: "content_block_start", Index: 0, ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "tool_use", ID: "toolu_ns", Name: "mcp__runtime__provision_sandbox"}},
		{Type: "content_block_stop", Index: 0},
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{Type: "message_delta", StopReason: anthropic.StopReasonToolUse}},
		{Type: "message_stop"},
	}
	var checked int
	for _, ev := range events {
		for _, out := range convert.AnthropicEventToResponsesEvents(ev, state) {
			items := []responses.ResponseOutputItemUnion{out.Item}
			if out.Type == "response.completed" {
				items = out.Response.Output
			}
			for _, item := range items {
				if item.Type != "function_call" {
					continue
				}
				checked++
				if ns, name := functionCallNames(t, item); ns != "mcp__runtime" || name != "provision_sandbox" {
					t.Errorf("%s: call = (%q, %q), want (mcp__runtime, provision_sandbox)", out.Type, ns, name)
				}
			}
		}
	}
	if checked < 3 {
		t.Errorf("checked %d function_call items in the stream, want added, done and completed", checked)
	}
}

// A joined name over Anthropic's 64-character limit is shortened, and a call to
// it still returns with the original namespace and name.
func TestNamespaceTools_LongNameShortened(t *testing.T) {
	t.Parallel()
	const namespace = "mcp__a_very_long_server_name_for_an_internal_system"
	const name = "create_an_extremely_specific_record"
	var params responses.ResponseNewParams
	body := `{"model":"m","input":"hi","tools":[{"type":"namespace","name":"` + namespace + `","description":"d","tools":[{"type":"function","name":"` + name + `","parameters":{"type":"object","properties":{}}}]}]}`
	if err := json.Unmarshal([]byte(body), &params); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("ResponsesParamsToAnthropicParams: %v", err)
	}
	sent := got.Tools[0].OfTool.Name
	if len(sent) > 64 {
		t.Fatalf("tool name %q is %d characters, want at most 64", sent, len(sent))
	}
	msg := &anthropic.Message{
		Content:    []anthropic.ContentBlockUnion{{Type: "tool_use", ID: "toolu_l", Name: sent, Input: json.RawMessage(`{}`)}},
		StopReason: anthropic.StopReasonToolUse,
	}
	resp := convert.AnthropicMessageToResponsesResponse(msg, convert.ResponsesToolNames(params.Tools))
	if ns, n := functionCallNames(t, resp.Output[0]); ns != namespace || n != name {
		t.Errorf("call = (%q, %q), want (%q, %q)", ns, n, namespace, name)
	}
}
