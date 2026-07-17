package example

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nullableTime returns nil for a zero time so the query's COALESCE falls through
// to the column's DEFAULT. Passing the zero value directly would write a zero
// timestamp and defeat the schema default.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// PostgresRepository is the Postgres-backed implementation of Repository.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a Postgres-backed repository over an existing
// pgx pool (obtained from storage.DB.Pool()).
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// GetUserByID retrieves a user by id, returning ErrUserNotFound when absent.
func (r *PostgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	const query = `SELECT id, email, created_at FROM users WHERE id = $1`

	var user User
	err := r.pool.QueryRow(ctx, query, id).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &user, nil
}

// CreateUser inserts a new user, mapping a unique-constraint violation to
// ErrDuplicateKey.
func (r *PostgresRepository) CreateUser(ctx context.Context, user *User) error {
	const query = `INSERT INTO users (id, email, created_at) VALUES ($1, $2, COALESCE($3, CURRENT_TIMESTAMP))`

	if _, err := r.pool.Exec(ctx, query, user.ID, user.Email, nullableTime(user.CreatedAt)); err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// CreateUserWithProfile writes a user and profile atomically, demonstrating a
// transaction. The deferred Rollback is a no-op once Commit succeeds.
func (r *PostgresRepository) CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	const userQuery = `INSERT INTO users (id, email, created_at) VALUES ($1, $2, COALESCE($3, CURRENT_TIMESTAMP))`
	if _, err := tx.Exec(ctx, userQuery, user.ID, user.Email, nullableTime(user.CreatedAt)); err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("insert user: %w", err)
	}

	const profileQuery = `INSERT INTO profiles (user_id, name) VALUES ($1, $2)`
	if _, err := tx.Exec(ctx, profileQuery, profile.UserID, profile.Name); err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// CreateUsers inserts many users in a single round trip, demonstrating batching.
func (r *PostgresRepository) CreateUsers(ctx context.Context, users []*User) error {
	const query = `INSERT INTO users (id, email, created_at) VALUES ($1, $2, COALESCE($3, CURRENT_TIMESTAMP))`

	batch := &pgx.Batch{}
	for _, user := range users {
		batch.Queue(query, user.ID, user.Email, nullableTime(user.CreatedAt))
	}

	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := range users {
		if _, err := results.Exec(); err != nil {
			if isDuplicateKeyError(err) {
				return fmt.Errorf("batch insert user %d: %w", i, ErrDuplicateKey)
			}
			return fmt.Errorf("batch insert user %d: %w", i, err)
		}
	}
	return nil
}

// isDuplicateKeyError reports whether err is a Postgres unique_violation (23505).
func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
