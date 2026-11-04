package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// BuildPoolConfig creates a pgxpool.Config with all pool settings and the
// OpenTelemetry query tracer applied.
func (c *PostgresConfig) BuildPoolConfig() (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(c.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	poolConfig.MaxConns = c.MaxConnections
	poolConfig.MinConns = c.MinConnections
	poolConfig.MaxConnLifetime = time.Duration(c.MaxConnLifetimeMinutes) * time.Minute
	poolConfig.MaxConnIdleTime = time.Duration(c.MaxConnIdleTimeMinutes) * time.Minute
	poolConfig.HealthCheckPeriod = time.Duration(c.HealthCheckPeriodSeconds) * time.Second

	if c.ConnectTimeoutSeconds > 0 {
		poolConfig.ConnConfig.ConnectTimeout = time.Duration(c.ConnectTimeoutSeconds) * time.Second
	}

	poolConfig.ConnConfig.Tracer = &otelQueryTracer{
		tracer:           otel.Tracer("github.com/runkora/kora/backend/internal/platform/storage"),
		captureQueryText: c.CaptureQueryText,
		dbAttrs: []attribute.KeyValue{
			semconv.DBSystemNamePostgreSQL, semconv.DBNamespace(c.Database),
			semconv.ServerAddress(c.Host), semconv.ServerPort(c.Port),
		},
	}

	return poolConfig, nil
}

// errQueryFailed is the static span status for a failed query.
var errQueryFailed = errors.New("database query failed")

// otelQueryTracer implements pgx.QueryTracer to emit OpenTelemetry spans for
// database operations that run under a parent span. SQL statement text is only captured when
// CaptureQueryText is true (disabled in production for security).
type otelQueryTracer struct {
	tracer           trace.Tracer
	captureQueryText bool
	// dbAttrs holds the database and server keys every query span carries.
	dbAttrs []attribute.KeyValue
}

// TraceQueryStart opens a span for a query under a parent span. A query with no parent records none.
func (t *otelQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(t.dbAttrs...)}
	if t.captureQueryText {
		opts = append(opts, trace.WithAttributes(semconv.DBQueryText(data.SQL)))
	}
	ctx, _ = t.tracer.Start(ctx, semconv.DBSystemNamePostgreSQL.Value.AsString(), opts...)
	return ctx
}

// TraceQueryEnd records query completion and any errors on the active span.
func (t *otelQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()

	if data.Err != nil {
		observability.RecordFailure(span, data.Err, errQueryFailed)
		return
	}

	if data.CommandTag.String() != "" {
		span.SetAttributes(observability.DBRowsAffectedKey.Int64(data.CommandTag.RowsAffected()))
	}
}
