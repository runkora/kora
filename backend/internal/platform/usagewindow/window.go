// Package usagewindow defines the reporting windows the Agents page offers
// (24h, 7d, 30d, 90d) so the inference gateway and orbit-agents cut the same
// windows from the same clock.
package usagewindow

import (
	"errors"
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

// Default is the window used when a caller names none.
const Default = Period7d

// ErrInvalidPeriod is returned for a period that is not one of the four windows.
var ErrInvalidPeriod = errors.New("period must be one of 24h, 7d, 30d, 90d")

// spec describes how one period is cut: its length, the width of a chart
// bucket, and the grid the first bucket is aligned to.
type spec struct {
	length time.Duration
	bucket time.Duration
	grid   time.Duration
}

const day = 24 * time.Hour

// The bucket widths give about 8, 7, 10 and 13 chart points, which suits the
// axis labels the page draws. Buckets sit on a UTC grid (the bucket for 24h, a
// day for the rest), so the first and last points can be partial.
var specs = map[Period]spec{
	Period24h: {length: day, bucket: 3 * time.Hour, grid: 3 * time.Hour},
	Period7d:  {length: 7 * day, bucket: day, grid: day},
	Period30d: {length: 30 * day, bucket: 3 * day, grid: day},
	Period90d: {length: 90 * day, bucket: 7 * day, grid: day},
}

// Parse validates a period query value. An empty value is the default.
func Parse(raw string) (Period, error) {
	if raw == "" {
		return Default, nil
	}
	p := Period(raw)
	if _, ok := specs[p]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidPeriod, raw)
	}
	return p, nil
}

// Window is one concrete reporting window: [Start, End) is the trailing window
// ending now, and the previous window of the same length ends where this one
// starts.
type Window struct {
	Period Period
	Start  time.Time
	End    time.Time
	// Origin is where the first chart bucket begins: Start rounded down to the
	// period's grid. Bucket i covers [Origin+i*Bucket, Origin+(i+1)*Bucket).
	Origin time.Time
	// Bucket is the width of one chart point.
	Bucket time.Duration
}

// For returns the trailing window for a period at the given instant.
func For(p Period, now time.Time) Window {
	s := specs[p]
	end := now.UTC()
	start := end.Add(-s.length)
	return Window{Period: p, Start: start, End: end, Origin: start.Truncate(s.grid), Bucket: s.bucket}
}

// Length is the duration of the window.
func (w Window) Length() time.Duration { return w.End.Sub(w.Start) }

// PreviousStart is where the window before this one begins.
func (w Window) PreviousStart() time.Time { return w.Start.Add(-w.Length()) }

// Buckets is the number of chart points, from the bucket holding Start to the
// one holding End.
func (w Window) Buckets() int {
	return int((w.End.Sub(w.Origin) + w.Bucket - 1) / w.Bucket)
}
