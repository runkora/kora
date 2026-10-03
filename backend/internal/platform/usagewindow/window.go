// Package usagewindow defines the reporting windows the Agents page offers, so
// the inference gateway and orbit-agents cut them the same way.
package usagewindow

import (
	"fmt"
	"time"
)

// Period names a reporting window.
type Period string

const (
	Period24h Period = "24h"
	Period7d  Period = "7d"
	Period30d Period = "30d"
	Period90d Period = "90d"
)

const day = 24 * time.Hour

// windows are each period's length and chart bucket width. The width divides the
// length, so every bucket is whole: 8, 7, 10 and 15 points.
var windows = map[Period]struct{ length, bucket time.Duration }{
	Period24h: {day, 3 * time.Hour},
	Period7d:  {7 * day, day},
	Period30d: {30 * day, 3 * day},
	Period90d: {90 * day, 6 * day},
}

// Parse validates a period query value. An empty value is 7d.
func Parse(raw string) (Period, error) {
	if raw == "" {
		return Period7d, nil
	}
	if _, ok := windows[Period(raw)]; !ok {
		return "", fmt.Errorf("period must be one of 24h, 7d, 30d, 90d: %q", raw)
	}
	return Period(raw), nil
}

// Window is a period's trailing window [Start, End). Buckets count from Start.
type Window struct {
	Period     Period
	Start, End time.Time
	Bucket     time.Duration
}

// For returns a period's window ending at end.
func For(p Period, end time.Time) Window {
	w := windows[p]
	return Window{Period: p, Start: end.Add(-w.length), End: end, Bucket: w.bucket}
}

// Buckets is the number of chart points in the window.
func (w Window) Buckets() int { return int(w.End.Sub(w.Start) / w.Bucket) }

// PreviousStart is where the window of the same length before this one begins.
func (w Window) PreviousStart() time.Time { return w.Start.Add(-w.End.Sub(w.Start)) }
