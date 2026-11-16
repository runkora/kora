package koraevents

import (
	"context"
	"sync"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// subscriberBuffer is how many batches a stream may fall behind before the
// hub drops it. A dropped stream catches up from the log.
const subscriberBuffer = 64

// hubStore is the store surface the hub uses.
type hubStore interface {
	After(ctx context.Context, f Filter, seq int64) ([]Record, error)
	LatestSeq(ctx context.Context) (int64, error)
}

// Hub reads each new event from the log once and fans it out to the
// replica's open streams in memory.
type Hub struct {
	store  hubStore
	logger logging.Logger

	mu     sync.Mutex
	cursor int64
	subs   map[chan []Record]struct{}
}

// NewHub returns a hub that has not read the log yet.
func NewHub(store hubStore, logger logging.Logger) *Hub {
	return &Hub{store: store, logger: logger, subs: map[chan []Record]struct{}{}}
}

// Run starts at the end of the log and fans out new events on each log
// notification and every tick, until ctx ends.
func (h *Hub) Run(ctx context.Context, l *Listener) error {
	cursor, err := h.store.LatestSeq(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.cursor = cursor
	h.mu.Unlock()
	go every(ctx, l.log, tick, func() { h.fetch(ctx) })
	return nil
}

// Subscribe returns a channel of the event batches after position, in seq
// order. The hub closes it when the stream falls behind. cancel releases it.
func (h *Hub) Subscribe() (ch <-chan []Record, position int64, cancel func()) {
	c := make(chan []Record, subscriberBuffer)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	position = h.cursor
	h.mu.Unlock()
	return c, position, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[c]; ok {
			delete(h.subs, c)
			close(c)
		}
	}
}

func (h *Hub) fetch(ctx context.Context) {
	for {
		h.mu.Lock()
		cursor := h.cursor
		h.mu.Unlock()
		batch, err := h.store.After(ctx, Filter{}, cursor)
		if err != nil {
			if ctx.Err() == nil {
				h.logger.Error(ctx, "event fan-out read failed", logging.Field{Key: "error", Value: err.Error()})
			}
			return
		}
		if len(batch) == 0 {
			return
		}
		h.broadcast(batch)
		if len(batch) < readBatchSize {
			return
		}
	}
}

// broadcast advances the cursor and sends batch under one lock, so a new
// subscriber sees either both or neither.
func (h *Hub) broadcast(batch []Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cursor = batch[len(batch)-1].Seq
	for c := range h.subs {
		select {
		case c <- batch:
		default:
			delete(h.subs, c)
			close(c)
		}
	}
}
