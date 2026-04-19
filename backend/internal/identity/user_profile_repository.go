package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserProfileRepository defines the storage contract for user profiles.
type UserProfileRepository interface {
	// Upsert inserts or updates a user profile. Safe to call on every login.
	Upsert(ctx context.Context, profile UserProfile) error

	// GetByID returns a user profile by (id, tenantID).
	// Returns ErrUserProfileNotFound if no record exists.
	GetByID(ctx context.Context, tenantID, id string) (UserProfile, error)

	// List returns a page of user profiles for a tenant.
	// When filter.IDs is non-empty, only those IDs are returned.
	// Also returns the total number of matching records for pagination UI.
	List(ctx context.Context, tenantID string, filter UserProfileFilter) ([]UserProfile, int, error)
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresUserProfileRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresUserProfileRepository creates a UserProfileRepository backed by PostgreSQL.
func NewPostgresUserProfileRepository(pool *pgxpool.Pool) UserProfileRepository {
	return &postgresUserProfileRepository{pool: pool}
}

func (r *postgresUserProfileRepository) Upsert(ctx context.Context, p UserProfile) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_profiles (id, tenant_id, email, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (id, tenant_id) DO UPDATE
		SET email        = EXCLUDED.email,
		    display_name = EXCLUDED.display_name,
		    updated_at   = EXCLUDED.updated_at
	`, p.ID, p.TenantID, p.Email, p.DisplayName, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert user profile: %w", err)
	}
	return nil
}

func (r *postgresUserProfileRepository) GetByID(ctx context.Context, tenantID, id string) (UserProfile, error) {
	var p UserProfile
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, display_name, created_at, updated_at
		FROM   user_profiles
		WHERE  id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(&p.ID, &p.TenantID, &p.Email, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserProfile{}, ErrUserProfileNotFound
		}
		return UserProfile{}, fmt.Errorf("get user profile: %w", err)
	}
	return p, nil
}

const (
	defaultUserProfileLimit = 20
	maxUserProfileLimit     = 100
)

func (r *postgresUserProfileRepository) List(ctx context.Context, tenantID string, filter UserProfileFilter) ([]UserProfile, int, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultUserProfileLimit
	}
	if limit > maxUserProfileLimit {
		limit = maxUserProfileLimit
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	var (
		rows pgx.Rows
		err  error
	)
	switch {
	case len(filter.IDs) > 0:
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, email, display_name, created_at, updated_at,
			       COUNT(*) OVER() AS total_count
			FROM   user_profiles
			WHERE  tenant_id = $1
			  AND  id = ANY($2)
			ORDER BY display_name ASC, id ASC
			LIMIT $3 OFFSET $4
		`, tenantID, filter.IDs, limit, offset)
	case filter.Search != "":
		pattern := "%" + filter.Search + "%"
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, email, display_name, created_at, updated_at,
			       COUNT(*) OVER() AS total_count
			FROM   user_profiles
			WHERE  tenant_id = $1
			  AND  (email ILIKE $2 OR display_name ILIKE $2)
			ORDER BY display_name ASC, id ASC
			LIMIT $3 OFFSET $4
		`, tenantID, pattern, limit, offset)
	default:
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, email, display_name, created_at, updated_at,
			       COUNT(*) OVER() AS total_count
			FROM   user_profiles
			WHERE  tenant_id = $1
			ORDER BY display_name ASC, id ASC
			LIMIT $2 OFFSET $3
		`, tenantID, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list user profiles: %w", err)
	}
	defer rows.Close()

	var profiles []UserProfile
	var total int
	for rows.Next() {
		var p UserProfile
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Email, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("scan user profile: %w", err)
		}
		profiles = append(profiles, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate user profiles: %w", err)
	}
	return profiles, total, nil
}

// noopUserProfileRepository silently discards all writes and returns not-found
// for all reads. Used when no profile repository is wired (e.g. tests that
// don't exercise the profile path).
type noopUserProfileRepository struct{}

func (noopUserProfileRepository) Upsert(_ context.Context, _ UserProfile) error { return nil }
func (noopUserProfileRepository) GetByID(_ context.Context, _, _ string) (UserProfile, error) {
	return UserProfile{}, ErrUserProfileNotFound
}
func (noopUserProfileRepository) List(_ context.Context, _ string, _ UserProfileFilter) ([]UserProfile, int, error) {
	return nil, 0, nil
}

// Compile-time interface checks.
var _ UserProfileRepository = (*postgresUserProfileRepository)(nil)
var _ UserProfileRepository = noopUserProfileRepository{}

// userProfileFromPrincipal builds a UserProfile from a verified OIDC principal.
// Only called for HUMAN / FEDERATED principals — not PAT, agent, or SYSTEM.
func userProfileFromPrincipal(p *Principal) UserProfile {
	now := time.Now().UTC()
	return UserProfile{
		ID:          p.ID,
		TenantID:    p.TenantID,
		Email:       p.Claims["email"],
		DisplayName: p.DisplayName,
		UpdatedAt:   now,
	}
}
