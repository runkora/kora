package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// DB wraps pgxpool.Pool and provides database infrastructure
type DB struct {
	pool   *pgxpool.Pool
	logger logging.Logger
}

// New creates and initializes a database connection pool
// Performs single-phase initialization with connectivity verification
func New(ctx context.Context, config *PostgresConfig, logger logging.Logger) (*DB, error) {
	logger.Info(ctx, "Initializing database connection pool",
		logging.Field{Key: "host", Value: config.Host},
		logging.Field{Key: "database", Value: config.Database},
		logging.Field{Key: "port", Value: config.Port},
	)

	poolConfig, err := config.BuildPoolConfig()
	if err != nil {
		return nil, fmt.Errorf("build pool config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	// Verify connectivity on initialization
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	logger.Info(ctx, "Database connection pool ready",
		logging.Field{Key: "max_conns", Value: poolConfig.MaxConns},
		logging.Field{Key: "min_conns", Value: poolConfig.MinConns},
	)

	return &DB{
		pool:   pool,
		logger: logger,
	}, nil
}

// Pool returns the underlying pgxpool.Pool for feature repositories
// Repositories use this directly - no need to manually acquire/release connections
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Close gracefully shuts down the connection pool
func (db *DB) Close() {
	db.logger.Info(context.Background(), "Closing database connection pool")
	db.pool.Close()
}

// Stats returns connection pool statistics for monitoring and observability
func (db *DB) Stats() *pgxpool.Stat {
	return db.pool.Stat()
}

// Health checks database connectivity with a timeout
// Used by health check endpoints and readiness probes
func (db *DB) Health(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := db.pool.Ping(healthCtx); err != nil {
		return fmt.Errorf("database unhealthy: %w", err)
	}

	// Check if pool is under pressure
	stats := db.pool.Stat()
	if stats.ConstructingConns() > 0 && stats.IdleConns() == 0 {
		db.logger.Warn(ctx, "Database connection pool under pressure",
			logging.Field{Key: "total_conns", Value: stats.TotalConns()},
			logging.Field{Key: "constructing_conns", Value: stats.ConstructingConns()},
			logging.Field{Key: "idle_conns", Value: stats.IdleConns()},
		)
	}

	return nil
}
