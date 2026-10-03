// Package usagewindow defines the reporting windows the Agents page offers
// (24h, 7d, 30d, 90d) so the inference gateway and orbit-agents cut the same
// windows from the same end instant.
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

const day = 24 * time.Hour

// spec is a period's length and chart bucket width. Each width divides the
// length, so every bucket is whole: 8, 7, 10 and 15 points.
type spec struct{ length, bucket time.Duration }

var specs = map[Period]spec{
	Period24h: {day, 3 * time.Hour},
	Period7d:  {7 * day, day},
	Period30d: {30 * day, 3 * day},
	Period90d: {90 * day, 6 * day},
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

// Window is the trailing window [Start, End) of one period. Buckets count from
// Start, and the previous window of the same length ends where this one starts.
type Window struct {
	Period Period
	Start  time.Time
	End    time.Time
	Bucket time.Duration
}

// For returns a period's window ending at end.
func For(p Period, end time.Time) Window {
	s := specs[p]
	end = end.UTC()
	return Window{Period: p, Start: end.Add(-s.length), End: end, Bucket: s.bucket}
}

// Length is the duration of the window.
func (w Window) Length() time.Duration { return w.End.Sub(w.Start) }

// PreviousStart is where the window before this one begins.
func (w Window) PreviousStart() time.Time { return w.Start.Add(-w.Length()) }

// Buckets is the number of chart points in the window.
func (w Window) Buckets() int { return int(w.Length() / w.Bucket) }
