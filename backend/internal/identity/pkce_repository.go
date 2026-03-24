package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const pkceKeyPrefix = "orbit:pkce:state:"

// ─── PKCE state repository interface ─────────────────────────────────────────

// PKCERepository stores and retrieves PKCE code-verifier state keyed by the
// opaque `state` value sent to the OIDC provider's authorization endpoint.
// Each entry is single-use: GetVerifier atomically retrieves and deletes it.
type PKCERepository interface {
	// StoreVerifier persists the PKCE code verifier associated with state for
	// the given TTL. The state value is the opaque random string included in
	// the OIDC authorization request; the verifier is the PKCE code_verifier.
	StoreVerifier(ctx context.Context, state, verifier string, ttl time.Duration) error

	// GetVerifier retrieves and atomically deletes the PKCE verifier for the
	// given state. Returns ErrPKCEStateNotFound if the state is missing or expired.
	GetVerifier(ctx context.Context, state string) (string, error)
}

// ─── Redis implementation ─────────────────────────────────────────────────────

// redisPKCERepository implements PKCERepository using Redis.
// Keys are namespaced under orbit:pkce:state:{state}.
// GetDel provides atomic single-use semantics: once retrieved the key is gone.
type redisPKCERepository struct {
	client *goredis.Client
}

// NewRedisPKCERepository creates a PKCERepository backed by Redis.
func NewRedisPKCERepository(client *goredis.Client) PKCERepository {
	return &redisPKCERepository{client: client}
}

// StoreVerifier persists the PKCE code verifier keyed by state with the given TTL.
func (r *redisPKCERepository) StoreVerifier(ctx context.Context, state, verifier string, ttl time.Duration) error {
	key := pkceKeyPrefix + state
	if err := r.client.Set(ctx, key, verifier, ttl).Err(); err != nil {
		return fmt.Errorf("store pkce verifier for state %q: %w", state, err)
	}
	return nil
}

// GetVerifier atomically retrieves and deletes the PKCE verifier for the given state.
// Returns ErrPKCEStateNotFound if the state is not present or has expired.
func (r *redisPKCERepository) GetVerifier(ctx context.Context, state string) (string, error) {
	key := pkceKeyPrefix + state
	verifier, err := r.client.GetDel(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return "", ErrPKCEStateNotFound
		}
		return "", fmt.Errorf("get pkce verifier for state %q: %w", state, err)
	}
	return verifier, nil
}
