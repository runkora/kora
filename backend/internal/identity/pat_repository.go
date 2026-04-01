package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── PAT repository interface ─────────────────────────────────────────────────

// PATRepository defines the PostgreSQL-backed PAT storage contract.
type PATRepository interface {
	// CreatePAT persists a new PAT record. TokenHash is the SHA-256 hex of the
	// raw JWT; it is stored as a security property and is never queried for auth.
	CreatePAT(ctx context.Context, pat *PAT, tokenHash string) error

	// ListByUser returns all active (non-revoked, non-expired) PATs for a user.
	ListByUser(ctx context.Context, tenantID, userID string) ([]PAT, error)

	// GetByID retrieves a PAT by its primary ID, scoped to the owning user.
	// Returns ErrPATNotFound if no matching active record exists.
	GetByID(ctx context.Context, tenantID, userID, id string) (*PAT, error)

	// Revoke marks a PAT as revoked. Returns ErrPATNotFound if the PAT does not
	// exist or belongs to a different user (prevents cross-user information leakage).
	Revoke(ctx context.Context, tenantID, userID, id string, revokedAt time.Time) error
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresPATRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPATRepository creates a PATRepository backed by PostgreSQL.
func NewPostgresPATRepository(pool *pgxpool.Pool) PATRepository {
	return &postgresPATRepository{pool: pool}
}

func (r *postgresPATRepository) CreatePAT(ctx context.Context, pat *PAT, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_pat_tokens
			(id, tenant_id, user_id, name, token_hash, jti, expires_at, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		pat.ID,
		pat.TenantID,
		pat.UserID,
		pat.Name,
		tokenHash,
		pat.JTI,
		pat.ExpiresAt,
		pat.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create PAT: %w", err)
	}
	return nil
}

func (r *postgresPATRepository) ListByUser(ctx context.Context, tenantID, userID string) ([]PAT, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, jti, expires_at, created_at, revoked_at
		FROM   user_pat_tokens
		WHERE  tenant_id  = $1
		  AND  user_id    = $2
		  AND  revoked_at IS NULL
		  AND  expires_at > now()
		ORDER BY created_at DESC
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list PATs: %w", err)
	}
	defer rows.Close()

	var pats []PAT
	for rows.Next() {
		var p PAT
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.UserID, &p.Name,
			&p.JTI, &p.ExpiresAt, &p.CreatedAt, &p.RevokedAt,
		); err != nil {
			return nil, fmt.Errorf("scan PAT row: %w", err)
		}
		pats = append(pats, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PAT rows: %w", err)
	}
	return pats, nil
}

func (r *postgresPATRepository) GetByID(ctx context.Context, tenantID, userID, id string) (*PAT, error) {
	var p PAT
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, name, jti, expires_at, created_at, revoked_at
		FROM   user_pat_tokens
		WHERE  id        = $1
		  AND  tenant_id = $2
		  AND  user_id   = $3
		  AND  revoked_at IS NULL
	`, id, tenantID, userID).Scan(
		&p.ID, &p.TenantID, &p.UserID, &p.Name,
		&p.JTI, &p.ExpiresAt, &p.CreatedAt, &p.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPATNotFound
		}
		return nil, fmt.Errorf("get PAT by ID: %w", err)
	}
	return &p, nil
}

func (r *postgresPATRepository) Revoke(ctx context.Context, tenantID, userID, id string, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_pat_tokens
		SET    revoked_at = $1
		WHERE  id        = $2
		  AND  tenant_id = $3
		  AND  user_id   = $4
		  AND  revoked_at IS NULL
	`, revokedAt, id, tenantID, userID)
	if err != nil {
		return fmt.Errorf("revoke PAT: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPATNotFound
	}
	return nil
}
