package inferencegw

import (
	"net/url"
	"testing"
)

// An uncapped limit lets one request pull every usage row the tenant has.
// See https://github.com/clear-route/orbit/issues/229.
func TestParseAnalyticsPrincipalsFilter_Limit(t *testing.T) {
	tests := []struct {
		name      string
		limit     string
		wantLimit int
		wantErr   bool
	}{
		{"absent uses the default", "", 20, false},
		{"within the ceiling", "50", 50, false},
		{"at the ceiling", "200", 200, false},
		{"above the ceiling", "201", 0, true},
		{"absurd", "100000", 0, true},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"not a number", "all", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{}
			if tc.limit != "" {
				q.Set("limit", tc.limit)
			}

			f, apiErr, _ := parseAnalyticsPrincipalsFilter(q)
			if tc.wantErr {
				if apiErr == nil {
					t.Fatalf("limit=%q was accepted, giving %d", tc.limit, f.Limit)
				}
				return
			}
			if apiErr != nil {
				t.Fatalf("limit=%q rejected: %v", tc.limit, apiErr)
			}
			if f.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", f.Limit, tc.wantLimit)
			}
		})
	}
}
