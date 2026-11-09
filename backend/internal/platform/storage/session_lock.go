package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionLockPing is how often a held session lock checks its connection.
const sessionLockPing = 10 * time.Second

// SessionLocker takes session-level advisory locks, each on a connection of its own.
type SessionLocker struct {
	pool *pgxpool.Pool
	ping time.Duration
}

// NewSessionLocker takes locks on connections from p.
func NewSessionLocker(p *pgxpool.Pool) *SessionLocker {
	return &SessionLocker{pool: p, ping: sessionLockPing}
}

// TryLock takes pg_try_advisory_lock(hashtext(key)) on a dedicated connection.
// ok is false when another session holds the lock. held is canceled when the
// connection fails, since the lock went with it. release unlocks and returns
// the connection.
func (l *SessionLocker) TryLock(ctx context.Context, key string) (held context.Context, release func(), ok bool, err error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("acquire lock connection: %w", err)
	}
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, key).Scan(&ok); err != nil {
		conn.Release()
		return nil, nil, false, fmt.Errorf("try advisory lock: %w", err)
	}
	if !ok {
		conn.Release()
		return nil, nil, false, nil
	}

	held, cancel := context.WithCancel(ctx)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		ticker := time.NewTicker(l.ping)
		defer ticker.Stop()
		for {
			select {
			case <-held.Done():
				return
			case <-ticker.C:
				if conn.Ping(held) != nil {
					cancel()
					return
				}
			}
		}
	}()
	release = func() {
		cancel()
		<-watched
		unlockCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		// Closing the connection ends the session, which drops the lock too.
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext($1))`, key); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
		conn.Release()
	}
	return held, release, true, nil
}
