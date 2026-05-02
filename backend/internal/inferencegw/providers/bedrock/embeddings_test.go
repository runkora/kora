package bedrock

import (
	"testing"

	openai "github.com/openai/openai-go"
)

// TestExtractEmbeddingInputs covers the multi-input fix: native Bedrock
// embedding paths previously returned only the first array element, silently
// dropping the rest. extractEmbeddingInputs returns the full slice.
func TestExtractEmbeddingInputs(t *testing.T) {
	t.Parallel()

	t.Run("single string", func(t *testing.T) {
		t.Parallel()
		var p openai.EmbeddingNewParams
		p.Input.OfString = openai.String("hello")
		got, err := extractEmbeddingInputs(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "hello" {
			t.Errorf("got %v, want [hello]", got)
		}
	})

	t.Run("array preserved", func(t *testing.T) {
		t.Parallel()
		var p openai.EmbeddingNewParams
		p.Input.OfArrayOfStrings = []string{"a", "b", "c"}
		got, err := extractEmbeddingInputs(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3 — multi-input must not be truncated", len(got))
		}
		for i, want := range []string{"a", "b", "c"} {
			if got[i] != want {
				t.Errorf("got[%d] = %q, want %q", i, got[i], want)
			}
		}
	})

	t.Run("empty rejected", func(t *testing.T) {
		t.Parallel()
		var p openai.EmbeddingNewParams
		_, err := extractEmbeddingInputs(p)
		if err == nil {
			t.Fatal("expected error on empty input")
		}
	})
}
