package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	psc "github.com/clear-route/orbit/cmd/policy-service/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/policyservice/api"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
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
	logger.Info(ctx, "Policy service starting", logging.Field{Key: "version", Value: psc.Current.App.Version})

	storageCfg, err := storage.LoadStorageConfig()
	if err != nil {
		return fmt.Errorf("load storage config: %w", err)
	}

	db, err := storage.New(ctx, &storageCfg.Postgres, logger)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	grantRepo := grants.NewPostgresRepository(db.Pool())
	vocabRepo := vocabulary.NewPostgresRepository(db.Pool())
	compiler := bundle.NewCompiler()
	bundleSrv := bundle.NewServer(compiler, grantRepo, vocabRepo, logger)

	// Pre-compile bundles for all tenants that have grants.
	if tenantIDs, err := grantRepo.ListDistinctTenants(ctx); err == nil {
		bundleSrv.Bootstrap(ctx, tenantIDs)
	} else {
		logger.Error(ctx, "bundle bootstrap: failed to list tenants", logging.Field{Key: "error", Value: err.Error()})
	}

	svc := api.NewService(grantRepo, vocabRepo, bundleSrv, logger)
	controller := api.NewController(svc, logger)
	server := initServer(ctx, logger, controller)

	run(ctx, logger, server)
	return nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     psc.Current.Observability.ServiceName,
		ServiceVersion:  psc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: psc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
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

func initServer(ctx context.Context, logger logging.Logger, controller *api.Controller) *http.Server {
	apiServer := platformapi.NewAPI(ctx, logger)
	return apiServer.Server(
		ctx,
		psc.Current.Server.Port,
		psc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(psc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Policy service shutdown complete")
}

