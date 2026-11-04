package identity

import (
	"context"
	"time"

	"github.com/runkora/kora/backend/internal/platform/storage"
)

// ─── JTI blocklist repository interface ──────────────────────────────────────

// JTIBlocklistRepository defines the Redis-backed JTI blocklist contract.
// Tokens are added to the blocklist with a TTL equal to their remaining
// lifetime so the set never grows without bound.
type JTIBlocklistRepository interface {
	// BlockJTI inserts a JTI into the blocklist with the given TTL.
	// Use the token's remaining lifetime as ttl so revoked entries self-expire.
	BlockJTI(ctx context.Context, jti string, ttl time.Duration) error

	// IsBlocked reports whether a JTI is present in the blocklist.
	IsBlocked(ctx context.Context, jti string) (bool, error)
}

// ─── Redis implementation ─────────────────────────────────────────────────────

// redisJTIBlocklistRepository implements JTIBlocklistRepository via the shared
// platform blocklist (internal/platform/storage.JTIBlocklist), so the identity
// service writes to the exact namespace other services read revocation from
// (e.g. DelegationMiddlewareWithBlocklist's revoked-delegation check).
type redisJTIBlocklistRepository struct {
	blocklist *storage.JTIBlocklist
}

// NewRedisJTIBlocklistRepository creates a JTIBlocklistRepository backed by Redis.
func NewRedisJTIBlocklistRepository(redis *storage.RedisClient) JTIBlocklistRepository {
	return &redisJTIBlocklistRepository{blocklist: storage.NewJTIBlocklist(redis)}
}

// BlockJTI inserts a JTI into the blocklist with the given TTL.
func (r *redisJTIBlocklistRepository) BlockJTI(ctx context.Context, jti string, ttl time.Duration) error {
	return r.blocklist.BlockJTI(ctx, jti, ttl)
}

// IsBlocked reports whether a JTI is present in the blocklist.
func (r *redisJTIBlocklistRepository) IsBlocked(ctx context.Context, jti string) (bool, error) {
	return r.blocklist.IsBlocked(ctx, jti)
}
