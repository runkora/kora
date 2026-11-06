package triggers

import (
	"context"
	"fmt"
	"time"

	eventsclient "github.com/runkora/kora/backend/internal/platform/clients/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// consumerLockKey names the advisory lock that admits one consumer at a time.
const consumerLockKey = "kora_triggers_event_consumer"

// eventStream is the kora-events internal stream.
type eventStream interface {
	Stream(ctx context.Context, after int64, handle func(eventsclient.Record) error) error
}

// sessionLocker takes a session-level advisory lock. held ends when the lock is lost.
type sessionLocker interface {
	TryLock(ctx context.Context, key string) (held context.Context, release func(), ok bool, err error)
}

// eventHandler records the deliveries of one event.
type eventHandler interface {
	HandleEvent(ctx context.Context, envelope []byte) error
}

// EventConsumer reads the event log from its cursor and fires event triggers.
// One replica consumes at a time, and the others retry for the lock.
type EventConsumer struct {
	handler eventHandler
	cursor  EventCursorStore
	stream  eventStream
	locker  sessionLocker
	logger  logging.Logger
	// retry is how long a replica waits to try the lock again, or to reopen a failed stream.
	retry   time.Duration
	stopped chan struct{}
}

// NewEventConsumer returns a consumer that has not started.
func NewEventConsumer(handler eventHandler, cursor EventCursorStore, stream eventStream, locker sessionLocker, logger logging.Logger) *EventConsumer {
	return &EventConsumer{handler: handler, cursor: cursor, stream: stream, locker: locker, logger: logger, retry: 10 * time.Second, stopped: make(chan struct{})}
}

// Start runs the consumer until ctx ends.
func (c *EventConsumer) Start(ctx context.Context) {
	go func() {
		defer close(c.stopped)
		for {
			c.consumeWhileHeld(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.retry):
			}
		}
	}()
}

// Wait blocks until the consumer has stopped, its last fan-out included, or ctx ends.
func (c *EventConsumer) Wait(ctx context.Context) {
	select {
	case <-c.stopped:
	case <-ctx.Done():
	}
}

// consumeWhileHeld takes the lock and reads the stream until the lock or ctx is lost.
func (c *EventConsumer) consumeWhileHeld(ctx context.Context) {
	held, release, ok, err := c.locker.TryLock(ctx, consumerLockKey)
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Error(ctx, "event consumer could not try its lock", logging.Field{Key: "error", Value: err.Error()})
		}
		return
	}
	if !ok {
		return
	}
	defer release()
	c.logger.Info(ctx, "event consumer started")
	for held.Err() == nil {
		// A stream that ended at token expiry reopens after a second. A failed one waits for retry.
		wait := time.Second
		if err := c.consume(held); err != nil && held.Err() == nil {
			c.logger.Error(ctx, "event stream failed", logging.Field{Key: "error", Value: err.Error()})
			wait = c.retry
		}
		select {
		case <-held.Done():
		case <-time.After(wait):
		}
	}
}

// consume reads the stream from the cursor and advances it after each event's deliveries are recorded.
func (c *EventConsumer) consume(ctx context.Context) error {
	after, err := c.cursor.EventCursor(ctx)
	if err != nil {
		return err
	}
	return c.stream.Stream(ctx, after, func(rec eventsclient.Record) error {
		if err := c.handler.HandleEvent(ctx, rec.Event); err != nil {
			return fmt.Errorf("handle event %d: %w", rec.Seq, err)
		}
		return c.cursor.AdvanceEventCursor(ctx, rec.Seq)
	})
}
