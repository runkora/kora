package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// ErrRedisUnavailable is returned without a network call while the last probe
// found Redis unreachable.
var ErrRedisUnavailable = errors.New("redis unavailable")

// redisProbeInterval is how often the client re-checks Redis reachability.
var redisProbeInterval = 10 * time.Second

// RedisClient wraps redis.Client and provides cache/blocklist infrastructure.
// It never fails on an unreachable Redis: a background probe tracks
// reachability, so services start and recover when Redis comes back.
type RedisClient struct {
	client    *redis.Client
	logger    logging.Logger
	addr      string
	available atomic.Bool
	probed    atomic.Bool
	done      chan struct{}
	closeOnce sync.Once
}

// NewRedis creates a Redis client and starts its reachability probe. It returns
// an error only for invalid configuration, never because Redis is unreachable.
func NewRedis(ctx context.Context, cfg *RedisConfig, logger logging.Logger) (*RedisClient, error) {
	logger.Info(ctx, "Initializing Redis client",
		logging.Field{Key: "host", Value: cfg.Host},
		logging.Field{Key: "port", Value: cfg.Port},
		logging.Field{Key: "db", Value: cfg.DB},
	)

	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		return nil, fmt.Errorf("tls: %w", err)
	}

	opts := &redis.Options{
		TLSConfig:       tlsCfg,
		Addr:            fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
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

	r := &RedisClient{client: redis.NewClient(opts), logger: logger, addr: opts.Addr, done: make(chan struct{})}
	r.probe(ctx)
	// The probe lives until Close, whatever the caller's ctx does.
	go r.watch(context.WithoutCancel(ctx), redisProbeInterval)
	r.registerAvailabilityGauge(ctx)
	return r, nil
}

// probe pings Redis and records reachability, logging each change.
func (r *RedisClient) probe(ctx context.Context) {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := r.client.Ping(pingCtx).Err()
	if r.closed() {
		return
	}
	up := err == nil
	prev := r.available.Swap(up)
	if r.probed.Swap(true) && prev == up {
		return
	}
	if up {
		r.logger.Info(ctx, "Redis available", logging.Field{Key: "addr", Value: r.addr}, logging.Field{Key: "redis_available", Value: true})
		return
	}
	r.logger.Error(ctx, "Redis unavailable, retrying in the background",
		logging.Field{Key: "addr", Value: r.addr},
		logging.Field{Key: "redis_available", Value: false},
		logging.Field{Key: "error", Value: err.Error()})
}

// watch re-probes Redis until the client closes.
func (r *RedisClient) watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-t.C:
			r.probe(ctx)
		}
	}
}

// registerAvailabilityGauge publishes kora.redis.available (1 up, 0 down).
func (r *RedisClient) registerAvailabilityGauge(ctx context.Context) {
	_, err := otel.Meter("kora.storage").Int64ObservableGauge("kora.redis.available",
		metric.WithDescription("1 while Redis answers the reachability probe, 0 otherwise"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			if r.Available() {
				o.Observe(1)
			} else {
				o.Observe(0)
			}
			return nil
		}))
	if err != nil {
		r.logger.Error(ctx, "register redis availability gauge", logging.Field{Key: "error", Value: err.Error()})
	}
}

// closed reports whether Close has run.
func (r *RedisClient) closed() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Available reports whether the last probe reached Redis.
func (r *RedisClient) Available() bool { return r.available.Load() }

// Client returns the underlying redis.Client for feature repositories.
func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// Close gracefully shuts down the Redis client.
func (r *RedisClient) Close() error {
	r.logger.Info(context.Background(), "Closing Redis client")
	r.closeOnce.Do(func() { close(r.done) })
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
