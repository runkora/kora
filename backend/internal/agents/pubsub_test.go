package agents

import (
	"sync"
	"testing"
)

func TestPubSub_PublishToSubscriber(t *testing.T) {
	ps := NewPubSub()
	ch, cancel := ps.Subscribe("run-1")
	defer cancel()

	ev := PushedEvent{Type: "text_delta"}
	ps.Publish("run-1", ev)

	select {
	case got := <-ch:
		if got.Type != ev.Type {
			t.Errorf("event type = %q, want %q", got.Type, ev.Type)
		}
	default:
		t.Error("expected event on channel, got nothing")
	}
}

func TestPubSub_NoSubscriber_DoesNotPanic(t *testing.T) {
	ps := NewPubSub()
	// Should not block or panic.
	ps.Publish("run-nobody", PushedEvent{Type: "text_delta"})
}

func TestPubSub_MultipleSubscribers_AllReceive(t *testing.T) {
	ps := NewPubSub()

	ch1, cancel1 := ps.Subscribe("run-1")
	ch2, cancel2 := ps.Subscribe("run-1")
	defer cancel1()
	defer cancel2()

	ps.Publish("run-1", PushedEvent{Type: "ping"})

	for _, ch := range []<-chan PushedEvent{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Type != "ping" {
				t.Errorf("event type = %q, want ping", ev.Type)
			}
		default:
			t.Error("expected event on subscriber channel")
		}
	}
}

func TestPubSub_UnsubscribeClosesChannel(t *testing.T) {
	ps := NewPubSub()
	ch, cancel := ps.Subscribe("run-1")

	cancel()

	// Channel must be closed after cancel.
	_, open := <-ch
	if open {
		t.Error("channel should be closed after cancel()")
	}
}

func TestPubSub_CancelTwice_IsSafeNoOp(t *testing.T) {
	ps := NewPubSub()
	ch, cancel := ps.Subscribe("run-1")

	cancel()
	// A second call must not panic (closing ch twice) or double-record the
	// session duration metric — see pubsub.go's sync.Once guard.
	cancel()

	_, open := <-ch
	if open {
		t.Error("channel should be closed after cancel()")
	}
}

func TestPubSub_UnsubscribeRemovesMapEntry(t *testing.T) {
	ps := NewPubSub()
	_, cancel := ps.Subscribe("run-1")
	cancel()

	ps.mu.RLock()
	_, exists := ps.subs["run-1"]
	ps.mu.RUnlock()

	if exists {
		t.Error("map entry should be removed after last subscriber unsubscribes")
	}
}

func TestPubSub_DifferentRunsAreIsolated(t *testing.T) {
	ps := NewPubSub()

	ch1, cancel1 := ps.Subscribe("run-A")
	ch2, cancel2 := ps.Subscribe("run-B")
	defer cancel1()
	defer cancel2()

	ps.Publish("run-A", PushedEvent{Type: "for-A"})

	select {
	case <-ch2:
		t.Error("run-B should not receive events published to run-A")
	default:
	}
	select {
	case ev := <-ch1:
		if ev.Type != "for-A" {
			t.Errorf("event type = %q, want for-A", ev.Type)
		}
	default:
		t.Error("run-A should receive its own event")
	}
}

func TestPubSub_Concurrent(t *testing.T) {
	ps := NewPubSub()

	const runs = 10
	const msgs = 20

	var wg sync.WaitGroup
	for i := range runs {
		runID := "run-" + string(rune('A'+i))
		ch, cancel := ps.Subscribe(runID)

		wg.Go(func() {
			defer cancel()
			received := 0
			for range ch {
				received++
				if received == msgs {
					return
				}
			}
		})

		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for range msgs {
				ps.Publish(id, PushedEvent{Type: "tick"})
			}
		}(runID)
	}
	wg.Wait()
}
