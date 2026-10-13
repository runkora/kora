package inferencegw

import "testing"

func TestInferFamily(t *testing.T) {
	for id, want := range map[string]string{
		"anthropic/claude-sonnet-4.5": "anthropic",
		"meta-llama/llama-3.3-70b":    "meta-llama",
		"gpt-4o":                      "openai",
		"o3-mini":                     "openai",
		"Qwen2.5-Coder":               "qwen",
		"claude-opus-4-1":             "anthropic",
		"text-embedding-3-small":      "openai",
		"something":                   "unknown",
	} {
		if got := InferFamily(id); got != want {
			t.Errorf("InferFamily(%q) = %q, want %q", id, got, want)
		}
	}
}
