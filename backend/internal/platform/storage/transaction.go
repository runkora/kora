package storage

import (
	"context"
	"errors"
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

// WithSavepoint runs fn in a savepoint of the context transaction, releasing it
// when fn returns nil and rolling back to it otherwise. Outside a transaction
// it runs fn plainly. It writes the savepoint SQL itself: a pgx nested
// transaction cannot roll back once its release has failed.
func WithSavepoint(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, ok := TxFromContext(ctx)
	if !ok {
		return fn(ctx)
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT kora_savepoint`); err != nil {
		return fmt.Errorf("savepoint: %w", err)
	}
	err := fn(ctx)
	if err == nil {
		if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT kora_savepoint`); err == nil {
			return nil
		}
	}
	if _, rbErr := tx.Exec(context.WithoutCancel(ctx), `ROLLBACK TO SAVEPOINT kora_savepoint`); rbErr != nil {
		return errors.Join(err, fmt.Errorf("roll back savepoint: %w", rbErr))
	}
	return err
}
