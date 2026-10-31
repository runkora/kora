package convert_test

import (
	"encoding/json"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/runkora/kora/backend/internal/inferencegw/providers/convert"
)

// breakpoints reports where cache_control appears in the marshalled request.
func breakpoints(t *testing.T, p anthropic.MessageNewParams) (system, tools, messages int) {
	t.Helper()

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var body struct {
		System   []map[string]any `json:"system"`
		Tools    []map[string]any `json:"tools"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	marked := func(blocks []map[string]any) int {
		n := 0
		for _, b := range blocks {
			if _, ok := b["cache_control"]; ok {
				n++
			}
		}
		return n
	}
	system = marked(body.System)
	tools = marked(body.Tools)
	for _, m := range body.Messages {
		messages += marked(m.Content)
	}
	return system, tools, messages
}

// Claude reached through the Responses or OpenAI dialects was paying full input
// rate on every token: neither dialect can express cache_control, and neither
// converter set one, so no prefix was ever cached.
// See https://github.com/runkora/kora/pull/205.
func TestResponsesParamsToAnthropicParams_CacheBreakpoints(t *testing.T) {
	t.Parallel()

	params := responses.ResponseNewParams{
		Model:        "claude-sonnet-4-6",
		Instructions: openai.String("You are a helpful assistant."),
	}
	params.Input.OfString = openai.String(strings.Repeat("context ", 2000))
	params.Tools = []responses.ToolUnionParam{
		{OfFunction: &responses.FunctionToolParam{Name: "lookup"}},
	}

	got, err := convert.ResponsesParamsToAnthropicParams(params)
	if err != nil {
		t.Fatalf("ResponsesParamsToAnthropicParams: %v", err)
	}

	system, tools, messages := breakpoints(t, got)
	if system != 1 || tools != 1 || messages != 1 {
		t.Errorf("breakpoints: system=%d tools=%d messages=%d, want 1 each", system, tools, messages)
	}
}

func TestOpenAIParamsToAnthropicParams_CacheBreakpoints(t *testing.T) {
	t.Parallel()

	got := convert.OpenAIParamsToAnthropicParams(openai.ChatCompletionNewParams{
		Model: "claude-sonnet-4-6",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a helpful assistant."),
			openai.UserMessage(strings.Repeat("context ", 2000)),
		},
	})

	system, _, messages := breakpoints(t, got)
	if system != 1 || messages != 1 {
		t.Errorf("breakpoints: system=%d messages=%d, want 1 each", system, messages)
	}
}

// Anthropic rejects a request carrying more than four breakpoints, so the
// placement must not scale with the conversation.
func TestPromptCaching_StaysUnderTheBreakpointLimit(t *testing.T) {
	t.Parallel()

	msgs := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage("System.")}
	for range 20 {
		msgs = append(msgs, openai.UserMessage("Question."), openai.AssistantMessage("Answer."))
	}

	system, tools, messages := breakpoints(t, convert.OpenAIParamsToAnthropicParams(openai.ChatCompletionNewParams{
		Model:    "claude-sonnet-4-6",
		Messages: msgs,
	}))
	if total := system + tools + messages; total > 4 {
		t.Errorf("%d breakpoints across a 40-turn conversation, Anthropic allows 4", total)
	}
}

// A cache-write turn reports nearly all of its prompt under
// cache_creation_input_tokens. Responses has no field for that bucket, so the
// bridge counts those tokens as input rather than losing them: they were read
// from the request either way, and dropping them bills an 11k-token prompt as
// a 2-token one.
func TestAnthropicMessageToResponsesResponse_CacheWriteCountsAsInput(t *testing.T) {
	t.Parallel()

	got := convert.AnthropicMessageToResponsesResponse(&anthropic.Message{
		ID:         "msg_write",
		Model:      "claude-sonnet-4-6",
		Role:       "assistant",
		Type:       "message",
		Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "ok"}},
		StopReason: anthropic.StopReasonEndTurn,
		Usage: anthropic.Usage{
			InputTokens:              2,
			OutputTokens:             4,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 11215,
		},
	}, nil)

	if got.Usage.InputTokens != 11217 {
		t.Errorf("InputTokens = %d, want 11217 (uncached 2 + cache write 11215)", got.Usage.InputTokens)
	}
	if got.Usage.InputTokensDetails.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0: a write is not a read", got.Usage.InputTokensDetails.CachedTokens)
	}
	if got.Usage.TotalTokens != 11221 {
		t.Errorf("TotalTokens = %d, want 11221", got.Usage.TotalTokens)
	}
}

func TestAnthropicStreamToResponses_CacheWriteCountsAsInput(t *testing.T) {
	t.Parallel()

	state := convert.NewAnthropicResponsesStreamState(nil)
	events := []anthropic.MessageStreamEventUnion{
		{
			Type: "message_start",
			Message: anthropic.Message{
				ID: "msg_write_stream", Model: "claude-sonnet-4-6", Role: "assistant", Type: "message",
				Usage: anthropic.Usage{InputTokens: 2, CacheCreationInputTokens: 11215},
			},
		},
		{Type: "content_block_start", Index: 0, ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{Type: "text"}},
		{Type: "content_block_delta", Index: 0, Delta: anthropic.MessageStreamEventUnionDelta{Type: "text_delta", Text: "ok"}},
		{Type: "message_delta", Delta: anthropic.MessageStreamEventUnionDelta{StopReason: anthropic.StopReasonEndTurn}, Usage: anthropic.MessageDeltaUsage{OutputTokens: 4}},
		{Type: "message_stop"},
	}

	var completed *responses.ResponseStreamEventUnion
	for _, ev := range events {
		for _, out := range convert.AnthropicEventToResponsesEvents(ev, state) {
			if out.Type == "response.completed" {
				e := out
				completed = &e
			}
		}
	}
	if completed == nil {
		t.Fatal("expected a response.completed event")
	}
	if completed.Response.Usage.InputTokens != 11217 {
		t.Errorf("InputTokens = %d, want 11217 (uncached 2 + cache write 11215)", completed.Response.Usage.InputTokens)
	}
	if completed.Response.Usage.InputTokensDetails.CachedTokens != 0 {
		t.Errorf("CachedTokens = %d, want 0: a write is not a read", completed.Response.Usage.InputTokensDetails.CachedTokens)
	}
}

// The non-streaming chat conversion has no side channel to carry cache tokens
// to billing, so both buckets have to reach it through prompt_tokens.
func TestAnthropicMessageToOpenAI_CacheTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		read, write            int64
		wantPrompt, wantCached int64
	}{
		{"cache read turn", 11215, 0, 11217, 11215},
		{"cache write turn", 0, 11215, 11217, 0},
		{"no caching", 0, 0, 2, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convert.AnthropicMessageToOpenAI(&anthropic.Message{
				ID:         "msg_1",
				Model:      "claude-sonnet-4-6",
				Role:       "assistant",
				Type:       "message",
				Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "ok"}},
				StopReason: anthropic.StopReasonEndTurn,
				Usage: anthropic.Usage{
					InputTokens:              2,
					OutputTokens:             4,
					CacheReadInputTokens:     tc.read,
					CacheCreationInputTokens: tc.write,
				},
			}, 0)

			if got.Usage.PromptTokens != tc.wantPrompt {
				t.Errorf("PromptTokens = %d, want %d", got.Usage.PromptTokens, tc.wantPrompt)
			}
			if got.Usage.PromptTokensDetails.CachedTokens != tc.wantCached {
				t.Errorf("CachedTokens = %d, want %d", got.Usage.PromptTokensDetails.CachedTokens, tc.wantCached)
			}
			if want := tc.wantPrompt + 4; got.Usage.TotalTokens != want {
				t.Errorf("TotalTokens = %d, want %d", got.Usage.TotalTokens, want)
			}
		})
	}
}
