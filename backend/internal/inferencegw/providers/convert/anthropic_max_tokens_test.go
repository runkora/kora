package convert_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw/providers/convert"
)

func TestAnthropicModelMaxOutputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modelID   string
		wantMax   int64
		wantKnown bool
	}{
		// ── Bedrock canonical IDs ────────────────────────────────────────────
		{"sonnet 4.6 bedrock", "anthropic.claude-sonnet-4-6-20251015-v1:0", 64_000, true},
		{"opus 4.7 bedrock", "anthropic.claude-opus-4-7-20251015-v1:0", 32_000, true},
		{"haiku 4.5 bedrock", "anthropic.claude-haiku-4-5-20251001-v1:0", 64_000, true},

		// ── Cross-region inference profile IDs ───────────────────────────────
		{"us-prefixed sonnet", "us.anthropic.claude-sonnet-4-5", 64_000, true},
		{"eu-prefixed opus", "eu.anthropic.claude-opus-4", 32_000, true},
		{"apac-prefixed haiku", "apac.anthropic.claude-haiku-4-5", 64_000, true},

		// ── Vertex @version form ─────────────────────────────────────────────
		{"vertex sonnet 4.5", "claude-sonnet-4-5@20250929", 64_000, true},
		{"vertex 3.5 sonnet", "claude-3-5-sonnet-v2@20241022", 8_192, true},

		// ── Plain alias form ─────────────────────────────────────────────────
		{"alias sonnet 4.6", "claude-sonnet-4-6", 64_000, true},
		{"alias haiku 3.5", "claude-3-5-haiku", 8_192, true},
		{"alias 3 opus", "claude-3-opus", 4_096, true},

		// ── Older models (3.x family) ────────────────────────────────────────
		{"3.7 sonnet", "anthropic.claude-3-7-sonnet-20250219-v1:0", 8_192, true},
		{"3 haiku", "anthropic.claude-3-haiku-20240307-v1:0", 4_096, true},

		// ── Unknown model — fallback to high ceiling, known=false ────────────
		{"unknown future model", "anthropic.claude-saturn-9", 64_000, false},
		{"non-claude model", "amazon.nova-pro-v1:0", 64_000, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotMax, gotKnown := convert.AnthropicModelMaxOutputTokens(tc.modelID)
			if gotMax != tc.wantMax {
				t.Errorf("max = %d, want %d", gotMax, tc.wantMax)
			}
			if gotKnown != tc.wantKnown {
				t.Errorf("known = %v, want %v", gotKnown, tc.wantKnown)
			}
		})
	}
}
