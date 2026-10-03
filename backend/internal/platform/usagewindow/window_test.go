package usagewindow

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for raw, want := range map[string]Period{"": Period7d, "24h": Period24h, "90d": Period90d} {
		if got, err := Parse(raw); err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"1y", "7D"} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q): want an error", raw)
		}
	}
}

func TestFor(t *testing.T) {
	end := time.Date(2026, 10, 3, 14, 23, 5, 0, time.UTC)
	for p, buckets := range map[Period]int{Period24h: 8, Period7d: 7, Period30d: 10, Period90d: 15} {
		w := For(p, end)
		if w.Buckets() != buckets || w.Start.Add(time.Duration(buckets)*w.Bucket) != end {
			t.Errorf("%s: %d buckets of %s from %s, want %d whole buckets ending at %s", p, w.Buckets(), w.Bucket, w.Start, buckets, end)
		}
		if got := w.Start.Sub(w.PreviousStart()); got != end.Sub(w.Start) {
			t.Errorf("%s: previous window lasts %s, want %s", p, got, end.Sub(w.Start))
		}
	}
}
