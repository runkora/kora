package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// One session holds the lock at a time, and release lets the next take it.
func TestPostgres_SessionLockIsExclusive(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	key := "test-" + uuid.NewString()
	a, b := NewSessionLocker(pool), NewSessionLocker(pool)

	_, release, ok, err := a.TryLock(ctx, key)
	if err != nil || !ok {
		t.Fatalf("first TryLock = %v, %v, want the lock", ok, err)
	}
	if _, _, ok, err := b.TryLock(ctx, key); err != nil || ok {
		t.Fatalf("second TryLock = %v, %v, want refused while held", ok, err)
	}
	release()
	_, releaseB, ok, err := b.TryLock(ctx, key)
	if err != nil || !ok {
		t.Fatalf("TryLock after release = %v, %v, want the lock", ok, err)
	}
	releaseB()
}

// A lock whose connection dies ends its context, and another session can take it.
func TestPostgres_SessionLockLostWithItsConnection(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	key := "test-" + uuid.NewString()
	l := &SessionLocker{pool: pool, ping: 20 * time.Millisecond}

	held, release, ok, err := l.TryLock(ctx, key)
	if err != nil || !ok {
		t.Fatalf("TryLock = %v, %v", ok, err)
	}
	defer release()
	if _, err := pool.Exec(ctx, `
		SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND objid = hashtext($1)::oid AND granted`, key); err != nil {
		t.Fatalf("terminate the lock's backend: %v", err)
	}
	select {
	case <-held.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("held context still open after its connection died")
	}
	_, releaseNext, ok, err := NewSessionLocker(pool).TryLock(ctx, key)
	if err != nil || !ok {
		t.Fatalf("TryLock after the holder died = %v, %v, want the lock", ok, err)
	}
	releaseNext()
}
