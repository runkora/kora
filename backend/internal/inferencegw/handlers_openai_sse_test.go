package inferencegw

import (
	"encoding/json"
	"testing"

	openai "github.com/openai/openai-go/v3"
)

// The SSE serializer builds its own usage shape, so a field the converter
// populates only reaches the client if it is listed here. ccusage and OpenCode
// read cached_tokens off the wire; without it they cannot see cache reads at all.
func TestMarshalStreamChunk_CarriesCachedTokens(t *testing.T) {
	data, err := marshalStreamChunk(&openai.ChatCompletionChunk{
		ID:    "chunk-1",
		Model: "claude-sonnet-4-6",
		Usage: openai.CompletionUsage{
			PromptTokens:     11248,
			CompletionTokens: 4,
			TotalTokens:      11252,
			PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
				CachedTokens: 11246,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}

	var got struct {
		Usage struct {
			PromptTokens        int64 `json:"prompt_tokens"`
			PromptTokensDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}

	if got.Usage.PromptTokens != 11248 {
		t.Errorf("prompt_tokens = %d, want 11248", got.Usage.PromptTokens)
	}
	if got.Usage.PromptTokensDetails.CachedTokens != 11246 {
		t.Errorf("prompt_tokens_details.cached_tokens = %d, want 11246: %s", got.Usage.PromptTokensDetails.CachedTokens, data)
	}
}

// A turn with no caching must not grow a details object full of zeroes.
func TestMarshalStreamChunk_OmitsEmptyDetails(t *testing.T) {
	data, err := marshalStreamChunk(&openai.ChatCompletionChunk{
		ID:    "chunk-1",
		Model: "gpt-5",
		Usage: openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	})
	if err != nil {
		t.Fatalf("marshalStreamChunk: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	usage, _ := got["usage"].(map[string]any)
	if _, present := usage["prompt_tokens_details"]; present {
		t.Errorf("expected no prompt_tokens_details without cache reads: %s", data)
	}
}
