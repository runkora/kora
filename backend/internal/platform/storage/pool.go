package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool runs queries on the transaction in the context when one is open, and on
// the connection pool otherwise. A repository that holds a Pool joins the
// transaction of an enclosing WithTransaction with no change to its methods.
type Pool struct {
	pool *pgxpool.Pool
}

// NewPool wraps p.
func NewPool(p *pgxpool.Pool) *Pool {
	return &Pool{pool: p}
}

// Exec runs sql on the context transaction or the pool.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Exec(ctx, sql, args...)
	}
	return p.pool.Exec(ctx, sql, args...)
}

// Query runs sql on the context transaction or the pool.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow runs sql on the context transaction or the pool.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	return p.pool.QueryRow(ctx, sql, args...)
}

// Begin opens a transaction, or a savepoint on the context transaction.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Begin(ctx)
	}
	return p.pool.Begin(ctx)
}
