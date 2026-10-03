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
	now := time.Date(2026, 10, 3, 14, 23, 5, 0, time.UTC)
	cases := []struct {
		period  Period
		start   time.Time
		origin  time.Time
		buckets int
	}{
		{Period24h, time.Date(2026, 10, 2, 14, 23, 5, 0, time.UTC), time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC), 9},
		{Period7d, time.Date(2026, 9, 26, 14, 23, 5, 0, time.UTC), time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), 8},
		{Period30d, time.Date(2026, 9, 3, 14, 23, 5, 0, time.UTC), time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 11},
		{Period90d, time.Date(2026, 7, 5, 14, 23, 5, 0, time.UTC), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), 13},
	}
	for _, tc := range cases {
		w := For(tc.period, now)
		if !w.End.Equal(now) || !w.Start.Equal(tc.start) || !w.Origin.Equal(tc.origin) {
			t.Errorf("%s: window = [%s, %s) origin %s, want [%s, %s) origin %s", tc.period, w.Start, w.End, w.Origin, tc.start, now, tc.origin)
		}
		if got := w.Buckets(); got != tc.buckets {
			t.Errorf("%s: buckets = %d, want %d", tc.period, got, tc.buckets)
		}
		if w.Length() != specs[tc.period].length {
			t.Errorf("%s: length = %s, want the full period", tc.period, w.Length())
		}
		if got := w.PreviousStart(); !got.Equal(w.Start.Add(-w.Length())) {
			t.Errorf("%s: previous start = %s", tc.period, got)
		}
	}
}

func TestForNonUTCInput(t *testing.T) {
	loc := time.FixedZone("AEDT", 11*3600)
	now := time.Date(2026, 10, 4, 1, 0, 0, 0, loc) // 14:00 UTC on 3 Oct
	w := For(Period7d, now)
	if want := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC); !w.End.Equal(want) || w.End.Location() != time.UTC {
		t.Errorf("end = %s, want %s in UTC", w.End, want)
	}
}
