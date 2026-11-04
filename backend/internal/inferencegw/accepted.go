package inferencegw

import (
	"context"
	"sync"
)

// acceptedSignal is closed once an upstream has accepted a request, meaning it
// returned a 2xx and sent its first stream event, so a request-level rejection
// can no longer happen.
type acceptedSignal struct {
	once sync.Once
	ch   chan struct{}
}

type acceptedKey struct{}

// WithAcceptedSignal returns a context that connectors can use to report that
// the upstream accepted the request, and a channel closed when they do. The
// non-streaming handler uses it to defer committing a 200 until a 4xx is no
// longer possible.
func WithAcceptedSignal(ctx context.Context) (context.Context, <-chan struct{}) {
	sig := &acceptedSignal{ch: make(chan struct{})}
	return context.WithValue(ctx, acceptedKey{}, sig), sig.ch
}

// NotifyAccepted reports that the upstream accepted the request. It is safe to
// call repeatedly and is a no-op when ctx carries no signal.
func NotifyAccepted(ctx context.Context) {
	if sig, ok := ctx.Value(acceptedKey{}).(*acceptedSignal); ok {
		sig.once.Do(func() { close(sig.ch) })
	}
}
