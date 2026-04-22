package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/redis/go-redis/v9"
)

// RedisClient wraps redis.Client and provides cache/blocklist infrastructure.
type RedisClient struct {
	client *redis.Client
	logger logging.Logger
}

// NewRedis creates and initializes a Redis client with connectivity verification.
func NewRedis(ctx context.Context, cfg *RedisConfig, logger logging.Logger) (*RedisClient, error) {
	logger.Info(ctx, "Initializing Redis client",
		logging.Field{Key: "host", Value: cfg.Host},
		logging.Field{Key: "port", Value: cfg.Port},
		logging.Field{Key: "db", Value: cfg.DB},
	)

	opts := &redis.Options{
		Addr:            fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Username:        cfg.Username,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      cfg.MaxRetries,
		DialTimeout:     time.Duration(cfg.DialTimeoutSeconds) * time.Second,
		ReadTimeout:     time.Duration(cfg.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:    time.Duration(cfg.WriteTimeoutSeconds) * time.Second,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		ConnMaxIdleTime: time.Duration(cfg.ConnMaxIdleTimeMinutes) * time.Minute,
	}

	client := redis.NewClient(opts)

	// Verify connectivity on initialisation.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info(ctx, "Redis client ready",
		logging.Field{Key: "pool_size", Value: cfg.PoolSize},
		logging.Field{Key: "min_idle_conns", Value: cfg.MinIdleConns},
	)

	return &RedisClient{
		client: client,
		logger: logger,
	}, nil
}

// Client returns the underlying redis.Client for feature repositories.
func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// Close gracefully shuts down the Redis client.
func (r *RedisClient) Close() error {
	r.logger.Info(context.Background(), "Closing Redis client")
	return r.client.Close()
}

// Health checks Redis connectivity with a timeout.
func (r *RedisClient) Health(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := r.client.Ping(healthCtx).Err(); err != nil {
		return fmt.Errorf("redis unhealthy: %w", err)
	}

	return nil
}

// Set stores a key-value pair with an expiry. A zero TTL means the key does not expire.
func (r *RedisClient) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %q: %w", key, err)
	}
	return nil
}

// Exists reports whether a key is present in Redis.
func (r *RedisClient) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("redis exists %q: %w", key, err)
	}
	return n > 0, nil
}

// Delete removes one or more keys from Redis.
func (r *RedisClient) Delete(ctx context.Context, keys ...string) error {
	if err := r.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}
