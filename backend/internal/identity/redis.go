package identity

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const jtiKeyPrefix = "orbit:jti:blocklist:"

// redisJTIBlocklistRepository implements JTIBlocklistRepository using Redis.
// Keys are namespaced under orbit:jti:blocklist:{jti} and carry a TTL equal
// to the token's remaining lifetime, so the set never grows without bound.
type redisJTIBlocklistRepository struct {
	client *goredis.Client
}

// NewRedisJTIBlocklistRepository creates a JTIBlocklistRepository backed by Redis.
func NewRedisJTIBlocklistRepository(client *goredis.Client) JTIBlocklistRepository {
	return &redisJTIBlocklistRepository{client: client}
}

// BlockJTI inserts a JTI into the blocklist with the given TTL.
// A zero TTL stores the key without expiry — use the token's remaining
// lifetime as the TTL so revoked entries are self-expiring.
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
