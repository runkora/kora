package compatible

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestParsePrice(t *testing.T) {
	cases := []struct {
		name            string
		raw             string
		ok              bool
		in, out, cr, cw float64
	}{
		{"decimal strings", `{"id":"m","pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000003","input_cache_write":"0.00000375"}}`, true, 3, 15, 0.3, 3.75},
		{"no cache prices", `{"id":"m","pricing":{"prompt":"0.00000018","completion":"0.0000009"}}`, true, 0.18, 0.9, 0, 0},
		{"free model", `{"id":"m:free","pricing":{"prompt":"0","completion":"0"}}`, true, 0, 0, 0, 0},
		{"no pricing block", `{"id":"llama3","object":"model"}`, false, 0, 0, 0, 0},
		{"missing completion", `{"id":"m","pricing":{"prompt":"0.000001"}}`, false, 0, 0, 0, 0},
		{"not a number", `{"id":"m","pricing":{"prompt":"n/a","completion":"0.000001"}}`, false, 0, 0, 0, 0},
		{"variable price router", `{"id":"openrouter/auto","pricing":{"prompt":"-1","completion":"-1"}}`, false, 0, 0, 0, 0},
		{"bad cache price", `{"id":"m","pricing":{"prompt":"0.000001","completion":"0.000002","input_cache_read":"x"}}`, false, 0, 0, 0, 0},
		{"not json", `not json`, false, 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := parsePrice(tc.raw)
			if (p != nil) != tc.ok {
				t.Fatalf("parsePrice = %+v, want ok=%v", p, tc.ok)
			}
			if p == nil {
				return
			}
			if !near(p.InputCostPer1M, tc.in) || !near(p.OutputCostPer1M, tc.out) || !near(p.CacheReadCostPer1M, tc.cr) || !near(p.CacheWriteCostPer1M, tc.cw) {
				t.Errorf("price = %+v", p)
			}
		})
	}
}
