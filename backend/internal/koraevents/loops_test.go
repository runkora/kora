package koraevents

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// scriptedRelay returns its results in order and ends the test's context on
// the last one.
type scriptedRelay struct {
	results []RelayResult
	calls   int
	cancel  context.CancelFunc
}

func (s *scriptedRelay) RelayBatch(context.Context) (RelayResult, error) {
	r := s.results[s.calls]
	s.calls++
	if s.calls == len(s.results) {
		s.cancel()
	}
	return r, nil
}

// countingCounter sums what the relay adds to the dead-letter counter.
type countingCounter struct {
	noop.Int64Counter
	total int64
}

func (c *countingCounter) Add(_ context.Context, n int64, _ ...metric.AddOption) { c.total += n }

func TestRunRelay_DrainsFullBatchesAndCountsDeadLetters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bad := errors.New("malformed")
	store := &scriptedRelay{cancel: cancel, results: []RelayResult{
		{Taken: relayBatchSize, DeadLettered: map[string]error{"a": bad}},
		{Taken: relayBatchSize},
		{Taken: 3, DeadLettered: map[string]error{"b": bad, "c": bad}},
	}}
	counter := &countingCounter{}

	RunRelay(ctx, store, NewListener(nil, testLogger), counter, testLogger)

	if store.calls != 3 {
		t.Errorf("RelayBatch calls = %d, want 3: drain while full, stop on a short batch", store.calls)
	}
	if counter.total != 3 {
		t.Errorf("dead-lettered count = %d, want 3", counter.total)
	}
}

func TestEvery_RunsOnStartSignalAndTick(t *testing.T) {
	for name, tc := range map[string]struct {
		signalled bool
		interval  time.Duration
	}{
		"signal": {signalled: true, interval: time.Hour},
		"tick":   {interval: time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			signal := make(chan struct{}, 1)
			if tc.signalled {
				signal <- struct{}{}
			}
			runs := 0
			every(ctx, signal, tc.interval, func() {
				// select may still pick a ready tick over the cancelled context.
				if ctx.Err() != nil {
					return
				}
				runs++
				if runs == 2 {
					cancel()
				}
			})
			if runs != 2 {
				t.Errorf("runs = %d, want 2: once on start, once on the %s", runs, name)
			}
		})
	}
}
