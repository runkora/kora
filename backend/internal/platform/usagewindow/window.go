// Package usagewindow defines the reporting windows the Agents page offers, so
// the inference gateway and orbit-agents cut them the same way.
package usagewindow

import (
	"errors"
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

// ErrInvalidPeriod is returned by Parse for a value that is not a period.
var ErrInvalidPeriod = errors.New("period must be one of 24h, 7d, 30d, 90d")

const day = 24 * time.Hour

// sizes returns a period's length and chart bucket width, which divides the
// length. An unknown period gets the 7d sizes, so a bucket is never zero.
func sizes(p Period) (length, bucket time.Duration) {
	switch p {
	case Period24h:
		return day, 3 * time.Hour
	case Period30d:
		return 30 * day, 3 * day
	case Period90d:
		return 90 * day, 6 * day
	default:
		return 7 * day, day
	}
}

// Parse validates a period query value. An empty value is 7d.
func Parse(raw string) (Period, error) {
	switch p := Period(raw); p {
	case "":
		return Period7d, nil
	case Period24h, Period7d, Period30d, Period90d:
		return p, nil
	}
	return "", ErrInvalidPeriod
}

// Window is a period's trailing window [Start, End). Buckets count from Start.
type Window struct {
	Start, End time.Time
	Bucket     time.Duration
}

// For returns a period's window ending at end.
func For(p Period, end time.Time) Window {
	length, bucket := sizes(p)
	return Window{Start: end.Add(-length), End: end, Bucket: bucket}
}
