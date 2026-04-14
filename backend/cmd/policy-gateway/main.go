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
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policygw"
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

	eval := policy.NewEvaluator(pgc.Current.Policy.BundleEndpoint, pgc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	svc := policygw.NewService(eval, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// HTTP server for the utility check/query API.
	// Layer 1 enforcement is now handled by the OPA Envoy plugin sidecar
	// on agent pods — the gRPC ext_authz server has been removed.
	controller := policygw.NewController(svc, verifier, logger)
	apiServer := platformapi.NewAPI(ctx, logger)
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

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), pgc.Current.ShutdownTimeout())
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

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
