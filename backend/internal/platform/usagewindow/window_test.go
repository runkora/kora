package usagewindow

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Period
		err  error
	}{
		{"", Period7d, nil},
		{"24h", Period24h, nil},
		{"90d", Period90d, nil},
		{"1y", "", ErrInvalidPeriod},
		{"7D", "", ErrInvalidPeriod},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got, err := Parse(tc.raw); got != tc.want || err != tc.err {
				t.Errorf("Parse(%q) = %q, %v; want %q, %v", tc.raw, got, err, tc.want, tc.err)
			}
		})
	}
}

func TestForCutsWholeBuckets(t *testing.T) {
	end := time.Date(2026, 10, 3, 14, 23, 5, 0, time.UTC)
	for _, tc := range []struct {
		period  Period
		buckets int
	}{{Period24h, 8}, {Period7d, 7}, {Period30d, 10}, {Period90d, 15}, {"unknown", 7}} {
		t.Run(string(tc.period), func(t *testing.T) {
			w := For(tc.period, end)
			if got := int(w.End.Sub(w.Start) / w.Bucket); got != tc.buckets {
				t.Errorf("%d buckets of %s, want %d", got, w.Bucket, tc.buckets)
			}
			if w.End.Sub(w.Start)%w.Bucket != 0 {
				t.Errorf("bucket %s does not divide the window", w.Bucket)
			}
		})
	}
}
