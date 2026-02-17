package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/clear-route/orbit/internal/example"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
)

func main() {
	// Initialize observability (OTEL tracing)
	ctx := context.Background()
	shutdown, err := observability.SetupFromEnv(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer func() {
		// Use a fresh context with timeout for shutdown to avoid using cancelled context
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}()

	// Create logger instance
	logger := logging.NewDevelopmentLogger()
	logger.Info(ctx, "Orbit control plane starting", logging.Field{Key: "version", Value: "0.1.0"})

	// Initialize API server with context and logger
	apiServer := api.NewAPI(ctx, logger)

	// Create example controller
	exampleController := example.NewController(logger)

	// Create HTTP server with controllers
	// Server handles controller registration and middleware setup
	// Middleware chain order:
	//   1. TracingMiddleware - extracts trace context, creates spans
	//   2. LoggingMiddleware - logs with automatic trace correlation
	server := apiServer.Server(
		ctx,
		6069,      // port
		"/api/v1", // base path
		mw.MiddlewareChain( // server-level middleware
			mw.TracingMiddleware("orbit"), // OTEL tracing (first - provides context)
			mw.LoggingMiddleware(logger),  // Request logging (second - uses trace context)
		),
		exampleController, // controllers to register
	)

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Run server in goroutine
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-quit
	logger.Info(ctx, "Shutting down server...")

	// Graceful shutdown with 5 second timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Orbit control plane shutdown complete")
}
