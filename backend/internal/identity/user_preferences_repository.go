package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxPreferencesDocBytes is enforced at the handler layer before any upsert.
// The database CHECK constraint is a belt-and-suspenders guard.
const maxPreferencesDocBytes = 65536

// UserPreferencesRepository defines the storage contract for user preferences.
type UserPreferencesRepository interface {
	// Get returns the stored preferences document for (userID, tenantID).
	// Returns ErrUserPreferencesNotFound if no row exists yet.
	Get(ctx context.Context, userID, tenantID string) (json.RawMessage, error)

	// Upsert replaces the stored preferences document in its entirety.
	// Creates a row if none exists (lazy creation on first write).
	Upsert(ctx context.Context, userID, tenantID string, prefs json.RawMessage) error

	// Delete removes the stored preferences document.
	// Subsequent Get calls return ErrUserPreferencesNotFound.
	Delete(ctx context.Context, userID, tenantID string) error
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresUserPreferencesRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresUserPreferencesRepository creates a UserPreferencesRepository backed by PostgreSQL.
func NewPostgresUserPreferencesRepository(pool *pgxpool.Pool) UserPreferencesRepository {
	return &postgresUserPreferencesRepository{pool: pool}
}

func (r *postgresUserPreferencesRepository) Get(ctx context.Context, userID, tenantID string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := r.pool.QueryRow(ctx, `
		SELECT prefs
		FROM   user_preferences
		WHERE  user_id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserPreferencesNotFound
		}
		return nil, fmt.Errorf("get user preferences: %w", err)
	}
	return raw, nil
}

func (r *postgresUserPreferencesRepository) Upsert(ctx context.Context, userID, tenantID string, prefs json.RawMessage) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_preferences (user_id, tenant_id, prefs, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (user_id, tenant_id) DO UPDATE
		SET prefs      = EXCLUDED.prefs,
		    updated_at = EXCLUDED.updated_at
	`, userID, tenantID, prefs)
	if err != nil {
		return fmt.Errorf("upsert user preferences: %w", err)
	}
	return nil
}

func (r *postgresUserPreferencesRepository) Delete(ctx context.Context, userID, tenantID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM user_preferences
		WHERE user_id = $1 AND tenant_id = $2
	`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("delete user preferences: %w", err)
	}
	return nil
}

// ─── Noop implementation ──────────────────────────────────────────────────────

type noopUserPreferencesRepository struct{}

func (noopUserPreferencesRepository) Get(_ context.Context, _, _ string) (json.RawMessage, error) {
	return nil, ErrUserPreferencesNotFound
}
func (noopUserPreferencesRepository) Upsert(_ context.Context, _, _ string, _ json.RawMessage) error {
	return nil
}
func (noopUserPreferencesRepository) Delete(_ context.Context, _, _ string) error { return nil }

// Compile-time interface checks.
var _ UserPreferencesRepository = (*postgresUserPreferencesRepository)(nil)
var _ UserPreferencesRepository = noopUserPreferencesRepository{}
