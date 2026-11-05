package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
)

// functionOutputRequest builds a Codex-shaped request whose tool result has the given output.
func functionOutputRequest(t *testing.T, output string) responses.ResponseNewParams {
	t.Helper()
	body := `{
  "model": "openai/gpt-5",
  "input": [
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "add 2 and 3"}]},
    {"type": "function_call", "call_id": "call_1", "namespace": "mcp__everything", "name": "get_sum", "arguments": "{\"a\":2,\"b\":3}"},
    {"type": "function_call_output", "call_id": "call_1", "output": ` + output + `}
  ]
}`
	var params responses.ResponseNewParams
	if err := json.Unmarshal([]byte(body), &params); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return params
}

// Codex sends an MCP tool result as a list of content parts.
const listOutput = `[{"type": "input_text", "text": "Wall time: 0.0014 seconds\n"}, {"type": "input_text", "text": "Output: 5"}]`

const imageOnlyOutput = `[{"type": "input_image", "image_url": "data:image/png;base64,AAAA"}]`

func TestFunctionCallOutputList_ChatCompletions(t *testing.T) {
	t.Parallel()
	got, err := convert.ResponsesParamsToChatCompletionParams(functionOutputRequest(t, listOutput))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	b, _ := json.Marshal(got.Messages)
	if !strings.Contains(string(b), `"role":"tool"`) || !strings.Contains(string(b), `Output: 5`) {
		t.Errorf("tool message missing or without the list's text: %s", b)
	}
}

func TestFunctionCallOutputList_Anthropic(t *testing.T) {
	t.Parallel()
	got, err := convert.ResponsesParamsToAnthropicParams(functionOutputRequest(t, listOutput))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	b, _ := json.Marshal(got.Messages)
	if !strings.Contains(string(b), `"tool_result"`) || !strings.Contains(string(b), `Output: 5`) {
		t.Errorf("tool_result missing or without the list's text: %s", b)
	}
}

func TestFunctionCallOutputImageOnly(t *testing.T) {
	t.Parallel()
	if _, err := convert.ResponsesParamsToAnthropicParams(functionOutputRequest(t, imageOnlyOutput)); err == nil {
		t.Error("Anthropic: want an error for a tool result with no text")
	}
	got, err := convert.ResponsesParamsToChatCompletionParams(functionOutputRequest(t, imageOnlyOutput))
	if err != nil {
		t.Fatalf("chat completions convert: %v", err)
	}
	b, _ := json.Marshal(got.Messages)
	if strings.Contains(string(b), `"role":"tool"`) {
		t.Errorf("chat completions: want no tool message for a result with no text: %s", b)
	}
}
