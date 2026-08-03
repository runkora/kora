package bedrock

import (
	"testing"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// TestBuildProviderModel_DisplayNameNormalisation verifies systematic
// corrections applied to AWS-returned ModelNames.
func TestBuildProviderModel_DisplayNameNormalisation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		canonicalID string
		apiName     string // what AWS returned
		wantName    string
	}{
		// Cohere: AWS injects a region word into the ModelName.
		{"us.cohere.embed-v4:0", "US Cohere Embed v4", "Cohere Embed v4"},
		{"global.cohere.embed-v4:0", "Global Cohere Embed v4", "Cohere Embed v4"},
		// apac.* must strip "APAC " not "AP " (apac. and ap. both prefix-match apac.*).
		{"apac.anthropic.claude-3-haiku-20240307-v1:0", "APAC Claude 3 Haiku", "Claude 3 Haiku"},
		// DeepSeek: AWS returns empty ModelName — fallback must still produce vendor prefix.
		{"deepseek.v3.1", "", "DeepSeek V3.1"},
		// DeepSeek: AWS uses a hyphen separator after the vendor name.
		{"deepseek.v3-v1:0", "DeepSeek-V3.1", "DeepSeek V3.1"},
		// Unaffected: correct AWS name passes through unchanged.
		{"deepseek.v3.2", "DeepSeek V3.2", "DeepSeek V3.2"},
		// Unaffected: non-DeepSeek hyphenated names are not touched.
		{"amazon.nova-pro-v1:0", "Nova Pro", "Nova Pro"},
		// No false-positive: model with no region ID prefix keeps "Global" in name.
		{"acme.global-model-v1", "Global Model v1", "Global Model v1"},
		// openai.*: AWS returns correct mixed-case name — must not be re-cased.
		{"openai.gpt-oss-safeguard-120b", "GPT OSS Safeguard 120B", "GPT OSS Safeguard 120B"},
		// openai.*: AWS returns a raw lowercase slug — title-case + acronym upcase.
		{"openai.gpt-oss-120b-1:0", "gpt-oss-120b", "GPT OSS 120b"},
		{"openai.gpt-oss-20b-1:0", "gpt-oss-20b", "GPT OSS 20b"},
		// openai.*: no AWS name — fallback path must apply acronym upcase.
		{"openai.gpt-oss-120b", "", "GPT OSS 120b"},
		{"openai.gpt-5.4", "", "GPT 5.4"},
		// Qwen: AWS returns a raw lowercase slug — must title-case.
		{"qwen.qwen3-coder-30b-a3b-instruct-v1:0", "qwen3-coder-30b-a3b-instruct", "Qwen3 Coder 30b A3b Instruct"},
	}
	for _, tc := range cases {
		t.Run(tc.canonicalID, func(t *testing.T) {
			t.Parallel()
			m := buildProviderModel(tc.canonicalID, tc.apiName, nil, nil, "bedrock")
			if m.DisplayName != tc.wantName {
				t.Errorf("DisplayName = %q, want %q", m.DisplayName, tc.wantName)
			}
		})
	}
}

// TestFallbackDisplayName_UnknownNamespace verifies that an unrecognised
// provider namespace is stripped at the last "." rather than leaking into
// the title-cased display name.
func TestFallbackDisplayName_UnknownNamespace(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   string
		want string
	}{
		{"future.cool-model-v1", "Cool Model V1"},
		{"newco.reasoning-8b-v1:0", "Reasoning 8b V1"},
		// Known namespace still works correctly.
		{"xai.grok-4.3", "Grok 4.3"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := fallbackDisplayName(tc.id)
			if got != tc.want {
				t.Errorf("fallbackDisplayName(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

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
