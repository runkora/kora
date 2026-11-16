package triggers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	eventsclient "github.com/runkora/kora/backend/internal/platform/clients/events"
)

// fakeStream serves records after the cursor once, then fails with end.
type fakeStream struct {
	mu      sync.Mutex
	records []eventsclient.Record
	afters  []int64
	end     error
}

func (f *fakeStream) Stream(_ context.Context, after int64, handle func(eventsclient.Record) error) error {
	f.mu.Lock()
	f.afters = append(f.afters, after)
	f.mu.Unlock()
	for _, rec := range f.records {
		if rec.Seq <= after {
			continue
		}
		if err := handle(rec); err != nil {
			return err
		}
	}
	return f.end
}

type fakeLocker struct{ free bool }

func (f fakeLocker) TryLock(ctx context.Context, _ string) (context.Context, func(), bool, error) {
	if !f.free {
		return nil, nil, false, nil
	}
	held, cancel := context.WithCancel(ctx)
	return held, cancel, true, nil
}

// failingHandler fails on one seq, so the cursor must stop before it.
type failingHandler struct {
	mu     sync.Mutex
	seen   []string
	failAt string
}

func (h *failingHandler) HandleEvent(_ context.Context, envelope []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if string(envelope) == h.failAt {
		return errors.New("db down")
	}
	h.seen = append(h.seen, string(envelope))
	return nil
}

func records(envelopes ...string) []eventsclient.Record {
	out := make([]eventsclient.Record, len(envelopes))
	for i, e := range envelopes {
		out[i] = eventsclient.Record{Seq: int64(i + 1), Event: []byte(e)}
	}
	return out
}

func TestEventConsumer_AdvancesTheCursorAfterEachEvent(t *testing.T) {
	repo := newMockRepository()
	stream := &fakeStream{records: records(`"a"`, `"b"`, `"c"`), end: errors.New("closed")}
	handler := &failingHandler{failAt: `"c"`}
	c := NewEventConsumer(handler, repo, stream, fakeLocker{free: true}, silentLogger())

	if err := c.consume(context.Background()); err == nil {
		t.Fatal("consume should return the handler's failure")
	}
	if cur, _ := repo.EventCursor(context.Background()); cur != 2 {
		t.Errorf("cursor = %d, want 2: an event whose deliveries failed is read again", cur)
	}
	handler.failAt = ""
	_ = c.consume(context.Background())
	if cur, _ := repo.EventCursor(context.Background()); cur != 3 || stream.afters[1] != 2 {
		t.Errorf("cursor = %d after reading from %d, want 3 from 2", cur, stream.afters[1])
	}
}

func TestEventConsumer_WaitsForTheLock(t *testing.T) {
	repo := newMockRepository()
	stream := &fakeStream{records: records(`"a"`)}
	c := NewEventConsumer(&failingHandler{}, repo, stream, fakeLocker{free: false}, silentLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c.consumeWhileHeld(ctx)
	if len(stream.afters) != 0 {
		t.Error("a replica without the lock must not read the stream")
	}
}
