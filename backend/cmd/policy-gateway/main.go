package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	pgc "github.com/runkora/kora/backend/cmd/policy-gateway/config"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policygw"
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

	metrics, err := platformmetrics.Setup(ctx, pgc.Current.Metrics, observability.Config{
		ServiceName:    pgc.Current.Observability.ServiceName,
		ServiceVersion: pgc.Current.App.Version,
		Environment:    config.Environment(),
	}, pgc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	eval := policy.NewEvaluator(pgc.Current.Policy.BundleEndpoint, pgc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	svc := policygw.NewService(eval, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// The shared revoked-JTI blocklist refuses a revoked delegation before its
	// token expires (docs/adr/2026-07-23-delegated-agent-identity.md).
	redisCfg, err := storage.LoadRedisConfig()
	if err != nil {
		return fmt.Errorf("load redis config: %w", err)
	}
	redis, err := storage.NewRedis(ctx, redisCfg, logger)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	defer func() { _ = redis.Close() }()

	// HTTP server for the utility check/query API.
	// Layer 1 enforcement is now handled by the OPA Envoy plugin sidecar
	// on agent pods — the gRPC ext_authz server has been removed.
	controller := policygw.NewController(svc, verifier, storage.NewJTIBlocklist(redis), logger)
	apiServer := platformapi.NewAPI(ctx, logger)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	httpSrv := apiServer.Server(
		ctx,
		pgc.Current.Server.HTTPPort,
		pgc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(),
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
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.ShutdownTimeout())
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Policy gateway shutdown complete")
	return nil
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
