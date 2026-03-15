package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Refresh token repository interface ──────────────────────────────────────

// RefreshTokenRepository defines the PostgreSQL-backed refresh token storage
// contract. Only the SHA-256 hash of each token is persisted — the raw value
// is returned to the client exactly once and never stored.
type RefreshTokenRepository interface {
	// CreateRefreshToken persists a hashed refresh token record.
	CreateRefreshToken(ctx context.Context, rt *RefreshToken) error

	// GetRefreshTokenByHash looks up a refresh token by its SHA-256 hash.
	GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error)

	// RevokeRefreshToken marks a specific refresh token as revoked at revokedAt.
	RevokeRefreshToken(ctx context.Context, id string, revokedAt time.Time) error

	// RevokeAllForSession revokes every active refresh token belonging to a session.
	RevokeAllForSession(ctx context.Context, sessionID string, revokedAt time.Time) error
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

type postgresRefreshTokenRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRefreshTokenRepository creates a RefreshTokenRepository backed by PostgreSQL.
func NewPostgresRefreshTokenRepository(pool *pgxpool.Pool) RefreshTokenRepository {
	return &postgresRefreshTokenRepository{pool: pool}
}

func (r *postgresRefreshTokenRepository) CreateRefreshToken(ctx context.Context, rt *RefreshToken) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO identity_refresh_tokens
			(id, session_id, token_hash, created_at, expires_at)
		VALUES
			($1, $2, $3, $4, $5)
	`,
		rt.ID,
		rt.SessionID,
		rt.TokenHash,
		rt.CreatedAt,
		rt.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

func (r *postgresRefreshTokenRepository) GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, session_id, token_hash, created_at, expires_at, revoked_at
		FROM   identity_refresh_tokens
		WHERE  token_hash = $1
	`, hash)

	var rt RefreshToken
	err := row.Scan(
		&rt.ID,
		&rt.SessionID,
		&rt.TokenHash,
		&rt.CreatedAt,
		&rt.ExpiresAt,
		&rt.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, fmt.Errorf("scan refresh token: %w", err)
	}
	return &rt, nil
}

func (r *postgresRefreshTokenRepository) RevokeRefreshToken(ctx context.Context, id string, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE identity_refresh_tokens
		SET    revoked_at = $1
		WHERE  id = $2
		  AND  revoked_at IS NULL
	`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRefreshTokenNotFound
	}
	return nil
}

func (r *postgresRefreshTokenRepository) RevokeAllForSession(ctx context.Context, sessionID string, revokedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE identity_refresh_tokens
		SET    revoked_at = $1
		WHERE  session_id = $2
		  AND  revoked_at IS NULL
	`, revokedAt, sessionID)
	if err != nil {
		return fmt.Errorf("revoke refresh tokens for session: %w", err)
	}
	return nil
}
