package convert_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

// newAnthropicAPIError constructs a valid *anthropic.Error with the given HTTP
// status code. anthropic.Error.Error() dereferences Request and Response, so
// we must supply non-nil values to avoid a nil-pointer panic in tests.
func newAnthropicAPIError(statusCode int) *anthropic.Error {
	req := &http.Request{
		Method: "POST",
		URL:    &url.URL{Scheme: "https", Host: "api.anthropic.com", Path: "/v1/messages"},
	}
	resp := &http.Response{StatusCode: statusCode}
	return &anthropic.Error{StatusCode: statusCode, Request: req, Response: resp}
}

// ─── AnthropicMessageToOpenAI ─────────────────────────────────────────────────

func TestAnthropicMessageToOpenAI_TextContent(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_123",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "Hello, world!"},
		},
		StopReason: anthropic.StopReasonEndTurn,
		Usage:      anthropic.Usage{InputTokens: 10, OutputTokens: 5},
	}
	createdAt := time.Now().Unix()
	got := convert.AnthropicMessageToOpenAI(msg, createdAt)

	if got.ID != "msg_123" {
		t.Errorf("ID = %q, want %q", got.ID, "msg_123")
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-sonnet-4-6")
	}
	if got.Created != createdAt {
		t.Errorf("Created = %d, want %d", got.Created, createdAt)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(got.Choices))
	}
	if got.Choices[0].Message.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", got.Choices[0].Message.Content, "Hello, world!")
	}
	if got.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", got.Choices[0].FinishReason)
	}
	if got.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", got.Usage.PromptTokens)
	}
	if got.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", got.Usage.CompletionTokens)
	}
	if got.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", got.Usage.TotalTokens)
	}
}

func TestAnthropicMessageToOpenAI_ToolUse(t *testing.T) {
	t.Parallel()
	inputJSON, _ := json.Marshal(map[string]string{"city": "London"})
	msg := &anthropic.Message{
		ID:    "msg_tool",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "call_abc", Name: "get_weather", Input: json.RawMessage(inputJSON)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToOpenAI(msg, time.Now().Unix())

	if len(got.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(got.Choices))
	}
	if got.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", got.Choices[0].FinishReason)
	}
	if len(got.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(got.Choices[0].Message.ToolCalls))
	}
	tc := got.Choices[0].Message.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want call_abc", tc.ID)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("ToolCall.Function.Name = %q, want get_weather", tc.Function.Name)
	}
	if tc.Function.Arguments != string(inputJSON) {
		t.Errorf("ToolCall.Function.Arguments = %q, want %q", tc.Function.Arguments, string(inputJSON))
	}
}

func TestAnthropicMessageToOpenAI_MultipleTextBlocks(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:    "msg_multi",
		Model: "claude-3",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "First. "},
			{Type: "text", Text: "Second."},
		},
		StopReason: anthropic.StopReasonEndTurn,
	}
	got := convert.AnthropicMessageToOpenAI(msg, time.Now().Unix())
	if got.Choices[0].Message.Content != "First. Second." {
		t.Errorf("Content = %q, want %q", got.Choices[0].Message.Content, "First. Second.")
	}
}

func TestAnthropicMessageToOpenAI_ObjectField(t *testing.T) {
	t.Parallel()
	msg := &anthropic.Message{
		ID:         "msg_obj",
		Model:      "claude-sonnet-4-6",
		Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "hi"}},
		StopReason: anthropic.StopReasonEndTurn,
	}
	got := convert.AnthropicMessageToOpenAI(msg, time.Now().Unix())
	if string(got.Object) != "chat.completion" {
		t.Errorf("Object = %q, want \"chat.completion\"", got.Object)
	}
}

