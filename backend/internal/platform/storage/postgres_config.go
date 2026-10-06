package storage

import (
	"errors"
	"fmt"
	"os"
)

// PostgresConfig holds database connection and pool configuration. Everything
// but the password loads from YAML; the password comes from DB_PASSWORD.
type PostgresConfig struct {
	Host                     string `yaml:"host"`
	Port                     int    `yaml:"port"`
	Database                 string `yaml:"database"`
	User                     string `yaml:"user"`
	SSLMode                  string `yaml:"sslMode"`
	MaxConnections           int32  `yaml:"maxConnections"`
	MinConnections           int32  `yaml:"minConnections"`
	MaxConnLifetimeMinutes   int    `yaml:"maxConnLifetimeMinutes"`
	MaxConnIdleTimeMinutes   int    `yaml:"maxConnIdleTimeMinutes"`
	HealthCheckPeriodSeconds int    `yaml:"healthCheckPeriodSeconds"`
	ConnectTimeoutSeconds    int    `yaml:"connectTimeoutSeconds"`
	CaptureQueryText         bool   `yaml:"captureQueryText"`

	// Loaded from DB_PASSWORD, never from YAML.
	Password string `yaml:"-"`
}

// ConnectionString builds a libpq-style connection string from the config.
func (c *PostgresConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.Port, c.Database, c.User, c.Password, c.SSLMode,
	)
}

// loadPostgresConfig checks the YAML connection settings and reads the password
// from the environment. It is called by LoadStorageConfig.
func loadPostgresConfig(cfg *PostgresConfig) error {
	switch {
	case cfg.Host == "":
		return errors.New("postgres.host must be set")
	case cfg.Database == "":
		return errors.New("postgres.database must be set")
	case cfg.User == "":
		return errors.New("postgres.user must be set")
	}
	cfg.Password = os.Getenv("DB_PASSWORD")
	if cfg.Password == "" {
		return fmt.Errorf("DB_PASSWORD environment variable is required")
	}
	return nil
}
