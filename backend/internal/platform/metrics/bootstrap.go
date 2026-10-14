package metrics

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/metric"

	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
)

// Bootstrap owns one service's full metrics pipeline: the OTel meter
// instruments are built on, the per-route HTTP metrics middleware, and (when
// cfg.Enabled) the dedicated Prometheus-scrape *http.Server. Every
// cmd/{service}/main.go wires these through Setup, Middleware, Start and
// Shutdown instead of each re-deriving the OTel/Prometheus plumbing. See
// docs/adr/2026-10-06-prometheus-metrics-endpoints.md.
type Bootstrap struct {
	meter      metric.Meter
	middleware mw.MiddlewareHandler
	srv        *http.Server // nil when cfg.Enabled is false
	shutdown   func(context.Context) error
}

// Setup builds the OTel metrics pipeline (OTLP when a collector is
// configured per obsCfg, Prometheus when cfg.Enabled) and the per-route HTTP
// metrics middleware for serviceName.
func Setup(ctx context.Context, cfg Config, obsCfg observability.Config, serviceName string, logger logging.Logger) (*Bootstrap, error) {
	exp, err := New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	meter, shutdownOtel, err := observability.SetupMetrics(ctx, obsCfg, exp.Reader())
	if err != nil {
		return nil, err
	}

	metricsMW, err := mw.MetricsMiddleware(meter, serviceName)
	if err != nil {
		return nil, fmt.Errorf("create metrics middleware: %w", err)
	}

	return &Bootstrap{
		meter:      meter,
		middleware: mw.MiddlewareChain(metricsMW),
		srv:        exp.Server(logger),
		shutdown:   shutdownOtel,
	}, nil
}

// Meter is the instruments a service's own business-metric instrumentation
// should be built on, in addition to the generic HTTP middleware above.
func (b *Bootstrap) Meter() metric.Meter {
	return b.meter
}

// Middleware is the per-route HTTP request counter/histogram. Wire it with
// api.Server.SetMetricsMiddleware (or Mux.SetMetricsMiddleware), never into
// the global middleware chain — see Mux.HandleWithMiddleware for why it must
// run after RoutePattern.
func (b *Bootstrap) Middleware() mw.MiddlewareHandler {
	return b.middleware
}

// Start launches the dedicated Prometheus-scrape server in the background.
// No-op when metrics are disabled.
func (b *Bootstrap) Start(ctx context.Context, logger logging.Logger) {
	if b.srv == nil {
		return
	}
	go func() {
		logger.Info(ctx, "Starting metrics server", logging.Field{Key: "addr", Value: b.srv.Addr})
		if err := b.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start metrics server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
}

// StopServer gracefully shuts down the Prometheus-scrape server within ctx's
// deadline. Call it alongside the main API server's own Shutdown, under the
// same shutdown context. No-op when metrics are disabled.
func (b *Bootstrap) StopServer(ctx context.Context, logger logging.Logger) {
	if b.srv == nil {
		return
	}
	if err := b.srv.Shutdown(ctx); err != nil {
		logger.Error(ctx, "Metrics server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
}

// Shutdown flushes the OTel metrics pipeline within ctx's deadline. Call it
// from a defer in main, after run() returns, with a context built from this
// service's cfg.Metrics.ShutdownTimeout() — separate from StopServer's
// context because the OTel flush and the dedicated server's drain are
// independent budgets.
func (b *Bootstrap) Shutdown(ctx context.Context, logger logging.Logger) {
	if err := b.shutdown(ctx); err != nil {
		logger.Error(ctx, "Error shutting down metrics", logging.Field{Key: "error", Value: err.Error()})
	}
}
