package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	ec "github.com/runkora/kora/backend/cmd/example/config"
	"github.com/runkora/kora/backend/internal/example"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

func main() {
	ctx := context.Background()

	// Initialize observability with deferred cleanup.
	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	// Create logger.
	logger := initLogger()
	logger.Info(ctx, "Example service starting", logging.Field{Key: "version", Value: ec.Current.App.Version})

	// Initialize the Postgres connection pool. The example service does not use
	// Redis, so only the DB is initialized here.
	db, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: explicit cleanup needed before exit
	}
	defer db.Close()

	// Create HTTP server with wired controllers.
	server := initServer(ctx, logger, db)

	// Run server with graceful shutdown.
	run(ctx, logger, server)
}

// initObservability sets up OTEL tracing and returns a cleanup function.
func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     ec.Current.Observability.ServiceName,
		ServiceVersion:  ec.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: ec.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ec.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}, nil
}

// initLogger creates the appropriate logger based on environment.
func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// initStorage initializes the PostgreSQL connection pool from the shared
// storage config. The example service does not use Redis.
func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
	cfg, err := storage.LoadPostgresConfig()
	if err != nil {
		return nil, err
	}

	db, err := storage.New(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(ctx, "Database initialized successfully")

	return db, nil
}

// initServer creates and configures the HTTP server with middleware and
// the example controller.
func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) *http.Server {
	apiServer := api.NewAPI(ctx, logger)

	repo := example.NewPostgresRepository(db.Pool())
	controller := example.NewController(logger, repo)

	// Middleware order: Tracing (provides context) → Logging (uses trace context).
	return apiServer.Server(
		ctx,
		ec.Current.Server.Port,
		ec.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(ec.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)
}

// run starts the HTTP server and handles graceful shutdown.
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
	logger.Info(ctx, "Shutting down example service...")

	shutdownCtx, cancel := context.WithTimeout(ctx, ec.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Example service shutdown complete")
}
