package compatible

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	ip "github.com/clear-route/orbit/internal/platform/inferenceproviders/v1alpha1"
)

// claudeCodeTurn has the role sequence of a request on Claude Code's
// mid-conversation-system beta: every turn ends with a system message.
const claudeCodeTurn = `{"model":"vendor/open-model","max_tokens":1024,"messages":[
{"role":"user","content":[{"type":"text","text":"Task."}]},
{"role":"system","content":"System note one."},
{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Bash","input":{"command":"ls"}}]},
{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"Result one."}]},
{"role":"system","content":"System note two."},
{"role":"assistant","content":[{"type":"tool_use","id":"call_2","name":"Bash","input":{"command":"pwd"}}]},
{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_2","content":"Result two."}]},
{"role":"system","content":[{"type":"text","text":"System note three.","cache_control":{"type":"ephemeral"}}]}
]}`

func decodeParams(t *testing.T, body, model string) anthropic.MessageNewParams {
	t.Helper()
	var p anthropic.MessageNewParams
	if err := p.UnmarshalJSON([]byte(body)); err != nil {
		t.Fatal(err)
	}
	p.Model = model
	return p
}

func roles(msgs []anthropic.MessageParam) []anthropic.MessageParamRole {
	out := make([]anthropic.MessageParamRole, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Role)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFoldTrailingSystem_FoldsIntoTheLastUserTurn(t *testing.T) {
	in := decodeParams(t, claudeCodeTurn, "vendor/open-model")
	before := mustJSON(t, in)

	out := foldTrailingSystem(in)

	want := []anthropic.MessageParamRole{"user", "system", "assistant", "user", "system", "assistant", "user"}
	if got := roles(out.Messages); !slices.Equal(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	last := out.Messages[len(out.Messages)-1].Content
	if len(last) != 2 || last[0].OfToolResult == nil || last[1].OfText == nil {
		t.Fatalf("last user turn = %s, want the tool result then a text block", mustJSON(t, last))
	}
	if got := last[1].OfText.Text; got != "System note three." {
		t.Errorf("folded text = %q", got)
	}
	if mustJSON(t, in) != before {
		t.Error("the caller's params were modified")
	}
}

func TestFoldTrailingSystem_LeavesRequestsUnchanged(t *testing.T) {
	noTrailing := decodeParams(t, claudeCodeTurn, "vendor/open-model")
	noTrailing.Messages = noTrailing.Messages[:len(noTrailing.Messages)-1]
	afterAssistant := decodeParams(t, claudeCodeTurn, "vendor/open-model")
	afterAssistant.Messages = slices.Delete(afterAssistant.Messages, 6, 7)

	tests := []struct {
		name   string
		params anthropic.MessageNewParams
	}{
		{"no trailing system message", noTrailing},
		{"trailing system message after an assistant turn", afterAssistant},
		{"Anthropic model with a vendor prefix", decodeParams(t, claudeCodeTurn, "anthropic/claude-sonnet-4-5")},
		{"Anthropic model without a prefix", decodeParams(t, claudeCodeTurn, "claude-sonnet-4-5")},
		{"Anthropic model behind a proxy prefix", decodeParams(t, claudeCodeTurn, "bedrock/anthropic.claude-sonnet-4-5")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := mustJSON(t, foldTrailingSystem(tt.params)), mustJSON(t, tt.params); got != want {
				t.Errorf("request changed:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// The upstream receives the folded request on both messages paths.
func TestMessagesForwardFoldedRequest(t *testing.T) {
	c, up := setup(t, "k", ip.APIMessages)
	ctx := context.Background()
	params := decodeParams(t, claudeCodeTurn, "vendor/open-model")

	if _, err := c.AnthropicMessages(ctx, params); err != nil {
		t.Fatal(err)
	}
	events, err := c.AnthropicMessagesStream(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}

	if len(up.seen) != 2 {
		t.Fatalf("upstream saw %d requests, want 2", len(up.seen))
	}
	for i, r := range up.seen {
		var sent struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(r.body), &sent); err != nil {
			t.Fatal(err)
		}
		if n := len(sent.Messages); n != 7 || sent.Messages[n-1].Role != "user" {
			t.Errorf("request %d sent %+v, want 7 messages ending with user", i, sent.Messages)
		}
	}
}
