package compatible

import (
	"encoding/json"
	"strconv"

	"github.com/clear-route/orbit/internal/inferencegw"
)

// perMillion converts a per-token price to a per-1M-token price.
const perMillion = 1_000_000

// listedPrice is the pricing block OpenRouter publishes per model: decimal
// strings in USD per token. Tiered overrides are not read.
type listedPrice struct {
	Pricing *struct {
		Prompt          string `json:"prompt"`
		Completion      string `json:"completion"`
		InputCacheRead  string `json:"input_cache_read"`
		InputCacheWrite string `json:"input_cache_write"`
	} `json:"pricing"`
}

// parsePrice reads the price a /models entry publishes, or nil when it has none.
// A negative price marks a variable-priced router and counts as none.
func parsePrice(raw string) *inferencegw.ModelPriceEntry {
	var l listedPrice
	if err := json.Unmarshal([]byte(raw), &l); err != nil || l.Pricing == nil {
		return nil
	}
	input, okIn := perToken(l.Pricing.Prompt)
	output, okOut := perToken(l.Pricing.Completion)
	if !okIn || !okOut {
		return nil
	}
	cacheRead, okRead := perToken(l.Pricing.InputCacheRead)
	cacheWrite, okWrite := perToken(l.Pricing.InputCacheWrite)
	if (l.Pricing.InputCacheRead != "" && !okRead) || (l.Pricing.InputCacheWrite != "" && !okWrite) {
		return nil
	}
	return &inferencegw.ModelPriceEntry{
		InputCostPer1M:      input * perMillion,
		OutputCostPer1M:     output * perMillion,
		CacheReadCostPer1M:  cacheRead * perMillion,
		CacheWriteCostPer1M: cacheWrite * perMillion,
	}
}

// perToken parses one non-negative decimal string. An empty string is not a price.
func perToken(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}
