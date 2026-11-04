package storage

import (
	"context"
	"fmt"
	"time"
)

// jtiBlocklistKeyPrefix namespaces revoked-token JTIs in Redis. It MUST match
// the prefix the identity service writes under (the identity service is the
// sole writer for platform-token revocation; delegation revocation writes the
// delegation JTI under the same namespace), so any service holding a Redis
// client can check revocation with a shared JTIBlocklist.
const jtiBlocklistKeyPrefix = "kora:jti:blocklist:"

// JTIBlocklist is the Redis-backed revoked-JTI set shared across services.
// Entries carry a TTL equal to the token's maximum remaining lifetime so the
// set never grows without bound.
type JTIBlocklist struct {
	redis *RedisClient
}

// NewJTIBlocklist creates a JTIBlocklist backed by the given Redis client.
func NewJTIBlocklist(redis *RedisClient) *JTIBlocklist {
	return &JTIBlocklist{redis: redis}
}

// BlockJTI inserts a JTI into the blocklist with the given TTL.
// Use the token's remaining lifetime as ttl so revoked entries self-expire.
func (b *JTIBlocklist) BlockJTI(ctx context.Context, jti string, ttl time.Duration) error {
	if err := b.redis.Set(ctx, jtiBlocklistKeyPrefix+jti, "1", ttl); err != nil {
		return fmt.Errorf("block jti %q: %w", jti, err)
	}
	return nil
}

// IsBlocked reports whether a JTI is present in the blocklist. While Redis is
// unreachable it returns ErrRedisUnavailable at once, and callers fail open.
func (b *JTIBlocklist) IsBlocked(ctx context.Context, jti string) (bool, error) {
	if !b.redis.Available() {
		return false, fmt.Errorf("check jti %q: %w", jti, ErrRedisUnavailable)
	}
	blocked, err := b.redis.Exists(ctx, jtiBlocklistKeyPrefix+jti)
	if err != nil {
		return false, fmt.Errorf("check jti %q: %w", jti, err)
	}
	return blocked, nil
}
