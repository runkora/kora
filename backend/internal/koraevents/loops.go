package koraevents

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	// tick runs the relay and the stream fan-out when no notification arrives.
	tick = time.Second
	// retentionInterval is how often the retention sweep runs.
	retentionInterval = time.Hour
	// reconnectDelay paces reconnecting a lost LISTEN connection.
	reconnectDelay = time.Second
)

// Listener holds the replica's LISTEN connection and wakes the relay on an
// outbox notification and the hub on a log notification.
type Listener struct {
	pool   *pgxpool.Pool
	logger logging.Logger
	outbox chan struct{}
	log    chan struct{}
}

// NewListener returns a listener on pool.
func NewListener(pool *pgxpool.Pool, logger logging.Logger) *Listener {
	return &Listener{pool: pool, logger: logger, outbox: make(chan struct{}, 1), log: make(chan struct{}, 1)}
}

// Run listens until ctx ends, reconnecting a lost connection.
func (l *Listener) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := l.listen(ctx); err != nil && ctx.Err() == nil {
			l.logger.Warn(ctx, "event listener connection lost", logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
		case <-time.After(reconnectDelay):
		}
	}
}

func (l *Listener) listen(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// A connection that has run LISTEN must not return to the pool.
	raw := conn.Hijack()
	defer func() { _ = raw.Close(context.WithoutCancel(ctx)) }()

	for _, channel := range []string{events.OutboxChannel, LogChannel} {
		if _, err := raw.Exec(ctx, "LISTEN "+channel); err != nil {
			return err
		}
	}
	// Notifications sent while the connection was down are lost, so catch up.
	wake(l.outbox)
	wake(l.log)
	for {
		n, err := raw.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if n.Channel == LogChannel {
			wake(l.log)
		} else {
			wake(l.outbox)
		}
	}
}

// wake signals ch without blocking. One pending signal covers any number.
func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// relayStore is the store surface the relay uses.
type relayStore interface {
	RelayBatch(ctx context.Context) (RelayResult, error)
}

// RunRelay moves outbox rows into the log on each outbox notification and
// every tick, until ctx ends. Every replica runs it. The relay lock
// serializes them.
// deadLettered counts the outbox rows moved to event_outbox_dead.
func RunRelay(ctx context.Context, store relayStore, l *Listener, deadLettered metric.Int64Counter, logger logging.Logger) {
	every(ctx, l.outbox, tick, func() {
		for {
			result, err := store.RelayBatch(ctx)
			if err != nil {
				if ctx.Err() == nil {
					logger.Error(ctx, "event relay batch failed", logging.Field{Key: "error", Value: err.Error()})
				}
				return
			}
			for id, reason := range result.DeadLettered {
				logger.Error(ctx, "moved an outbox row the event log cannot hold to event_outbox_dead",
					logging.Field{Key: "outbox_id", Value: id}, logging.Field{Key: "error", Value: reason.Error()})
			}
			deadLettered.Add(ctx, int64(len(result.DeadLettered)))
			if result.Taken < relayBatchSize {
				return
			}
		}
	})
}

// every runs fn now, then on each signal and interval until ctx ends.
func every(ctx context.Context, signal <-chan struct{}, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		fn()
		select {
		case <-ctx.Done():
			return
		case <-signal:
		case <-ticker.C:
		}
	}
}
