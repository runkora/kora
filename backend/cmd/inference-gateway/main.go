package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	igc "github.com/clear-route/orbit/cmd/inference-gateway/config"
	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
)

func main() {
	ctx := context.Background()

	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	logger := initLogger()
	logger.Info(ctx, "Inference gateway starting", logging.Field{Key: "version", Value: igc.Current.App.Version})

	db, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic
	}
	defer db.Close()

	server, err := initServer(ctx, logger, db)
	if err != nil {
		logger.Error(ctx, "Failed to initialize server", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1)
	}

	run(ctx, logger, server)
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     igc.Current.Observability.ServiceName,
		ServiceVersion:  igc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: igc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), igc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}, nil
}

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// initStorage initializes only PostgreSQL — the inference gateway does not use Redis.
func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
	cfg, err := storage.LoadStorageConfig()
	if err != nil {
		return nil, err
	}

	db, err := storage.New(ctx, &cfg.Postgres, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(ctx, "Database initialized successfully")

	return db, nil
}

func initControllers(ctx context.Context, logger logging.Logger, db *storage.DB) ([]api.Controller, error) {
	// ── Provider connectors ───────────────────────────────────────────────────
	// Register factories for every supported provider. Each factory reads its
	// own env vars via LoadConfig — a nil result means the provider is disabled.
	// Adding a new provider is a single line here.
	factories := []inferencegw.ConnectorFactory{
		bedrock.Factory{},
		vertexai.Factory{},
		azure.Factory{},
	}

	connectors := map[inferencegw.ProviderName]inferencegw.ProviderConnector{}
	for _, f := range factories {
		c, err := f.Build(logger)
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", f.Name(), err)
		}
		if c != nil {
			connectors[f.Name()] = c
			logger.Info(ctx, "provider enabled", logging.Field{Key: "provider", Value: string(f.Name())})
		}
	}

	if len(connectors) == 0 {
		logger.Warn(ctx, "warning: no providers enabled — all inference requests will fail")
	}

	// ── Domain layer ──────────────────────────────────────────────────────────
	pricing := inferencegw.NewNopPricingCache()
	router := inferencegw.NewRoutingEngine(connectors, pricing)
	policy := &inferencegw.AllowAllPolicyEvaluator{}
	logger.Warn(ctx, "WARNING: using AllowAllPolicyEvaluator — all inference requests are permitted without policy evaluation; replace before production use")
	usageRepo := inferencegw.NewPostgresInferenceUsageRepository(db.Pool())

	// Warm the alias table before accepting traffic so that alias routing
	// (e.g. "bedrock/claude-sonnet") works from the very first request.
	// A failure here is non-fatal — aliases will resolve after the first
	// successful GET /v1/models call.
	if _, err := router.ListModels(ctx); err != nil {
		logger.Warn(ctx, "startup model warm-up failed — alias routing unavailable until next /v1/models call",
			logging.Field{Key: "error", Value: err})
	} else {
		logger.Info(ctx, "model catalog warm-up complete")
	}

	svc := inferencegw.NewService(policy, router, pricing, usageRepo, logger)

	return []api.Controller{
		inferencegw.NewController(ctx, svc, logger),
	}, nil
}

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, db)
	if err != nil {
		return nil, err
	}

	server := apiServer.Server(
		ctx,
		igc.Current.Server.Port,
		igc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(igc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)

	return server, nil
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	<-quit
	logger.Info(ctx, "Shutting down inference gateway...")

	shutdownCtx, cancel := context.WithTimeout(ctx, igc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Inference gateway shutdown complete")
}
