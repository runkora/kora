package inferencegw

import (
	"context"
	"testing"
)

func TestAcceptedSignal(t *testing.T) {
	t.Parallel()
	NotifyAccepted(context.Background()) // no signal: must not panic

	ctx, ch := WithAcceptedSignal(context.Background())
	select {
	case <-ch:
		t.Fatal("signal closed before NotifyAccepted")
	default:
	}
	NotifyAccepted(ctx)
	NotifyAccepted(ctx) // idempotent
	select {
	case <-ch:
	default:
		t.Fatal("signal not closed after NotifyAccepted")
	}
}
