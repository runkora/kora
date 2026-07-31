package storage

import (
	"fmt"

	"github.com/clear-route/orbit/internal/platform/config"
)

// StorageConfig is the single top-level configuration struct for all storage backends.
// It is loaded from internal/platform/storage/config/{env}.yaml using two nested keys:
// "postgres" for PostgreSQL settings and "redis" for Redis settings.
type StorageConfig struct {
	Postgres PostgresConfig `yaml:"postgres"`
	Redis    RedisConfig    `yaml:"redis"`
}

// LoadPostgresConfig loads storage YAML and applies Postgres environment-variable
// overlays. Use this in services that only need a database connection.
func LoadPostgresConfig() (*PostgresConfig, error) {
	cfg, err := config.Load[StorageConfig]("internal/platform/storage", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load storage config: %w", err)
	}

	if err := loadPostgresConfig(&cfg.Postgres); err != nil {
		return nil, err
	}

	return &cfg.Postgres, nil
}

// LoadStorageConfig loads the unified YAML once, then delegates environment-variable
// overlays to each backend's dedicated loader. Use this in services that need
// both Postgres and Redis.
func LoadStorageConfig() (*StorageConfig, error) {
	cfg, err := config.Load[StorageConfig]("internal/platform/storage", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load storage config: %w", err)
	}

	if err := loadPostgresConfig(&cfg.Postgres); err != nil {
		return nil, err
	}

	if err := loadRedisConfig(&cfg.Redis); err != nil {
		return nil, err
	}

	return cfg, nil
}
