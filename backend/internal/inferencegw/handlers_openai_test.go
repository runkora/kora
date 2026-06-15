package inferencegw

import (
	"encoding/json"
	"strings"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// ─── buildFilter ─────────────────────────────────────────────────────────────

func TestBuildFilter_NoFilters(t *testing.T) {
	t.Parallel()
	// A filter with no fields set (including empty TenantID) produces no WHERE clause.
	// In practice, List() rejects an empty TenantID before calling buildFilter — this
	// test covers the helper in isolation.
	where, args := buildFilter(InferenceUsageFilter{})
	if where != "" {
		t.Errorf("expected empty WHERE clause, got %q", where)
	}
	if len(args) != 0 {
		t.Errorf("expected 0 args, got %d", len(args))
	}
}

func TestBuildFilter_TenantID(t *testing.T) {
	t.Parallel()
	tid := "tenant-abc"
	where, args := buildFilter(InferenceUsageFilter{TenantID: tid})
	if !strings.Contains(where, "tenant_id = $1") {
		t.Errorf("WHERE clause %q should contain 'tenant_id = $1'", where)
	}
	if len(args) != 1 || args[0] != "tenant-abc" {
		t.Errorf("args = %v, want [tenant-abc]", args)
	}
}

func TestBuildFilter_MultipleFilters(t *testing.T) {
	t.Parallel()
	tid := "t1"
	pid := "p1"
	where, args := buildFilter(InferenceUsageFilter{TenantID: tid, PrincipalID: &pid})
	if !strings.Contains(where, "WHERE") {
		t.Errorf("expected WHERE keyword, got %q", where)
	}
	if !strings.Contains(where, "AND") {
		t.Errorf("expected AND between filters, got %q", where)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuildFilter_StatusFilter(t *testing.T) {
	t.Parallel()
	s := UsageStatusSuccess
	where, args := buildFilter(InferenceUsageFilter{Status: &s})
	if !strings.Contains(where, "status = $1") {
		t.Errorf("WHERE clause %q should contain 'status = $1'", where)
	}
	if len(args) != 1 || args[0] != "success" {
		t.Errorf("args = %v, want [success]", args)
	}
}

// ─── marshalStreamChunk ───────────────────────────────────────────────────────

func TestMarshalStreamChunk_ChoicesNeverNull(t *testing.T) {
	t.Parallel()
	// message_start-style chunk: no choices, only usage.
	chunk := &openai.ChatCompletionChunk{
		ID:    "msg_123",
		Model: "claude-sonnet-4-6",
		Usage: openai.CompletionUsage{PromptTokens: 10},
	}
	raw, err := marshalStreamChunk(chunk)
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if string(m["choices"]) != "[]" {
		t.Errorf("choices = %s, want []", m["choices"])
	}
}

func TestMarshalStreamChunk_ObjectDefault(t *testing.T) {
	t.Parallel()
	// Manually constructed chunks don't have Object set; marshalStreamChunk
	// should default to "chat.completion.chunk".
	chunk := &openai.ChatCompletionChunk{ID: "msg_123", Model: "claude-sonnet-4-6"}
	raw, err := marshalStreamChunk(chunk)
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if string(m["object"]) != `"chat.completion.chunk"` {
		t.Errorf("object = %s, want \"chat.completion.chunk\"", m["object"])
	}
}

func TestMarshalStreamChunk_NoSpuriousFields(t *testing.T) {
	t.Parallel()
	// A text-delta chunk should not emit role:"", tool_calls:null, etc.
	chunk := &openai.ChatCompletionChunk{
		ID:    "msg_123",
		Model: "claude-sonnet-4-6",
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					Role:    "assistant",
					Content: "Hello",
				},
			},
		},
	}
	raw, err := marshalStreamChunk(chunk)
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	s := string(raw)
	for _, banned := range []string{`"tool_calls":null`, `"function_call"`, `"refusal"`, `"logprobs"`, `"service_tier"`, `"system_fingerprint"`} {
		if strings.Contains(s, banned) {
			t.Errorf("output contains %q, want it absent: %s", banned, s)
		}
	}
}

func TestMarshalStreamChunk_ToolCallFunctionAlwaysPresent(t *testing.T) {
	t.Parallel()
	// input_json_delta: tool call has index + arguments only — function must still appear.
	chunk := &openai.ChatCompletionChunk{
		ID:    "msg_123",
		Model: "claude-sonnet-4-6",
		Choices: []openai.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: openai.ChatCompletionChunkChoiceDelta{
					ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
						{Index: 0, Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{Arguments: `{"k":`}},
					},
				},
			},
		},
	}
	raw, err := marshalStreamChunk(chunk)
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	if !strings.Contains(string(raw), `"function"`) {
		t.Errorf("output missing \"function\" key: %s", raw)
	}
}

func TestMarshalStreamChunk_UsageTotalTokens(t *testing.T) {
	t.Parallel()
	chunk := &openai.ChatCompletionChunk{
		ID:    "msg_123",
		Model: "claude-sonnet-4-6",
		Usage: openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	raw, err := marshalStreamChunk(chunk)
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	var usage map[string]int64
	if err := json.Unmarshal(m["usage"], &usage); err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if usage["total_tokens"] != 15 {
		t.Errorf("total_tokens = %d, want 15", usage["total_tokens"])
	}
}
