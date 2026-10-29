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

// ─── Session repository interface ─────────────────────────────────────────────

// SessionRepository defines the PostgreSQL-backed session storage contract.
// It covers the full lifecycle of a platform session: creation, lookup,
// revocation, and expiry-aware queries.
type SessionRepository interface {
	// CreateSession persists a new session record.
	CreateSession(ctx context.Context, s *Session) error

	// GetSessionByID retrieves a session by its primary ID.
	GetSessionByID(ctx context.Context, id string) (*Session, error)

	// GetSessionByJTI retrieves a session by its JWT ID claim.
	// Used during refresh and revocation to tie a token back to a session.
	GetSessionByJTI(ctx context.Context, jti string) (*Session, error)

	// RevokeSession marks a session as revoked at the given timestamp.
	// Refresh token revocation is a separate operation — callers must invoke
	// RefreshTokenRepository.RevokeAllForSession before or after.
	RevokeSession(ctx context.Context, id string, revokedAt time.Time) error

	// CountActiveSessions returns the number of sessions that are neither
	// revoked nor expired, grouped by tenant. Backs the
	// kora_identity_sessions_active{tenant_id} gauge.
	CountActiveSessions(ctx context.Context) (map[string]int64, error)
}

// ─── PostgreSQL implementation ────────────────────────────────────────────────

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

func (r *postgresSessionRepository) CountActiveSessions(ctx context.Context) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tenant_id, COUNT(*)
		FROM   identity_sessions
		WHERE  revoked_at IS NULL
		  AND  expires_at > now()
		GROUP BY tenant_id
	`)
	if err != nil {
		return nil, fmt.Errorf("count active sessions: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var tenantID string
		var count int64
		if err := rows.Scan(&tenantID, &count); err != nil {
			return nil, fmt.Errorf("scan active session count: %w", err)
		}
		counts[tenantID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count active sessions: %w", err)
	}
	return counts, nil
}

// scanSession executes query with a single string argument and hydrates a Session.
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
