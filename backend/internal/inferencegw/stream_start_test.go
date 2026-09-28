package inferencegw

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func eventErr(ev AnthropicStreamEvent) error { return ev.Err }

// An auth failure as a stream's first event must come back before any output,
// so the handler can answer 403 instead of an error event after 200.
func TestAwaitStreamStart_FirstEventErrorIsReturned(t *testing.T) {
	t.Parallel()
	events := make(chan AnthropicStreamEvent, 1)
	events <- AnthropicStreamEvent{Err: &ConnectorError{Provider: ProviderBedrock, Code: "AUTH_FAILED", HTTPStatus: 403}}
	close(events)

	out, err := awaitStreamStart(context.Background(), events, eventErr)
	if out != nil || !errors.Is(err, ErrProviderError) {
		t.Fatalf("got (%v, %v), want (nil, ErrProviderError)", out, err)
	}
	if resp := inferenceErrorResponse(err); resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

// A transient first-event failure keeps a retryable status.
func TestAwaitStreamStart_TransientFirstEventStaysRetryable(t *testing.T) {
	t.Parallel()
	events := make(chan AnthropicStreamEvent, 1)
	events <- AnthropicStreamEvent{Err: &ConnectorError{Provider: ProviderBedrock, Code: "THROTTLED", HTTPStatus: 429, Retryable: true}}
	close(events)

	_, err := awaitStreamStart(context.Background(), events, eventErr)
	if resp := inferenceErrorResponse(err); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// A healthy stream is replayed in full and in order, first event included.
func TestAwaitStreamStart_ReplaysHealthyStream(t *testing.T) {
	t.Parallel()
	events := make(chan AnthropicStreamEvent, 3)
	for _, typ := range []string{"message_start", "content_block_delta", "message_stop"} {
		ev := AnthropicStreamEvent{}
		ev.Event.Type = typ
		events <- ev
	}
	close(events)

	out, err := awaitStreamStart(context.Background(), events, eventErr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var got []string
	for ev := range out {
		got = append(got, ev.Event.Type)
	}
	if len(got) != 3 || got[0] != "message_start" || got[2] != "message_stop" {
		t.Errorf("replayed %v, want all three events in order", got)
	}
}

// A slow first event does not hold the response back past the wait.
func TestAwaitStreamStart_SlowStreamPassesThrough(t *testing.T) {
	orig := streamStartWait
	streamStartWait = 10 * time.Millisecond
	t.Cleanup(func() { streamStartWait = orig })

	events := make(chan AnthropicStreamEvent)
	out, err := awaitStreamStart(context.Background(), events, eventErr)
	if err != nil || out != (<-chan AnthropicStreamEvent)(events) {
		t.Fatalf("got (%v, %v), want the original channel and no error", out, err)
	}
}
