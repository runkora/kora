package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// TxFunc runs inside a transaction. ctx carries the transaction, so a Pool
// call made with it joins the transaction.
type TxFunc func(ctx context.Context, tx pgx.Tx) error

// Beginner opens a transaction. *pgxpool.Pool and *Pool satisfy it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type txContextKey struct{}

// TxFromContext returns the transaction opened by an enclosing WithTransaction.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}

// WithTransaction runs fn in a transaction that commits when fn returns nil and
// rolls back otherwise. A call inside an open transaction joins it, and the
// outermost call commits or rolls back.
func WithTransaction(ctx context.Context, db Beginner, fn TxFunc) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback after a commit is a no-op, so it also covers a panic in fn.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := fn(context.WithValue(ctx, txContextKey{}, tx), tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
