package sessionmanager

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// traceNotifier records the span context each run completion was reported under.
type traceNotifier struct {
	NoopNotifier
	mu        sync.Mutex
	completed []trace.SpanContext
}

func (n *traceNotifier) CompleteRun(ctx context.Context, _, _ string, _ bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.completed = append(n.completed, trace.SpanContextFromContext(ctx))
	return nil
}

// Lifecycle calls after the create request has returned stay in the create-session trace.
func TestSessionLifecycleStaysInCreateTrace(t *testing.T) {
	n := &traceNotifier{}
	reg, _ := newTestRegistry(t, n)
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
	})
	reqCtx, cancel := context.WithCancel(trace.ContextWithSpanContext(context.Background(), parent))
	if err := reg.Create(reqCtx, SessionConfig{RunID: "run-1", Runner: newStubRunner(nil)}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := reg.Close(context.Background(), "run-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		n.mu.Lock()
		got := append([]trace.SpanContext(nil), n.completed...)
		n.mu.Unlock()
		if len(got) == 1 {
			if got[0].TraceID() != parent.TraceID() || got[0].SpanID() != parent.SpanID() {
				t.Fatalf("completion span context = %v, want the create-session span", got[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("completions = %d, want 1", len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
