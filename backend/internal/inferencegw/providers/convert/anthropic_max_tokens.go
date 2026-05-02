package convert

import "strings"

// anthropicMaxOutputTokens is the per-model maximum output token count used as
// the implicit ceiling when a Responses or Chat Completions client routes
// through the Anthropic bridge without setting max_tokens / max_output_tokens.
//
// Values reflect Anthropic's published per-model output limits as of
// 2026-05-02, without the extended-output beta header (which the bridge does
// not currently set). Bedrock and direct Anthropic both reject values that
// exceed the model's per-request limit, so a wrong value here surfaces as an
// explicit upstream INVALID_REQUEST rather than silent truncation.
//
// Order matters: longer/more-specific prefixes must be listed before shorter
// ones. The lookup is a longest-prefix match against a canonicalised model
// ID (region prefix, "anthropic." namespace, and Vertex "@version" suffix
// stripped). New Claude families must be added here when they ship.
var anthropicMaxOutputTokens = []struct {
	prefix string
	max    int64
}{
	// Claude 4.x
	{"claude-opus-4", 32_000},
	{"claude-sonnet-4", 64_000},
	{"claude-haiku-4", 64_000},

	// Claude 3.7
	{"claude-3-7-sonnet", 8_192},

	// Claude 3.5
	{"claude-3-5-sonnet", 8_192},
	{"claude-3-5-haiku", 8_192},

	// Claude 3 (legacy)
	{"claude-3-opus", 4_096},
	{"claude-3-sonnet", 4_096},
	{"claude-3-haiku", 4_096},
}

// fallbackAnthropicMaxOutputTokens is used when a model is not in the table.
// 64K matches the upper end of current Claude 4.x output limits — picking high
// rather than low means any too-high value surfaces as a clear upstream error
// the operator can act on, rather than as silent truncation that's hard to
// diagnose.
const fallbackAnthropicMaxOutputTokens int64 = 64_000

// AnthropicModelMaxOutputTokens returns the documented maximum output token
// count for a Claude model ID, plus a flag indicating whether the model was
// found in the lookup table. When the model is unknown, the fallback value
// (64K) is returned with ok=false so callers can log the fallback.
//
// modelID may arrive in any of the forms the routing engine produces:
// Bedrock canonical ("anthropic.claude-sonnet-4-6-20251015-v1:0"),
// cross-region profile ("us.anthropic.claude-sonnet-4-6-…"), Vertex AI
// versioned ("claude-sonnet-4-6@20251015"), or a plain alias
// ("claude-sonnet-4-6").
func AnthropicModelMaxOutputTokens(modelID string) (max int64, known bool) {
	canon := canonicalAnthropicModelName(modelID)
	for _, entry := range anthropicMaxOutputTokens {
		if strings.HasPrefix(canon, entry.prefix) {
			return entry.max, true
		}
	}
	return fallbackAnthropicMaxOutputTokens, false
}

// canonicalAnthropicModelName strips the leading region prefix ("us.", "eu.",
// "apac.", "global."), the "anthropic." namespace, and a Vertex AI "@version"
// suffix from a model ID so the stripped form can be matched against the
// table prefixes.
func canonicalAnthropicModelName(modelID string) string {
	if i := strings.IndexByte(modelID, '.'); i > 0 && i <= 6 {
		// Region prefix is short ("us.", "eu.", "apac.", "global.") — guard
		// against false-stripping by capping the dot index at 6.
		modelID = modelID[i+1:]
	}
	modelID = strings.TrimPrefix(modelID, "anthropic.")
	if i := strings.IndexByte(modelID, '@'); i >= 0 {
		modelID = modelID[:i]
	}
	return modelID
}
