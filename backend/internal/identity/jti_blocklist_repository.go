package identity

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const jtiKeyPrefix = "orbit:jti:blocklist:"

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

// redisJTIBlocklistRepository implements JTIBlocklistRepository using Redis.
// Keys are namespaced under orbit:jti:blocklist:{jti}.
type redisJTIBlocklistRepository struct {
	client *goredis.Client
}

// NewRedisJTIBlocklistRepository creates a JTIBlocklistRepository backed by Redis.
func NewRedisJTIBlocklistRepository(client *goredis.Client) JTIBlocklistRepository {
	return &redisJTIBlocklistRepository{client: client}
}

// BlockJTI inserts a JTI into the blocklist with the given TTL.
func (r *redisJTIBlocklistRepository) BlockJTI(ctx context.Context, jti string, ttl time.Duration) error {
	key := jtiKeyPrefix + jti
	if err := r.client.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("block jti %q: %w", jti, err)
	}
	return nil
}

// IsBlocked reports whether a JTI is present in the blocklist.
func (r *redisJTIBlocklistRepository) IsBlocked(ctx context.Context, jti string) (bool, error) {
	key := jtiKeyPrefix + jti
	n, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("check jti %q: %w", jti, err)
	}
	return n > 0, nil
}
