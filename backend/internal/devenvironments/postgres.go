package devenvironments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository is the Postgres-backed Repository implementation: the
// owner/audit registry only — lifecycle state is never stored here (see
// repository.go).
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs a PostgresRepository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, rec *Record) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dev_environments (id, owner, ref, created_at)
		VALUES ($1, $2, $3, $4)`,
		rec.ID, rec.Owner, rec.Ref, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert dev environment: %w", err)
	}
	return nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (*Record, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, owner, ref, created_at, deleted_at
		FROM dev_environments WHERE id = $1`, id)
	return scanRecord(row)
}

func (r *PostgresRepository) ListByOwner(ctx context.Context, owner string) ([]Record, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, owner, ref, created_at, deleted_at
		FROM dev_environments WHERE owner = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, fmt.Errorf("query dev environments by owner: %w", err)
	}
	defer rows.Close()
	return collectRecords(rows)
}

func (r *PostgresRepository) ListAll(ctx context.Context) ([]Record, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, owner, ref, created_at, deleted_at
		FROM dev_environments WHERE deleted_at IS NULL
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query all dev environments: %w", err)
	}
	defer rows.Close()
	return collectRecords(rows)
}

func (r *PostgresRepository) MarkDeleted(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE dev_environments SET deleted_at = $1
		WHERE id = $2 AND deleted_at IS NULL`, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("mark dev environment deleted: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) CountActiveByOwner(ctx context.Context, owner string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM dev_environments
		WHERE owner = $1 AND deleted_at IS NULL`, owner).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active dev environments: %w", err)
	}
	return count, nil
}

// scanner abstracts pgx.Row/pgx.Rows so scanRecord works for both a single
// QueryRow result and each row of a Query result set.
type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(s scanner) (*Record, error) {
	var rec Record
	err := s.Scan(&rec.ID, &rec.Owner, &rec.Ref, &rec.CreatedAt, &rec.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan dev environment: %w", err)
	}
	return &rec, nil
}

func collectRecords(rows pgx.Rows) ([]Record, error) {
	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}
