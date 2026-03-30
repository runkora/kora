package storage

import (
	"fmt"
	"os"
	"strconv"
)

// PostgresConfig holds database connection and pool configuration.
// Non-secret tunables (port, pool sizes, timeouts) are loaded from YAML.
// Connection credentials (host, database, user, password) are loaded from environment variables.
type PostgresConfig struct {
	Port                     int    `yaml:"port"`
	SSLMode                  string `yaml:"sslMode"`
	MaxConnections           int32  `yaml:"maxConnections"`
	MinConnections           int32  `yaml:"minConnections"`
	MaxConnLifetimeMinutes   int    `yaml:"maxConnLifetimeMinutes"`
	MaxConnIdleTimeMinutes   int    `yaml:"maxConnIdleTimeMinutes"`
	HealthCheckPeriodSeconds int    `yaml:"healthCheckPeriodSeconds"`
	ConnectTimeoutSeconds    int    `yaml:"connectTimeoutSeconds"`
	CaptureQueryText         bool   `yaml:"captureQueryText"`

	// Loaded from environment variables — never from YAML.
	Host     string `yaml:"-"`
	Database string `yaml:"-"`
	User     string `yaml:"-"`
	Password string `yaml:"-"`
}

// ConnectionString builds a libpq-style connection string from the config.
func (c *PostgresConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.Port, c.Database, c.User, c.Password, c.SSLMode,
	)
}

// loadPostgresConfig overlays environment-variable credentials and optional
// per-field overrides onto a PostgresConfig that has already been populated
// from YAML. It is called by LoadStorageConfig.
//
// Explicit env var overrides let local dev change individual settings (e.g.
// ORBIT_POSTGRES_SSL_MODE=disable) without needing APP_ENV-based YAML branching.
func loadPostgresConfig(cfg *PostgresConfig) error {
	cfg.Host = os.Getenv("DB_HOST")
	if cfg.Host == "" {
		return fmt.Errorf("DB_HOST environment variable is required")
	}

	cfg.Database = os.Getenv("DB_NAME")
	if cfg.Database == "" {
		return fmt.Errorf("DB_NAME environment variable is required")
	}

	cfg.User = os.Getenv("DB_USER")
	if cfg.User == "" {
		return fmt.Errorf("DB_USER environment variable is required")
	}

	cfg.Password = os.Getenv("DB_PASSWORD")
	if cfg.Password == "" {
		return fmt.Errorf("DB_PASSWORD environment variable is required")
	}

	if port := os.Getenv("DB_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	// Explicit overrides — take precedence over YAML when set.
	// Used by local dev to disable SSL without an APP_ENV-specific YAML file.
	if v := os.Getenv("ORBIT_POSTGRES_SSL_MODE"); v != "" {
		cfg.SSLMode = v
	}
	if v := os.Getenv("ORBIT_POSTGRES_CAPTURE_QUERY_TEXT"); v != "" {
		cfg.CaptureQueryText = v == "true" || v == "1"
	}

	return nil
}
