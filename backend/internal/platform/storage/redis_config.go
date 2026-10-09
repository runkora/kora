package storage

import (
	"crypto/tls"
	"crypto/x509"
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
	// CAFile is a PEM bundle that signs the Redis certificate. Empty uses the system roots.
	CAFile string `yaml:"caFile"`
	// ServerName overrides the name verified against the Redis certificate.
	ServerName string `yaml:"serverName"`

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

// tlsConfig returns the client TLS settings, or nil when TLS is disabled.
func (c *RedisConfig) tlsConfig() (*tls.Config, error) {
	if !c.TLSEnabled {
		return nil, nil
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.ServerName}
	if tc.ServerName == "" {
		tc.ServerName = c.Host
	}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read redis.caFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("redis.caFile %q contains no PEM certificates", c.CAFile)
		}
		tc.RootCAs = pool
	}
	return tc, nil
}
