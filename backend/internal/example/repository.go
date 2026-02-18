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

var (
	// ErrUserNotFound is returned when a user cannot be found
	ErrUserNotFound = errors.New("user not found")

	// ErrDuplicateKey is returned when a unique constraint is violated
	ErrDuplicateKey = errors.New("duplicate key violation")
)

// User represents a user in the example domain
type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

// Profile represents a user profile in the example domain
type Profile struct {
	UserID string
	Name   string
}

// Repository defines the data access interface for the example feature
type Repository interface {
	GetUserByID(ctx context.Context, id string) (*User, error)
	CreateUser(ctx context.Context, user *User) error
	CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error
	CreateUsers(ctx context.Context, users []*User) error
}

// PostgresRepository implements Repository using PostgreSQL
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository creates a new PostgreSQL-backed repository
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		pool: pool,
	}
}

// GetUserByID retrieves a user by ID
func (r *PostgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `SELECT id, email, created_at FROM users WHERE id = $1`

	var user User
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// CreateUser inserts a new user
func (r *PostgresRepository) CreateUser(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (id, email, created_at)
		VALUES ($1, $2, $3)
	`

	_, err := r.pool.Exec(ctx, query, user.ID, user.Email, user.CreatedAt)
	if err != nil {
		// Check for unique constraint violation (duplicate key)
		if isDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

// CreateUserWithProfile demonstrates a transactional operation
// Creates both user and profile atomically
func (r *PostgresRepository) CreateUserWithProfile(ctx context.Context, user *User, profile *Profile) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // Rollback if not committed

	// Insert user
	userQuery := `
		INSERT INTO users (id, email, created_at)
		VALUES ($1, $2, $3)
	`
	if _, err := tx.Exec(ctx, userQuery, user.ID, user.Email, user.CreatedAt); err != nil {
		if isDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("insert user: %w", err)
	}

	// Insert profile
	profileQuery := `
		INSERT INTO profiles (user_id, name)
		VALUES ($1, $2)
	`
	if _, err := tx.Exec(ctx, profileQuery, profile.UserID, profile.Name); err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// CreateUsers demonstrates batch insertion
func (r *PostgresRepository) CreateUsers(ctx context.Context, users []*User) error {
	batch := &pgx.Batch{}

	query := `
		INSERT INTO users (id, email, created_at)
		VALUES ($1, $2, $3)
	`

	for _, user := range users {
		batch.Queue(query, user.ID, user.Email, user.CreatedAt)
	}

	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	// Process batch results
	for i := 0; i < len(users); i++ {
		_, err := results.Exec()
		if err != nil {
			if isDuplicateKeyError(err) {
				return fmt.Errorf("batch insert user %d: %w", i, ErrDuplicateKey)
			}
			return fmt.Errorf("batch insert user %d: %w", i, err)
		}
	}

	return nil
}

// isDuplicateKeyError checks if an error is a PostgreSQL unique constraint violation
func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 23505 is the PostgreSQL error code for unique_violation
		return pgErr.Code == "23505"
	}
	return false
}
