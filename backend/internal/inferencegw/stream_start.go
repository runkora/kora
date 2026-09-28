package inferencegw

import (
	"context"
	"fmt"
	"time"
)

// streamStartWait bounds how long a streaming handler holds back its 200 while
// waiting for the first upstream event, as the non-streaming fast path does. A
// var so tests can shrink it.
var streamStartWait = 5 * time.Second

// awaitStreamStart lets a provider failure before any output reach the caller
// as a status code (a 403 is final for agent runtimes) rather than an error
// event after 200. It returns that failure, or a channel replaying the stream.
func awaitStreamStart[E any](ctx context.Context, events <-chan E, errOf func(E) error) (<-chan E, error) {
	timer := time.NewTimer(streamStartWait)
	defer timer.Stop()

	var first E
	select {
	case ev, ok := <-events:
		if !ok {
			return events, nil
		}
		if err := errOf(ev); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrProviderError, err)
		}
		first = ev
	case <-timer.C:
		return events, nil
	case <-ctx.Done():
		return events, nil
	}

	out := make(chan E, 1)
	out <- first
	go func() {
		defer close(out)
		for ev := range events {
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
