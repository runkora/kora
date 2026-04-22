package storage

import (
	"fmt"
	"os"
	"strconv"
)

// RedisConfig holds Redis connection and pool configuration.
// Non-secret tunables (port, pool sizes, timeouts) are loaded from YAML under the "redis" key.
// Host, Username, and Password are always loaded from environment variables.
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
	Username string `yaml:"-"`
	Password string `yaml:"-"`
}

// loadRedisConfig overlays environment-variable credentials and optional
// per-field overrides onto a RedisConfig that has already been populated
// from YAML. It is called by LoadStorageConfig.
//
// Explicit env var overrides let local dev change individual settings (e.g.
// ORBIT_REDIS_TLS_ENABLED=false) without needing APP_ENV-based YAML branching.
func loadRedisConfig(cfg *RedisConfig) error {
	cfg.Host = os.Getenv("REDIS_HOST")
	if cfg.Host == "" {
		return fmt.Errorf("REDIS_HOST environment variable is required")
	}

	cfg.Username = os.Getenv("REDIS_USERNAME")
	if cfg.Username == "" {
		return fmt.Errorf("REDIS_USERNAME environment variable is required")
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

	// Explicit override — takes precedence over YAML when set.
	// Used by local dev to disable TLS without an APP_ENV-specific YAML file.
	if v := os.Getenv("ORBIT_REDIS_TLS_ENABLED"); v != "" {
		cfg.TLSEnabled = v == "true" || v == "1"
	}

	return nil
}
