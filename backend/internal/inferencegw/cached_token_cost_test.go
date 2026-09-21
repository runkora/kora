package inferencegw

import (
	"math"
	"testing"
)

// codexTurnPricing is the pricing on the row from the report in #262:
// input 2.20, output 13.20, cache read 0.22, all per million tokens.
var codexTurnPricing = resolvedPricing{
	inputCostPer1M:     2.20,
	outputCostPer1M:    13.20,
	cacheReadCostPer1M: 0.22,
}

// TestUncachedPromptTokens covers the conversion from an OpenAI-shaped usage
// report, whose prompt/input count includes the cached prefix, to the uncached
// count the cost formula charges at the full input price.
func TestUncachedPromptTokens(t *testing.T) {
	cases := []struct {
		name      string
		prompt    int
		cacheRead int
		want      int
	}{
		{name: "no cache hit", prompt: 10247, cacheRead: 0, want: 10247},
		{name: "mostly cached", prompt: 10405, cacheRead: 10205, want: 200},
		{name: "fully cached", prompt: 10205, cacheRead: 10205, want: 0},
		{name: "cached count exceeds the total", prompt: 100, cacheRead: 500, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uncachedPromptTokens(tc.prompt, tc.cacheRead); got != tc.want {
				t.Errorf("uncachedPromptTokens(%d, %d) = %d, want %d", tc.prompt, tc.cacheRead, got, tc.want)
			}
		})
	}
}

// TestCachedTurnCost_OpenAIShape reproduces the cached Codex turn from #262.
// The recorded cost was 0.0252021: the 10205 cached tokens were charged at the
// full input price inside prompt_tokens and again at the cache-read price.
func TestCachedTurnCost_OpenAIShape(t *testing.T) {
	const (
		promptTokens     = 10405
		completionTokens = 5
		cacheReadTokens  = 10205
	)

	got := codexTurnPricing.estimatedCostUSD(
		uncachedPromptTokens(promptTokens, cacheReadTokens), completionTokens, cacheReadTokens, 0)

	// 200 uncached input + 5 output + 10205 cache read.
	want := (200*2.20 + 5*13.20 + 10205*0.22) / 1_000_000
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %.9f, want %.9f", got, want)
	}

	doubleBilled := codexTurnPricing.estimatedCostUSD(promptTokens, completionTokens, cacheReadTokens, 0)
	if got >= doubleBilled {
		t.Errorf("cost %.9f is not below the double-billed %.9f", got, doubleBilled)
	}
}

// TestCachedTurnCost_AnthropicShapeUnchanged guards the other dialect.
// Anthropic reports input_tokens exclusive of cache_read_input_tokens, so its
// paths pass the reported count straight through and must keep doing so.
func TestCachedTurnCost_AnthropicShapeUnchanged(t *testing.T) {
	const (
		inputTokens     = 200
		outputTokens    = 5
		cacheReadTokens = 10205
	)

	got := codexTurnPricing.estimatedCostUSD(inputTokens, outputTokens, cacheReadTokens, 0)

	want := (200*2.20 + 5*13.20 + 10205*0.22) / 1_000_000
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %.9f, want %.9f", got, want)
	}
}

// TestCachedTurnCost_BridgedResponsesShape covers a Claude model served through
// the Responses API. The Anthropic bridge normalises its usage to the
// Responses convention (input_tokens inclusive of cached_tokens), so the same
// subtraction that the native OpenAI path needs recovers the uncached count
// here too. Without that normalisation the subtraction would run against
// Anthropic's already-exclusive count and undercharge the request.
func TestCachedTurnCost_BridgedResponsesShape(t *testing.T) {
	// Anthropic reported 100 uncached input and 30 cache reads; the bridge
	// presents that as input_tokens 130 with cached_tokens 30.
	const (
		bridgedInputTokens = 130
		cacheReadTokens    = 30
		outputTokens       = 20
	)

	if got := uncachedPromptTokens(bridgedInputTokens, cacheReadTokens); got != 100 {
		t.Fatalf("uncached input = %d, want 100", got)
	}

	got := codexTurnPricing.estimatedCostUSD(
		uncachedPromptTokens(bridgedInputTokens, cacheReadTokens), outputTokens, cacheReadTokens, 0)

	want := (100*2.20 + 20*13.20 + 30*0.22) / 1_000_000
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %.9f, want %.9f", got, want)
	}
}