func TestAnthropicMessageToOpenAI_ToolOnlyContentIsEmpty(t *testing.T) {
	t.Parallel()
	// When the response contains only a tool call, Content should be "" (not set),
	// not the empty-string sentinel that fails strict validators.
	inputJSON, _ := json.Marshal(map[string]string{"q": "weather"})
	msg := &anthropic.Message{
		ID:    "msg_tool_only",
		Model: "claude-sonnet-4-6",
		Content: []anthropic.ContentBlockUnion{
			{Type: "tool_use", ID: "call_1", Name: "search", Input: json.RawMessage(inputJSON)},
		},
		StopReason: anthropic.StopReasonToolUse,
	}
	got := convert.AnthropicMessageToOpenAI(msg, time.Now().Unix())
	if got.Choices[0].Message.Content != "" {
		t.Errorf("Content = %q, want empty string for tool-only response", got.Choices[0].Message.Content)
	}
}

func TestAnthropicMessageToOpenAI_CreatedAtIsPreserved(t *testing.T) {
	t.Parallel()
	// createdAt should reflect when the API call was made, not time.Now() at conversion.
	msg := &anthropic.Message{ID: "x", StopReason: anthropic.StopReasonEndTurn}
	ts := int64(1700000000)
	got := convert.AnthropicMessageToOpenAI(msg, ts)
	if got.Created != ts {
		t.Errorf("Created = %d, want %d (should use provided timestamp, not time.Now())", got.Created, ts)
	}
}

// ─── mapAnthropicStopReason (tested via AnthropicMessageToOpenAI) ─────────────

func TestStopReasonMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		reason   anthropic.StopReason
		expected string
	}{
		{anthropic.StopReasonEndTurn, "stop"},
		{anthropic.StopReasonMaxTokens, "length"},
		{anthropic.StopReasonStopSequence, "stop"},
		{anthropic.StopReasonToolUse, "tool_calls"},
		{"unknown_reason", "stop"},
	}
	for _, c := range cases {
		msg := &anthropic.Message{
			ID:         "x",
			StopReason: c.reason,
			Content:    []anthropic.ContentBlockUnion{{Type: "text", Text: "hi"}},
		}
		got := convert.AnthropicMessageToOpenAI(msg, 0)
		if got.Choices[0].FinishReason != c.expected {
			t.Errorf("StopReason(%q): got %q, want %q", c.reason, got.Choices[0].FinishReason, c.expected)
		}
	}
}

// ─── AnthropicEventToOpenAIChunk ─────────────────────────────────────────────

func TestAnthropicEventToOpenAIChunk_MessageStart(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	ev := anthropic.MessageStreamEventUnion{
		Type: "message_start",
		Message: anthropic.Message{
			ID:    "msg_stream",
			Model: "claude-sonnet-4-6",
			Usage: anthropic.Usage{InputTokens: 20},
		},
	}
	chunk, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
	if !ok {
		t.Fatal("expected chunk to be emitted for message_start")
	}
	if chunk.ID != "msg_stream" {
		t.Errorf("ID = %q, want msg_stream", chunk.ID)
	}
	// Usage is deferred to message_delta; message_start chunk carries no token counts.
	if chunk.Usage.PromptTokens != 0 {
		t.Errorf("PromptTokens = %d, want 0 (deferred to message_delta)", chunk.Usage.PromptTokens)
	}
	// Input tokens are captured in state for emission in the combined usage chunk.
	if s.InputTokens != 20 {
		t.Errorf("state.InputTokens = %d, want 20", s.InputTokens)
	}
	if s.MessageID != "msg_stream" {
		t.Errorf("state.MessageID = %q, want msg_stream", s.MessageID)
	}
	if s.Model != "claude-sonnet-4-6" {
		t.Errorf("state.Model = %q, want claude-sonnet-4-6", s.Model)
	}
	// OpenAI spec requires choices to be an array, never null — strict validators
	// (e.g. the openai npm package's Zod schemas) reject null on this field.
	raw, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if string(m["choices"]) != "[]" {
		t.Errorf("choices JSON = %s, want []", m["choices"])
	}
}

