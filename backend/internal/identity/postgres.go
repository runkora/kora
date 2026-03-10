package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Session repository ───────────────────────────────────────────────────────

type postgresSessionRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresSessionRepository creates a SessionRepository backed by PostgreSQL.
func NewPostgresSessionRepository(pool *pgxpool.Pool) SessionRepository {
	return &postgresSessionRepository{pool: pool}
}

func (r *postgresSessionRepository) CreateSession(ctx context.Context, s *Session) error {
	claimsJSON, err := json.Marshal(s.Claims)
	if err != nil {
		return fmt.Errorf("marshal claims: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO identity_sessions
			(id, jti, principal_id, principal_type, tenant_id, display_name,
			 identity_source, claims, created_at, expires_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		s.ID,
		s.JTI,
		s.PrincipalID,
		string(s.PrincipalType),
		s.TenantID,
		s.DisplayName,
		string(s.IdentitySource),
		claimsJSON,
		s.CreatedAt,
		s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *postgresSessionRepository) GetSessionByID(ctx context.Context, id string) (*Session, error) {
	return r.scanSession(ctx, `
		SELECT id, jti, principal_id, principal_type, tenant_id, display_name,
		       identity_source, claims, created_at, expires_at, revoked_at
		FROM   identity_sessions
		WHERE  id = $1
	`, id)
}

func (r *postgresSessionRepository) GetSessionByJTI(ctx context.Context, jti string) (*Session, error) {
	return r.scanSession(ctx, `
		SELECT id, jti, principal_id, principal_type, tenant_id, display_name,
		       identity_source, claims, created_at, expires_at, revoked_at
		FROM   identity_sessions
		WHERE  jti = $1
	`, jti)
}

func (r *postgresSessionRepository) RevokeSession(ctx context.Context, id string, revokedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE identity_sessions
		SET    revoked_at = $1
		WHERE  id = $2
		  AND  revoked_at IS NULL
	`, revokedAt, id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// scanSession executes query with a single argument and hydrates a Session.
func (r *postgresSessionRepository) scanSession(ctx context.Context, query, arg string) (*Session, error) {
	row := r.pool.QueryRow(ctx, query, arg)

	var s Session
	var principalType, identitySource string
	var claimsRaw []byte

	err := row.Scan(
		&s.ID,
		&s.JTI,
		&s.PrincipalID,
		&principalType,
		&s.TenantID,
		&s.DisplayName,
		&identitySource,
		&claimsRaw,
		&s.CreatedAt,
		&s.ExpiresAt,
		&s.RevokedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}

	s.PrincipalType = PrincipalType(principalType)
	s.IdentitySource = IdentitySource(identitySource)

	if err := json.Unmarshal(claimsRaw, &s.Claims); err != nil {
		return nil, fmt.Errorf("unmarshal session claims: %w", err)
	}

	return &s, nil
}

// ─── Refresh token repository ─────────────────────────────────────────────────

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
