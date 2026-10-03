package usagewindow

import (
	"errors"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw     string
		want    Period
		wantErr bool
	}{
		{"", Period7d, false},
		{"24h", Period24h, false},
		{"7d", Period7d, false},
		{"30d", Period30d, false},
		{"90d", Period90d, false},
		{"1y", "", true},
		{"7D", "", true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.raw)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidPeriod) {
				t.Errorf("Parse(%q) error = %v, want ErrInvalidPeriod", tc.raw, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Parse(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
}

func TestFor(t *testing.T) {
	end := time.Date(2026, 10, 3, 14, 23, 5, 0, time.UTC)
	cases := []struct {
		period  Period
		length  time.Duration
		buckets int
	}{
		{Period24h, day, 8},
		{Period7d, 7 * day, 7},
		{Period30d, 30 * day, 10},
		{Period90d, 90 * day, 15},
	}
	for _, tc := range cases {
		w := For(tc.period, end)
		if !w.End.Equal(end) || w.Length() != tc.length {
			t.Errorf("%s: window = [%s, %s), want it to end at %s and last %s", tc.period, w.Start, w.End, end, tc.length)
		}
		if got := w.Buckets(); got != tc.buckets {
			t.Errorf("%s: buckets = %d, want %d", tc.period, got, tc.buckets)
		}
		if w.Length()%w.Bucket != 0 {
			t.Errorf("%s: bucket %s does not divide the length", tc.period, w.Bucket)
		}
		if !w.PreviousStart().Equal(w.Start.Add(-tc.length)) {
			t.Errorf("%s: previous start = %s", tc.period, w.PreviousStart())
		}
	}
}