func TestAnthropicEventToOpenAIChunk_TextDelta(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	s.MessageID = "msg_1"
	s.Model = "claude-3"
	ev := anthropic.MessageStreamEventUnion{
		Type: "content_block_delta",
		Delta: anthropic.MessageStreamEventUnionDelta{
			Type: "text_delta",
			Text: "Hello",
		},
	}
	chunk, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
	if !ok {
		t.Fatal("expected chunk for text_delta")
	}
	if len(chunk.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if chunk.Choices[0].Delta.Content != "Hello" {
		t.Errorf("Content = %q, want Hello", chunk.Choices[0].Delta.Content)
	}
}

func TestAnthropicEventToOpenAIChunk_ToolCallStreaming(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	s.MessageID = "msg_1"

	// content_block_start for tool_use assigns a slot index.
	startEv := anthropic.MessageStreamEventUnion{
		Type:  "content_block_start",
		Index: 0,
		ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{
			Type: "tool_use",
			ID:   "call_xyz",
			Name: "search",
		},
	}
	chunk, ok := convert.AnthropicEventToOpenAIChunk(startEv, s)
	if !ok {
		t.Fatal("expected chunk for content_block_start tool_use")
	}
	if len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.Choices[0].Delta.ToolCalls))
	}
	if chunk.Choices[0].Delta.ToolCalls[0].ID != "call_xyz" {
		t.Errorf("ToolCall.ID = %q, want call_xyz", chunk.Choices[0].Delta.ToolCalls[0].ID)
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Function.Name != "search" {
		t.Errorf("ToolCall.Name = %q, want search", chunk.Choices[0].Delta.ToolCalls[0].Function.Name)
	}

	// input_json_delta streams argument fragments.
	deltaEv := anthropic.MessageStreamEventUnion{
		Type:  "content_block_delta",
		Index: 0,
		Delta: anthropic.MessageStreamEventUnionDelta{
			Type:        "input_json_delta",
			PartialJSON: `{"q":`,
		},
	}
	deltaChunk, ok := convert.AnthropicEventToOpenAIChunk(deltaEv, s)
	if !ok {
		t.Fatal("expected chunk for input_json_delta")
	}
	if deltaChunk.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"q":` {
		t.Errorf("Arguments = %q, want %q", deltaChunk.Choices[0].Delta.ToolCalls[0].Function.Arguments, `{"q":`)
	}
}

func TestAnthropicEventToOpenAIChunk_MessageDelta(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	s.MessageID = "msg_1"
	s.InputTokens = 20 // captured from message_start in real streams
	ev := anthropic.MessageStreamEventUnion{
		Type: "message_delta",
		Delta: anthropic.MessageStreamEventUnionDelta{
			StopReason: anthropic.StopReasonEndTurn,
		},
		Usage: anthropic.MessageDeltaUsage{OutputTokens: 30},
	}
	chunk, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
	if !ok {
		t.Fatal("expected chunk for message_delta")
	}
	// message_delta emits the combined usage chunk with both prompt and completion.
	if chunk.Usage.PromptTokens != 20 {
		t.Errorf("PromptTokens = %d, want 20 (carried from state.InputTokens)", chunk.Usage.PromptTokens)
	}
	if chunk.Usage.CompletionTokens != 30 {
		t.Errorf("CompletionTokens = %d, want 30", chunk.Usage.CompletionTokens)
	}
	if chunk.Usage.TotalTokens != 50 {
		t.Errorf("TotalTokens = %d, want 50 (20+30)", chunk.Usage.TotalTokens)
	}
	if chunk.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", chunk.Choices[0].FinishReason)
	}
}

// TestAnthropicEventToOpenAIChunk_CombinedUsage verifies that the full
// message_start → message_delta sequence surfaces all token counts, including
// cache read tokens in PromptTokensDetails.CachedTokens (used by ccusage/OpenCode).
func TestAnthropicEventToOpenAIChunk_CombinedUsage(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()

	start := anthropic.MessageStreamEventUnion{
		Type: "message_start",
		Message: anthropic.Message{
			ID:    "msg_1",
			Model: "claude-sonnet-4-5",
			Usage: anthropic.Usage{
				InputTokens:              100,
				CacheReadInputTokens:     40,
				CacheCreationInputTokens: 60,
			},
		},
	}
	startChunk, ok := convert.AnthropicEventToOpenAIChunk(start, s)
	if !ok {
		t.Fatal("expected chunk for message_start")
	}
	// message_start should not carry usage; deferring to message_delta.
	if startChunk.Usage.PromptTokens != 0 {
		t.Errorf("message_start PromptTokens = %d, want 0 (deferred)", startChunk.Usage.PromptTokens)
	}

	delta := anthropic.MessageStreamEventUnion{
		Type:  "message_delta",
		Delta: anthropic.MessageStreamEventUnionDelta{StopReason: anthropic.StopReasonEndTurn},
		Usage: anthropic.MessageDeltaUsage{OutputTokens: 50},
	}
	deltaChunk, ok := convert.AnthropicEventToOpenAIChunk(delta, s)
	if !ok {
		t.Fatal("expected chunk for message_delta")
	}
	if deltaChunk.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", deltaChunk.Usage.PromptTokens)
	}
	if deltaChunk.Usage.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", deltaChunk.Usage.CompletionTokens)
	}
	if deltaChunk.Usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", deltaChunk.Usage.TotalTokens)
	}
	if deltaChunk.Usage.PromptTokensDetails.CachedTokens != 40 {
		t.Errorf("CachedTokens = %d, want 40 (cache_read_input_tokens)", deltaChunk.Usage.PromptTokensDetails.CachedTokens)
	}
	// CacheCreationTokens propagate via state for the service layer to record in usage;
	// they are not forwarded in the OpenAI chunk (no standard field).
	if s.CacheCreationTokens != 60 {
		t.Errorf("state.CacheCreationTokens = %d, want 60", s.CacheCreationTokens)
	}
}

func TestAnthropicEventToOpenAIChunk_ContentBlockStop_Suppressed(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	ev := anthropic.MessageStreamEventUnion{Type: "content_block_stop"}
	_, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
	if ok {
		t.Error("content_block_stop should be suppressed (ok=false)")
	}
}

func TestAnthropicEventToOpenAIChunk_ContentBlockStart_NonToolUse_Suppressed(t *testing.T) {
	t.Parallel()
	s := convert.NewAnthropicStreamState()
	s.MessageID = "msg_1"
	ev := anthropic.MessageStreamEventUnion{
		Type: "content_block_start",
		ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{
			Type: "text",
		},
	}
	_, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
	if ok {
		t.Error("content_block_start for text should be suppressed")
	}
}

func TestAnthropicEventToOpenAIChunk_ToolCallSlotIndexing(t *testing.T) {
	t.Parallel()
	// Two parallel tool calls should get distinct slot indices 0 and 1.
	s := convert.NewAnthropicStreamState()
	s.MessageID = "msg_1"

	for i, name := range []string{"tool_a", "tool_b"} {
		ev := anthropic.MessageStreamEventUnion{
			Type:  "content_block_start",
			Index: int64(i),
			ContentBlock: anthropic.ContentBlockStartEventContentBlockUnion{
				Type: "tool_use",
				ID:   "id_" + name,
				Name: name,
			},
		}
		chunk, ok := convert.AnthropicEventToOpenAIChunk(ev, s)
		if !ok {
			t.Fatalf("expected chunk for tool %s", name)
		}
		gotIdx := chunk.Choices[0].Delta.ToolCalls[0].Index
		if gotIdx != int64(i) {
			t.Errorf("tool %s: slot index = %d, want %d", name, gotIdx, i)
		}
	}
}

// ─── OpenAIParamsToAnthropicParams ────────────────────────────────────────────

func TestOpenAIParamsToAnthropicParams_FallsBackToModelMaxWhenUnset(t *testing.T) {
	t.Parallel()
	// claude-haiku-4-5 has a 64K documented output ceiling. When the client
	// omits max_tokens we should pass the model's max — not an arbitrary cap.
	params := openai.ChatCompletionNewParams{
		Model: "anthropic.claude-haiku-4-5",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hi"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)
	if got.MaxTokens != 64_000 {
		t.Errorf("MaxTokens = %d, want 64000 (claude-haiku-4-5 model max)", got.MaxTokens)
	}
}

func TestOpenAIParamsToAnthropicParams_LegacyModelUsesItsOwnMax(t *testing.T) {
	t.Parallel()
	// Older Claude 3 family caps at 4K — the lookup must honour that.
	params := openai.ChatCompletionNewParams{
		Model: "anthropic.claude-3-haiku-20240307-v1:0",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hi"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)
	if got.MaxTokens != 4_096 {
		t.Errorf("MaxTokens = %d, want 4096 (claude-3-haiku model max)", got.MaxTokens)
	}
}

func TestOpenAIParamsToAnthropicParams_ExplicitMaxTokens(t *testing.T) {
	t.Parallel()
	params := openai.ChatCompletionNewParams{
		Model:     "claude-3",
		MaxTokens: openai.Int(2048),
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hi"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)
	if got.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", got.MaxTokens)
	}
}

func TestOpenAIParamsToAnthropicParams_SystemMessage(t *testing.T) {
	t.Parallel()
	params := openai.ChatCompletionNewParams{
		Model: "claude-3",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: openai.String("You are helpful."),
				},
			}},
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hello"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)

	if len(got.System) != 1 {
		t.Fatalf("System len = %d, want 1", len(got.System))
	}
	if got.System[0].Text != "You are helpful." {
		t.Errorf("System[0].Text = %q", got.System[0].Text)
	}
	// System message should not appear in the messages list.
	if len(got.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system should be extracted)", len(got.Messages))
	}
}

func TestOpenAIParamsToAnthropicParams_Temperature(t *testing.T) {
	t.Parallel()
	params := openai.ChatCompletionNewParams{
		Model:       "claude-3",
		Temperature: openai.Float(0.7),
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hi"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)
	if !got.Temperature.Valid() || got.Temperature.Value != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", got.Temperature)
	}
}

func TestOpenAIParamsToAnthropicParams_NoTemperatureWhenUnset(t *testing.T) {
	t.Parallel()
	params := openai.ChatCompletionNewParams{
		Model: "claude-3",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("Hi"),
				},
			}},
		},
	}
	got := convert.OpenAIParamsToAnthropicParams(params)
	if got.Temperature.Valid() {
		t.Errorf("Temperature should not be set when not specified in request")
	}
}

// ─── MapOpenAIError ───────────────────────────────────────────────────────────

func TestMapOpenAIError_Throttled(t *testing.T) {
	t.Parallel()
	apiErr := &openai.Error{StatusCode: 429, Message: "rate limit"}
	got := convert.MapOpenAIError(inferencegw.ProviderBedrock, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError in chain, got %T", got)
	}
	if connErr.Code != "THROTTLED" {
		t.Errorf("Code = %q, want THROTTLED", connErr.Code)
	}
	if !connErr.Retryable {
		t.Error("expected Retryable=true for 429")
	}
	if connErr.Provider != inferencegw.ProviderBedrock {
		t.Errorf("Provider = %q, want bedrock", connErr.Provider)
	}
}

func TestMapOpenAIError_InvalidRequest(t *testing.T) {
	t.Parallel()
	apiErr := &openai.Error{StatusCode: 400, Message: "bad request"}
	got := convert.MapOpenAIError(inferencegw.ProviderBedrock, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if connErr.Code != "INVALID_REQUEST" {
		t.Errorf("Code = %q, want INVALID_REQUEST", connErr.Code)
	}
	if connErr.Retryable {
		t.Error("expected Retryable=false for 400")
	}
}

func TestMapOpenAIError_ServerError_Retryable(t *testing.T) {
	t.Parallel()
	apiErr := &openai.Error{StatusCode: 503, Message: "unavailable"}
	got := convert.MapOpenAIError(inferencegw.ProviderVertexAI, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if !connErr.Retryable {
		t.Error("expected Retryable=true for 503")
	}
	if connErr.Provider != inferencegw.ProviderVertexAI {
		t.Errorf("Provider = %q, want vertexai", connErr.Provider)
	}
}

func TestMapOpenAIError_ModelNotFound(t *testing.T) {
	t.Parallel()
	apiErr := &openai.Error{StatusCode: 404}
	got := convert.MapOpenAIError(inferencegw.ProviderBedrock, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if connErr.Code != "MODEL_NOT_FOUND" {
		t.Errorf("Code = %q, want MODEL_NOT_FOUND", connErr.Code)
	}
	if connErr.Retryable {
		t.Error("expected Retryable=false for 404")
	}
}

func TestMapOpenAIError_UnknownError_Retryable(t *testing.T) {
	t.Parallel()
	// Non-API errors (network failures etc.) should be retryable.
	got := convert.MapOpenAIError(inferencegw.ProviderBedrock, errors.New("connection reset"))

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if connErr.Code != "UNKNOWN" {
		t.Errorf("Code = %q, want UNKNOWN", connErr.Code)
	}
	if !connErr.Retryable {
		t.Error("expected Retryable=true for unknown errors")
	}
}

// ─── MapAnthropicError ────────────────────────────────────────────────────────

func TestMapAnthropicError_Throttled(t *testing.T) {
	t.Parallel()
	apiErr := newAnthropicAPIError(429)
	got := convert.MapAnthropicError(inferencegw.ProviderBedrock, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if connErr.Code != "THROTTLED" {
		t.Errorf("Code = %q, want THROTTLED", connErr.Code)
	}
	if !connErr.Retryable {
		t.Error("expected Retryable=true for 429")
	}
}

func TestMapAnthropicError_AuthFailed(t *testing.T) {
	t.Parallel()
	apiErr := newAnthropicAPIError(401)
	got := convert.MapAnthropicError(inferencegw.ProviderBedrock, apiErr)

	var connErr *inferencegw.ConnectorError
	if !errors.As(got, &connErr) {
		t.Fatalf("expected *ConnectorError, got %T", got)
	}
	if connErr.Code != "AUTH_FAILED" {
		t.Errorf("Code = %q, want AUTH_FAILED", connErr.Code)
	}
	if connErr.Retryable {
		t.Error("expected Retryable=false for 401")
	}
}

// ─── HTTPStatus preservation ──────────────────────────────────────────────────

func TestMapOpenAIError_HTTPStatusPreserved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"api 429", &openai.Error{StatusCode: 429}, 429},
		{"api 400", &openai.Error{StatusCode: 400}, 400},
		{"api 503", &openai.Error{StatusCode: 503}, 503},
		{"api 404", &openai.Error{StatusCode: 404}, 404},
		{"non-api error", errors.New("connection reset"), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := convert.MapOpenAIError(inferencegw.ProviderBedrock, tc.err)
			var connErr *inferencegw.ConnectorError
			if !errors.As(got, &connErr) {
				t.Fatalf("expected *ConnectorError, got %T", got)
			}
			if connErr.HTTPStatus != tc.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", connErr.HTTPStatus, tc.wantStatus)
			}
		})
	}
}

func TestMapAnthropicError_HTTPStatusPreserved(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
	}{
		{"429", 429},
		{"401", 401},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := convert.MapAnthropicError(inferencegw.ProviderBedrock, newAnthropicAPIError(tc.statusCode))
			var connErr *inferencegw.ConnectorError
			if !errors.As(got, &connErr) {
				t.Fatalf("expected *ConnectorError, got %T", got)
			}
			if connErr.HTTPStatus != tc.statusCode {
				t.Errorf("HTTPStatus = %d, want %d", connErr.HTTPStatus, tc.statusCode)
			}
		})
	}
}
