package inferencegw

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSystemPrompt(t *testing.T) {
	t.Run("string system becomes TextBlockParam array", func(t *testing.T) {
		in := `{"model":"claude-3-5-sonnet-latest","max_tokens":1000,"system":"You are a helpful assistant.","messages":[]}`
		out := normalizeSystemPrompt([]byte(in))

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}

		var blocks []map[string]string
		if err := json.Unmarshal(raw["system"], &blocks); err != nil {
			t.Fatalf("system is not an array: %v", err)
		}
		if len(blocks) != 1 {
			t.Fatalf("want 1 block, got %d", len(blocks))
		}
		if blocks[0]["type"] != "text" {
			t.Errorf("want type=text, got %q", blocks[0]["type"])
		}
		if blocks[0]["text"] != "You are a helpful assistant." {
			t.Errorf("want correct text, got %q", blocks[0]["text"])
		}
	})

	t.Run("array system is left unchanged", func(t *testing.T) {
		in := `{"system":[{"type":"text","text":"Already an array."}],"messages":[]}`
		out := normalizeSystemPrompt([]byte(in))
		if string(out) != in {
			t.Errorf("array-form system should not be modified; got %s", out)
		}
		// Re-parse and confirm it is still an array.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		var blocks []map[string]string
		if err := json.Unmarshal(raw["system"], &blocks); err != nil {
			t.Fatalf("array system was corrupted: %v", err)
		}
		if blocks[0]["text"] != "Already an array." {
			t.Errorf("unexpected text: %q", blocks[0]["text"])
		}
	})

	t.Run("missing system field is a no-op", func(t *testing.T) {
		in := `{"model":"claude-3-5-sonnet-latest","messages":[]}`
		out := normalizeSystemPrompt([]byte(in))
		var rawOut map[string]json.RawMessage
		if err := json.Unmarshal(out, &rawOut); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}
		if _, ok := rawOut["system"]; ok {
			t.Error("system field should not be injected when absent")
		}
	})
}

// TestMessagesEnvelopeStringSystem verifies the full decode path: a request
// with a plain-string system field must populate MessageNewParams.System.
func TestMessagesEnvelopeStringSystem(t *testing.T) {
	body := `{
		"model": "claude-3-5-sonnet-latest",
		"max_tokens": 100,
		"system": "You are a patient math tutor.",
		"messages": [{"role":"user","content":"What is 1+1?"}]
	}`

	var env messagesEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(env.Params.System) == 0 {
		t.Fatal("system field was silently dropped — string→array normalisation is broken")
	}

	block := env.Params.System[0]
	want := "You are a patient math tutor."
	if block.Text != want {
		t.Errorf("system text = %q, want %q", block.Text, want)
	}
}
