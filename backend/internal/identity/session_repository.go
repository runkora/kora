package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/runkora/kora/backend/internal/platform/storage"
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

	// SwitchSession commits a tenant switch in one transaction: it consumes the
	// replaced refresh token, revokes its session and that session's other
	// refresh tokens, and inserts the next session and refresh token. A replaced
	// token already consumed is ErrRefreshTokenNotFound, a session already
	// revoked is ErrSessionNotFound, and either writes nothing.
	SwitchSession(ctx context.Context, next *Session, nextRefresh *RefreshToken, replaced *RefreshToken, at time.Time) error
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
	return insertSession(ctx, r.pool, s)
}

func (r *postgresSessionRepository) SwitchSession(ctx context.Context, next *Session, nextRefresh *RefreshToken, replaced *RefreshToken, at time.Time) error {
	return storage.WithTransaction(ctx, r.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := revokeRefreshToken(ctx, tx, replaced.ID, at); err != nil {
			return err
		}
		if err := revokeSession(ctx, tx, replaced.SessionID, at); err != nil {
			return err
		}
		if err := revokeAllForSession(ctx, tx, replaced.SessionID, at); err != nil {
			return err
		}
		if err := insertSession(ctx, tx, next); err != nil {
			return err
		}
		return insertRefreshToken(ctx, tx, nextRefresh)
	})
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
	return revokeSession(ctx, r.pool, id, revokedAt)
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

// ─── Statements shared with the switch transaction ───────────────────────────

// execer runs one statement on a pool or inside a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertSession(ctx context.Context, db execer, s *Session) error {
	claimsJSON, err := json.Marshal(s.Claims)
	if err != nil {
		return fmt.Errorf("marshal claims: %w", err)
	}

	_, err = db.Exec(ctx, `
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

func revokeSession(ctx context.Context, db execer, id string, revokedAt time.Time) error {
	tag, err := db.Exec(ctx, `
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
