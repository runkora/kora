package storage

import (
	"fmt"
	"os"
	"strconv"
)

// RedisConfig holds Redis connection and pool configuration.
// Non-secret tunables (port, pool sizes, timeouts) are loaded from YAML under the "redis" key.
// Host and Password are always loaded from environment variables.
type RedisConfig struct {
	Port                   int  `yaml:"port"`
	DB                     int  `yaml:"db"`
	MaxRetries             int  `yaml:"maxRetries"`
	DialTimeoutSeconds     int  `yaml:"dialTimeoutSeconds"`
	ReadTimeoutSeconds     int  `yaml:"readTimeoutSeconds"`
	WriteTimeoutSeconds    int  `yaml:"writeTimeoutSeconds"`
	PoolSize               int  `yaml:"poolSize"`
	MinIdleConns           int  `yaml:"minIdleConns"`
	ConnMaxIdleTimeMinutes int  `yaml:"connMaxIdleTimeMinutes"`
	TLSEnabled             bool `yaml:"tlsEnabled"`

	// Loaded from environment variables — never from YAML.
	Host     string `yaml:"-"`
	Password string `yaml:"-"`
}

// loadRedisConfig overlays environment-variable credentials onto a RedisConfig
// that has already been populated from YAML. It is called by LoadStorageConfig.
func loadRedisConfig(cfg *RedisConfig) error {
	cfg.Host = os.Getenv("REDIS_HOST")
	if cfg.Host == "" {
		return fmt.Errorf("REDIS_HOST environment variable is required")
	}

	cfg.Password = os.Getenv("REDIS_PASSWORD")
	if cfg.Password == "" {
		return fmt.Errorf("REDIS_PASSWORD environment variable is required")
	}

	if port := os.Getenv("REDIS_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	return nil
}
