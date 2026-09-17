package inferencegw

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestBuildFilter_IncludeDelegatedWidensPrincipalPredicate(t *testing.T) {
	t.Parallel()
	pid := "user-1"
	where, args := buildFilter(InferenceUsageFilter{PrincipalID: &pid, IncludeDelegated: true})
	if !strings.Contains(where, "(principal_id = $1 OR delegator_id = $1)") {
		t.Errorf("WHERE clause %q should widen to delegated rows", where)
	}
	if len(args) != 1 || args[0] != "user-1" {
		t.Errorf("args = %v, want [user-1]", args)
	}

	// Without the flag the predicate stays exact.
	where, _ = buildFilter(InferenceUsageFilter{PrincipalID: &pid})
	if !strings.Contains(where, "principal_id = $1") || strings.Contains(where, "delegator_id") {
		t.Errorf("WHERE clause %q must not widen without IncludeDelegated", where)
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

// ─── unified models response schema ──────────────────────────────────────────

func TestModelsResponseUnifiedSchema(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		input       []modelObject
		wantLen     int
		wantFirstID string // empty means expect nil
		wantLastID  string // empty means expect nil
	}{
		{
			name:  "empty",
			input: []modelObject{},
		},
		{
			name: "single",
			input: []modelObject{
				{
					ID:          "model-a",
					Object:      "model",
					Type:        "model",
					Created:     1700000000,
					CreatedAt:   "2023-11-15T00:26:40Z",
					DisplayName: "Model A",
				},
			},
			wantLen:     1,
			wantFirstID: "model-a",
			wantLastID:  "model-a",
		},
		{
			name: "multiple",
			input: []modelObject{
				{
					ID:          "model-a",
					Object:      "model",
					Type:        "model",
					Created:     1700000000,
					CreatedAt:   "2023-11-15T00:26:40Z",
					DisplayName: "Model A",
				},
				{
					ID:          "model-b",
					Object:      "model",
					Type:        "model",
					Created:     1700000001,
					CreatedAt:   "2023-11-15T00:26:41Z",
					DisplayName: "Model B",
				},
				{
					ID:          "model-c",
					Object:      "model",
					Type:        "model",
					Created:     1700000002,
					CreatedAt:   "2023-11-15T00:26:42Z",
					DisplayName: "Model C",
				},
			},
			wantLen:     3,
			wantFirstID: "model-a",
			wantLastID:  "model-c",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := modelsResponse{
				Object:  "list",
				Data:    tc.input,
				HasMore: false,
			}
			if len(tc.input) > 0 {
				firstID := tc.input[0].ID
				lastID := tc.input[len(tc.input)-1].ID
				resp.FirstID = &firstID
				resp.LastID = &lastID
			}

			// Verify OpenAI fields
			if resp.Object != "list" {
				t.Errorf("Object = %q, want \"list\"", resp.Object)
			}
			if len(resp.Data) != tc.wantLen {
				t.Errorf("len(Data) = %d, want %d", len(resp.Data), tc.wantLen)
			}
			if resp.HasMore {
				t.Errorf("HasMore = true, want false")
			}

			// Verify Anthropic fields
			if tc.wantFirstID == "" {
				if resp.FirstID != nil {
					t.Errorf("FirstID = %q, want nil", *resp.FirstID)
				}
			} else if resp.FirstID == nil || *resp.FirstID != tc.wantFirstID {
				got := "<nil>"
				if resp.FirstID != nil {
					got = *resp.FirstID
				}
				t.Errorf("FirstID = %q, want %q", got, tc.wantFirstID)
			}

			if tc.wantLastID == "" {
				if resp.LastID != nil {
					t.Errorf("LastID = %q, want nil", *resp.LastID)
				}
			} else if resp.LastID == nil || *resp.LastID != tc.wantLastID {
				got := "<nil>"
				if resp.LastID != nil {
					got = *resp.LastID
				}
				t.Errorf("LastID = %q, want %q", got, tc.wantLastID)
			}

			// Verify each model object has both field sets
			for i, item := range resp.Data {
				// OpenAI fields
				if item.Object != "model" {
					t.Errorf("Data[%d].Object = %q, want \"model\"", i, item.Object)
				}
				if item.Created == 0 {
					t.Errorf("Data[%d].Created = 0, want non-zero Unix timestamp", i)
				}

				// Anthropic fields
				if item.Type != "model" {
					t.Errorf("Data[%d].Type = %q, want \"model\"", i, item.Type)
				}
				if _, err := time.Parse(time.RFC3339, item.CreatedAt); err != nil {
					t.Errorf("Data[%d].CreatedAt = %q: not valid RFC3339: %v", i, item.CreatedAt, err)
				}

				// Common fields
				if item.ID != tc.input[i].ID {
					t.Errorf("Data[%d].ID = %q, want %q", i, item.ID, tc.input[i].ID)
				}
				if item.DisplayName != tc.input[i].DisplayName {
					t.Errorf("Data[%d].DisplayName = %q, want %q", i, item.DisplayName, tc.input[i].DisplayName)
				}
			}
		})
	}
}
