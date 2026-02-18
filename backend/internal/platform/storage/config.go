package storage

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Config holds database connection and pool configuration.
// Port and SSLMode are loaded from YAML files in internal/platform/storage/config/{env}.yaml.
// All other connection details (host, database, user, password) are loaded from environment variables.
type Config struct {
	Port                     int    `yaml:"port"`                     // Port is the PostgreSQL server port
	SSLMode                  string `yaml:"sslMode"`                  // SSLMode controls TLS connection behavior (disable, require, verify-ca, verify-full)
	MaxConnections           int32  `yaml:"maxConnections"`           // MaxConnections is the maximum number of connections in the pool
	MinConnections           int32  `yaml:"minConnections"`           // MinConnections is the minimum number of connections to keep warm
	MaxConnLifetimeMinutes   int    `yaml:"maxConnLifetimeMinutes"`   // MaxConnLifetimeMinutes is how long a connection can live before being recycled
	MaxConnIdleTimeMinutes   int    `yaml:"maxConnIdleTimeMinutes"`   // MaxConnIdleTimeMinutes is how long a connection can be idle before being closed
	HealthCheckPeriodSeconds int    `yaml:"healthCheckPeriodSeconds"` // HealthCheckPeriodSeconds is how often to check connection health
	ConnectTimeoutSeconds    int    `yaml:"connectTimeoutSeconds"`    // ConnectTimeoutSeconds is the timeout for establishing new connections
	CaptureQueryText         bool   `yaml:"captureQueryText"`         // CaptureQueryText controls whether SQL statements are included in traces (disable in production for security)

	// The following fields are not loaded from YAML but from environment variables and are replaced with actual values in LoadConfig. They are included here for completeness and to be used in ConnectionString.
	Host     string `yaml:"-"` // Host is the PostgreSQL server hostname (loaded from DB_HOST)
	Database string `yaml:"-"` // Database is the name of the database to connect to (loaded from DB_NAME)
	User     string `yaml:"-"` // User is the database username (loaded from DB_USER)
	Password string `yaml:"-"` // Password is loaded from DB_PASSWORD environment variable (never from YAML)
}

// LoadConfig loads database configuration from YAML and environment variables.
// Port and SSLMode come from internal/platform/storage/config/{environment}.yaml.
// Connection details (host, database, user, password) are loaded from environment variables.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/platform/storage", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load database config: %w", err)
	}

	// Load required connection details from environment
	cfg.Host = os.Getenv("DB_HOST")
	if cfg.Host == "" {
		return nil, fmt.Errorf("DB_HOST environment variable is required")
	}

	cfg.Database = os.Getenv("DB_NAME")
	if cfg.Database == "" {
		return nil, fmt.Errorf("DB_NAME environment variable is required")
	}

	cfg.User = os.Getenv("DB_USER")
	if cfg.User == "" {
		return nil, fmt.Errorf("DB_USER environment variable is required")
	}

	cfg.Password = os.Getenv("DB_PASSWORD")
	if cfg.Password == "" {
		return nil, fmt.Errorf("DB_PASSWORD environment variable is required")
	}

	// Optional: Allow port override via environment
	if port := os.Getenv("DB_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	return cfg, nil
}

// ConnectionString builds a PostgreSQL connection string from the config.
func (c *Config) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, c.Port, c.Database, c.User, c.Password, c.SSLMode,
	)
}

// BuildPoolConfig creates a pgxpool.Config with all pool settings applied.
func (c *Config) BuildPoolConfig() (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(c.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	// Apply pool settings
	poolConfig.MaxConns = c.MaxConnections
	poolConfig.MinConns = c.MinConnections
	poolConfig.MaxConnLifetime = time.Duration(c.MaxConnLifetimeMinutes) * time.Minute
	poolConfig.MaxConnIdleTime = time.Duration(c.MaxConnIdleTimeMinutes) * time.Minute
	poolConfig.HealthCheckPeriod = time.Duration(c.HealthCheckPeriodSeconds) * time.Second

	// Set connection timeout
	if c.ConnectTimeoutSeconds > 0 {
		poolConfig.ConnConfig.ConnectTimeout = time.Duration(c.ConnectTimeoutSeconds) * time.Second
	}

	// Add OpenTelemetry query tracer
	poolConfig.ConnConfig.Tracer = &otelQueryTracer{
		tracer:           otel.Tracer("orbit.database"),
		captureQueryText: c.CaptureQueryText,
	}

	return poolConfig, nil
}

// otelQueryTracer implements pgx.QueryTracer to create spans for database operations
type otelQueryTracer struct {
	tracer           trace.Tracer
	captureQueryText bool
}

// TraceQueryStart creates a span when a query begins
func (t *otelQueryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	attrs := []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
	}
	
	// Only capture SQL statement text if explicitly enabled (disabled in production for security)
	if t.captureQueryText {
		attrs = append(attrs, attribute.String("db.statement", data.SQL))
	}
	
	ctx, _ = t.tracer.Start(ctx, "db.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

// TraceQueryEnd records query completion and any errors
func (t *otelQueryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()

	// Set span status and record errors
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
		span.SetAttributes(attribute.Bool("db.error", true))
		return // Don't set operation attributes on error
	}
	
	// For successful operations, leave span status as Unset (idiomatic OpenTelemetry)
	// Only set status to Error for failures
	
	// Extract operation type from command tag (just the operation, not the full tag)
	// CommandTag.String() returns full tag like "INSERT 0 1", we want just "INSERT"
	commandTag := data.CommandTag.String()
	if commandTag != "" {
		// Parse operation type from command tag (first word before space)
		operation := commandTag
		if spaceIdx := strings.Index(commandTag, " "); spaceIdx > 0 {
			operation = commandTag[:spaceIdx]
		}
		
		span.SetAttributes(
			attribute.String("db.operation", operation),
			attribute.Int64("db.rows_affected", data.CommandTag.RowsAffected()),
		)
	}
}
