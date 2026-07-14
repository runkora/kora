package operator

import "time"

// Clock abstracts time so reconcilers with TTL/idle-timeout/sleep-wake logic
// are unit-testable without waiting on a real clock. Production wiring uses
// realClock; tests inject a fake that can be advanced explicitly. See
// docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md,
// Implementation §Ring 1.
type Clock interface {
	Now() time.Time
}

// realClock is the production Clock implementation.
type realClock struct{}

// NewRealClock returns the production Clock backed by time.Now.
func NewRealClock() Clock {
	return realClock{}
}

func (realClock) Now() time.Time {
	return time.Now()
}
