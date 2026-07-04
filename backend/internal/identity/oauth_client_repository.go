package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const oauthClientKeyPrefix = "orbit:oauth:client:"

// registeredClientTTL bounds how long a dynamically-registered client record
// lives without re-registration. Generous, but finite so abandoned registrations
// (DCR abuse) self-expire rather than accumulate forever.
const registeredClientTTL = 30 * 24 * time.Hour

// ErrClientNotFound is returned when a client_id is unknown (unregistered or expired).
var ErrClientNotFound = errors.New("client not registered")

// RegisteredClient is a dynamically-registered public OAuth client (RFC 7591) —
// a desktop MCP application. Public clients hold no secret; PKCE is the proof.
type RegisteredClient struct {
	ClientID     string    `json:"client_id"`
	RedirectURIs []string  `json:"redirect_uris"`
	ClientName   string    `json:"client_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// AllowsRedirect reports whether uri is one of the client's registered redirect URIs.
func (c RegisteredClient) AllowsRedirect(uri string) bool {
	return slices.Contains(c.RedirectURIs, uri)
}

// ClientRepository stores dynamically-registered OAuth clients.
type ClientRepository interface {
	Store(ctx context.Context, client RegisteredClient, ttl time.Duration) error
	Get(ctx context.Context, clientID string) (RegisteredClient, error)
}

// redisClientRepository implements ClientRepository using Redis, keyed under
// orbit:oauth:client:{client_id}.
type redisClientRepository struct {
	client *goredis.Client
}

// NewRedisClientRepository creates a ClientRepository backed by Redis.
func NewRedisClientRepository(client *goredis.Client) ClientRepository {
	return &redisClientRepository{client: client}
}

func (r *redisClientRepository) Store(ctx context.Context, c RegisteredClient, ttl time.Duration) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal client: %w", err)
	}
	if err := r.client.Set(ctx, oauthClientKeyPrefix+c.ClientID, raw, ttl).Err(); err != nil {
		return fmt.Errorf("store client: %w", err)
	}
	return nil
}

func (r *redisClientRepository) Get(ctx context.Context, clientID string) (RegisteredClient, error) {
	raw, err := r.client.Get(ctx, oauthClientKeyPrefix+clientID).Bytes()
	if errors.Is(err, goredis.Nil) {
		return RegisteredClient{}, ErrClientNotFound
	}
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("get client: %w", err)
	}
	var c RegisteredClient
	if err := json.Unmarshal(raw, &c); err != nil {
		return RegisteredClient{}, fmt.Errorf("unmarshal client: %w", err)
	}
	return c, nil
}
