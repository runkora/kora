package storage

import (
	"errors"
	"fmt"
	"os"
)

// RedisConfig holds Redis connection and pool configuration, loaded from YAML
// under the "redis" key. The password comes from REDIS_PASSWORD.
type RedisConfig struct {
	Host                   string `yaml:"host"`
	Port                   int    `yaml:"port"`
	DB                     int    `yaml:"db"`
	MaxRetries             int    `yaml:"maxRetries"`
	DialTimeoutSeconds     int    `yaml:"dialTimeoutSeconds"`
	ReadTimeoutSeconds     int    `yaml:"readTimeoutSeconds"`
	WriteTimeoutSeconds    int    `yaml:"writeTimeoutSeconds"`
	PoolSize               int    `yaml:"poolSize"`
	MinIdleConns           int    `yaml:"minIdleConns"`
	ConnMaxIdleTimeMinutes int    `yaml:"connMaxIdleTimeMinutes"`
	TLSEnabled             bool   `yaml:"tlsEnabled"`

	// Loaded from REDIS_PASSWORD, never from YAML.
	Password string `yaml:"-"`
}

// loadRedisConfig checks the YAML host and reads the password from the
// environment. It is called by LoadStorageConfig.
func loadRedisConfig(cfg *RedisConfig) error {
	if cfg.Host == "" {
		return errors.New("redis.host must be set")
	}
	cfg.Password = os.Getenv("REDIS_PASSWORD")
	if cfg.Password == "" {
		return fmt.Errorf("REDIS_PASSWORD environment variable is required")
	}
	return nil
}
