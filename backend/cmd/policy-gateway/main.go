package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	pgc "github.com/clear-route/orbit/cmd/policy-gateway/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policygw"
	"go.opentelemetry.io/otel/metric"
)

func main() {
	if err := realMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func realMain() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer shutdownObservability()

	logger := initLogger()
	logger.Info(ctx, "Policy gateway starting", logging.Field{Key: "version", Value: pgc.Current.App.Version})

	meter, metricsSrv, shutdownMeter, err := initMeter(ctx, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer shutdownMeter()

	eval := policy.NewEvaluator(pgc.Current.Policy.BundleEndpoint, pgc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	svc := policygw.NewService(eval, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// HTTP server for the utility check/query API.
	// Layer 1 enforcement is now handled by the OPA Envoy plugin sidecar
	// on agent pods — the gRPC ext_authz server has been removed.
	controller := policygw.NewController(svc, verifier, logger)
	apiServer := platformapi.NewAPI(ctx, logger)

	metricsMW, err := mw.MetricsMiddleware(meter, pgc.Current.Observability.ServiceName)
	if err != nil {
		return fmt.Errorf("create metrics middleware: %w", err)
	}
	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(mw.MiddlewareChain(metricsMW))

	httpSrv := apiServer.Server(
		ctx,
		pgc.Current.Server.HTTPPort,
		pgc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(pgc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: httpSrv.Addr})
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	if metricsSrv != nil {
		go func() {
			logger.Info(ctx, "Starting metrics server", logging.Field{Key: "addr", Value: metricsSrv.Addr})
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error(ctx, "Failed to start metrics server", logging.Field{Key: "error", Value: err.Error()})
			}
		}()
	}

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.ShutdownTimeout())
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error(ctx, "Metrics server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
		}
	}

	logger.Info(ctx, "Policy gateway shutdown complete")
	return nil
}

// initMeter sets up the OTel metrics pipeline — an OTLP reader when a
// collector is configured, and/or a Prometheus reader when metrics are
// enabled — and returns the meter instruments are built on, this service's
// dedicated metrics *http.Server (nil when disabled), and a cleanup func.
func initMeter(ctx context.Context, logger logging.Logger) (metric.Meter, *http.Server, func(), error) {
	promExporter, err := platformmetrics.New(pgc.Current.Metrics)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	meter, shutdownOtelMetrics, err := observability.SetupMetrics(ctx, observability.Config{
		ServiceName:    pgc.Current.Observability.ServiceName,
		ServiceVersion: pgc.Current.App.Version,
		Environment:    config.Environment(),
	}, promExporter.Reader())
	if err != nil {
		return nil, nil, nil, err
	}

	metricsSrv := promExporter.Server(logger)
	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		if err := shutdownOtelMetrics(shutdownCtx); err != nil {
			logger.Error(ctx, "Error shutting down metrics", logging.Field{Key: "error", Value: err.Error()})
		}
	}
	return meter, metricsSrv, cleanup, nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     pgc.Current.Observability.ServiceName,
		ServiceVersion:  pgc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: pgc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down observability: %v\n", err)
		}
	}, nil
}

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}
