package identity

import (
	"context"
	"time"
)

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
	// All refresh tokens associated with this session are also invalidated.
	RevokeSession(ctx context.Context, id string, revokedAt time.Time) error
}

// RefreshTokenRepository defines the PostgreSQL-backed refresh token storage contract.
type RefreshTokenRepository interface {
	// CreateRefreshToken persists a hashed refresh token record.
	CreateRefreshToken(ctx context.Context, rt *RefreshToken) error

	// GetRefreshTokenByHash looks up a refresh token by its SHA-256 hash.
	GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error)

	// RevokeRefreshToken marks a specific refresh token as revoked.
	RevokeRefreshToken(ctx context.Context, id string, revokedAt time.Time) error

	// RevokeAllForSession revokes every refresh token belonging to a session.
	RevokeAllForSession(ctx context.Context, sessionID string, revokedAt time.Time) error
}

// JTIBlocklistRepository defines the Redis-backed JTI blocklist contract.
// Tokens are added to the blocklist with a TTL equal to their remaining
// lifetime so the blocklist does not grow without bound.
type JTIBlocklistRepository interface {
	// BlockJTI inserts a JTI into the blocklist with the given TTL.
	BlockJTI(ctx context.Context, jti string, ttl time.Duration) error

	// IsBlocked reports whether a JTI is present in the blocklist.
	IsBlocked(ctx context.Context, jti string) (bool, error)
}
