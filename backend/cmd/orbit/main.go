package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	oc "github.com/clear-route/orbit/cmd/orbit/config"
	"github.com/clear-route/orbit/internal/example"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
)

func main() {
	ctx := context.Background()

	// Initialize observability with deferred cleanup
	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	// Create logger
	logger := initLogger()
	logger.Info(ctx, "Orbit control plane starting", logging.Field{Key: "version", Value: oc.Config.App.Version})

	// Initialize database with deferred cleanup
	db, err := initDatabase(ctx, logger)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create HTTP server with wired controllers
	server := initServer(ctx, logger, db)

	// Run server with graceful shutdown
	run(ctx, logger, server)
}

// initObservability sets up OTEL tracing and returns a cleanup function
func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     oc.Config.Observability.ServiceName,
		ServiceVersion:  oc.Config.App.Version,
		Environment:     oc.Config.Environment(),
		OTLPEndpoint:    oc.Config.Observability.OTLPEndpoint,
		SamplingRate:    oc.Config.Observability.SamplingRate,
		OTLPInsecure:    oc.Config.Observability.OTLPInsecure,
		ShutdownTimeout: oc.Config.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	// Return cleanup function that uses fresh context to avoid canceled context issues
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), oc.Config.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}, nil
}

// initLogger creates the appropriate logger based on environment
func initLogger() logging.Logger {
	if oc.Config.App.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// initDatabase loads configuration and initializes database connection
func initDatabase(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
	storageConfig, err := storage.LoadConfig()
	if err != nil {
		return nil, err
	}

	db, err := storage.New(ctx, storageConfig, logger)
	if err != nil {
		return nil, err
	}

	logger.Info(ctx, "Database initialized successfully")
	return db, nil
}

// initControllers wires up repositories and controllers
func initControllers(logger logging.Logger, db *storage.DB) []api.Controller {
	// Create repositories
	exampleRepo := example.NewPostgresRepository(db.Pool())

	// Create controllers with dependencies
	exampleController := example.NewController(logger, exampleRepo)

	return []api.Controller{exampleController}
}

// initServer creates and configures the HTTP server with middleware and controllers
func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) *http.Server {
	apiServer := api.NewAPI(ctx, logger)

	// Wire up controllers
	controllers := initControllers(logger, db)

	// Create HTTP server with middleware chain
	// Middleware order: Tracing (provides context) → Logging (uses trace context)
	return apiServer.Server(
		ctx,
		oc.Config.Server.Port,
		oc.Config.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(oc.Config.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)
}

// run starts the HTTP server and handles graceful shutdown
func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	// Setup signal handling for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in background
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-quit
	logger.Info(ctx, "Shutting down server...")

	// Graceful shutdown with environment-specific timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, oc.Config.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Orbit control plane shutdown complete")
}
