package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxFunc is a function that executes within a database transaction
type TxFunc func(tx pgx.Tx) error

// WithTransaction executes a function within a database transaction
// Automatically commits on success and rolls back on error
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn TxFunc) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Ensure rollback on panic or any early return.
	// After a successful Commit, Rollback is a no-op so this is always safe.
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p) // Re-panic after rollback
		}
		_ = tx.Rollback(ctx)
	}()

	// Execute the transactional function
	if err := fn(tx); err != nil {
		return err
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
