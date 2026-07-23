package bedrock

import (
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
)

func TestBuildProviderModel_ProviderField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider inferencegw.ProviderName
	}{
		{inferencegw.ProviderBedrock},
		{"bedrock:aus"},
		{"bedrock:us"},
	}
	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			t.Parallel()
			m := buildProviderModel("anthropic.claude-3-haiku-20240307-v1:0", "", nil, nil, tc.provider)
			if m.Provider != tc.provider {
				t.Errorf("Provider = %q, want %q", m.Provider, tc.provider)
			}
		})
	}
}

// TestIsAnthropicModel validates all three ID forms the routing engine may
// deliver to the connector, including the short-alias form that triggered the
// original MODEL_NOT_SUPPORTED regression (claude-sonnet-4-6 was rejected
// because the old check only matched "anthropic." prefix and did not handle
// the "claude-" prefix that clients and a cold-start routing engine supply).
func TestIsAnthropicModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want bool
	}{
		// ── canonical Bedrock namespace form ─────────────────────────────────────
		{
			name: "canonical anthropic namespace",
			id:   "anthropic.claude-3-5-sonnet-20241022-v1:0",
			want: true,
		},
		{
			name: "canonical anthropic haiku",
			id:   "anthropic.claude-3-haiku-20240307-v1:0",
			want: true,
		},

		// ── cross-region inference profile form ───────────────────────────────────
		{
			name: "us cross-region prefix",
			id:   "us.anthropic.claude-3-5-sonnet-20241022-v1:0",
			want: true,
		},
		{
			name: "eu cross-region prefix",
			id:   "eu.anthropic.claude-3-5-sonnet-20241022-v1:0",
			want: true,
		},
		{
			name: "apac cross-region prefix",
			id:   "apac.anthropic.claude-3-haiku-20240307-v1:0",
			want: true,
		},

		// ── short alias form (client-supplied / cold-start routing) ───────────────
		// These are the form that caused the original regression: the routing
		// engine passes the raw client model string before the catalog is warm,
		// so the connector receives "claude-sonnet-4-6" with no namespace prefix.
		{
			name: "short alias: claude-sonnet-4-6 (regression case)",
			id:   "claude-sonnet-4-6",
			want: true,
		},
		{
			name: "short alias: claude-3-5-haiku",
			id:   "claude-3-5-haiku",
			want: true,
		},
		{
			name: "short alias: claude-opus-4",
			id:   "claude-opus-4",
			want: true,
		},
		{
			name: "short alias: claude-3-5-sonnet-20241022-v1",
			id:   "claude-3-5-sonnet-20241022-v1",
			want: true,
		},

		// ── non-Anthropic models ─────────────────────────────────────────────────
		{
			name: "amazon nova pro",
			id:   "amazon.nova-pro-v1:0",
			want: false,
		},
		{
			name: "meta llama",
			id:   "meta.llama3-70b-instruct-v1:0",
			want: false,
		},
		{
			name: "mistral large",
			id:   "mistral.mistral-large-2402-v1:0",
			want: false,
		},
		{
			name: "cross-region non-anthropic",
			id:   "us.amazon.nova-pro-v1:0",
			want: false,
		},
		{
			name: "empty string",
			id:   "",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isAnthropicModel(tc.id); got != tc.want {
				t.Errorf("isAnthropicModel(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}
