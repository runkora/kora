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
)

func main() {
	// Initialize observability (OTEL tracing)
	ctx := context.Background()
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
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer func() {
		// Use a fresh context with timeout for shutdown to avoid using canceled context
		shutdownCtx, cancel := context.WithTimeout(context.Background(), oc.Config.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}()

	// Create logger — console for local development, JSON for all other environments
	var logger logging.Logger
	if oc.Config.App.LogFormat == "console" {
		logger = logging.NewDevelopmentLogger()
	} else {
		logger = logging.NewLogger()
	}
	logger.Info(ctx, "Orbit control plane starting", logging.Field{Key: "version", Value: oc.Config.App.Version})

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
		oc.Config.Server.Port,
		oc.Config.Server.BasePath,
		mw.MiddlewareChain( // server-level middleware
			mw.TracingMiddleware(oc.Config.Observability.ServiceName), // OTEL tracing (first - provides context)
			mw.LoggingMiddleware(logger),                              // Request logging (second - uses trace context)
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

	// Graceful shutdown — timeout is environment-specific (local: 5s, production: 30s)
	shutdownCtx, cancel := context.WithTimeout(ctx, oc.Config.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Orbit control plane shutdown complete")
}
