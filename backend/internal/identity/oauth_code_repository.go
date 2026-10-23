package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const oauthCodeKeyPrefix = "kora:oauth:code:"

// ErrAuthCodeNotFound is returned when an authorization code is absent, expired,
// or already consumed. Codes are single-use, so a second exchange returns this.
var ErrAuthCodeNotFound = errors.New("authorization code not found or already used")

// AuthCode is a short-lived, single-use OAuth 2.1 authorization code, bound to
// the human it was issued for and to the client's PKCE challenge and redirect
// URI. The BFF /authorize creates it after a session check; identity /token
// consumes it exactly once to mint the human's platform token.
type AuthCode struct {
	// Subject is the human's identity (the platform token's sub — an OIDC subject).
	Subject     string `json:"sub"`
	TenantID    string `json:"tenant_id"`
	DisplayName string `json:"display_name"`
	// Claims are the human's verified source claims (roles, etc.) captured from
	// their session. They must ride through to the minted MCP token so it carries
	// the same permissions — the consent promise is "acts as you". Without this,
	// role-derived grants (e.g. platform-admin → mcp:kora:use) would be lost.
	Claims map[string]string `json:"claims,omitempty"`
	// ClientID is the MCP client (from DCR later; free-form for now).
	ClientID string `json:"client_id"`
	// RedirectURI must match at the token exchange (prevents code injection).
	RedirectURI string `json:"redirect_uri"`
	// CodeChallenge is the PKCE S256 challenge; the verifier is checked at exchange.
	CodeChallenge string `json:"code_challenge"`
	Scope         string `json:"scope,omitempty"`
}

// AuthCodeRepository stores single-use authorization codes with a short TTL.
type AuthCodeRepository interface {
	// Store persists an authorization code with the given TTL.
	Store(ctx context.Context, code string, data AuthCode, ttl time.Duration) error
	// Consume atomically fetches and deletes a code — single-use. Returns
	// ErrAuthCodeNotFound if the code is absent, expired, or already consumed.
	Consume(ctx context.Context, code string) (AuthCode, error)
}

// redisAuthCodeRepository implements AuthCodeRepository using Redis, keyed under
// kora:oauth:code:{code}. Consume uses GETDEL so a code can never be redeemed
// twice even under concurrent exchange attempts.
type redisAuthCodeRepository struct {
	client *goredis.Client
}

// NewRedisAuthCodeRepository creates an AuthCodeRepository backed by Redis.
func NewRedisAuthCodeRepository(client *goredis.Client) AuthCodeRepository {
	return &redisAuthCodeRepository{client: client}
}

func (r *redisAuthCodeRepository) Store(ctx context.Context, code string, data AuthCode, ttl time.Duration) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal auth code: %w", err)
	}
	if err := r.client.Set(ctx, oauthCodeKeyPrefix+code, raw, ttl).Err(); err != nil {
		return fmt.Errorf("store auth code: %w", err)
	}
	return nil
}

func (r *redisAuthCodeRepository) Consume(ctx context.Context, code string) (AuthCode, error) {
	raw, err := r.client.GetDel(ctx, oauthCodeKeyPrefix+code).Bytes()
	if errors.Is(err, goredis.Nil) {
		return AuthCode{}, ErrAuthCodeNotFound
	}
	if err != nil {
		return AuthCode{}, fmt.Errorf("consume auth code: %w", err)
	}
	var data AuthCode
	if err := json.Unmarshal(raw, &data); err != nil {
		return AuthCode{}, fmt.Errorf("unmarshal auth code: %w", err)
	}
	return data, nil
}
