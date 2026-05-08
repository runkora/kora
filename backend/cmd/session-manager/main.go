package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"time"

	smConfig "github.com/clear-route/orbit/cmd/session-manager/config"
	"github.com/clear-route/orbit/internal/agents/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
)

func main() {
	ctx := context.Background()

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObs()

	logger := initLogger()
	logger.Info(ctx, "Session-manager starting",
		logging.Field{Key: "version", Value: smConfig.Current.App.Version},
	)

	db, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObs()
		os.Exit(1) //nolint:gocritic
	}
	defer db.Close()

	server := initServer(ctx, logger, db)
	run(ctx, logger, server)
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     smConfig.Current.Observability.ServiceName,
		ServiceVersion:  smConfig.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: smConfig.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), smConfig.Current.ShutdownTimeout())
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

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) *http.Server {
	apiServer := api.NewAPI(ctx, logger)

	cfg := smConfig.Current

	var notifier sessionmanager.Notifier = sessionmanager.NoopNotifier{}
	if cfg.OrbitAgents.URL != "" {
		notifier = sessionmanager.NewHTTPNotifier(cfg.OrbitAgents.URL)
	}

	idleTimeout := time.Duration(cfg.Session.IdleTimeoutSecs) * time.Second
	registry := sessionmanager.NewRegistry(logger, db.Pool(), cfg.Workspaces.BasePath, notifier, idleTimeout)
	controller := sessionmanager.NewController(logger, sessionmanager.ControllerConfig{
		ClaudeBinaryPath:   cfg.Claude.BinaryPath,
		ClaudeDefaultModel: cfg.Claude.DefaultModel,
		ClaudeBare:         cfg.Claude.Bare,
		SelfPort:           cfg.Server.Port,
	}, registry)

	return apiServer.Server(
		ctx,
		cfg.Server.Port,
		cfg.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(cfg.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)
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
	logger.Info(ctx, "Shutting down session-manager...")

	shutdownCtx, cancel := context.WithTimeout(ctx, smConfig.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Session-manager shutdown complete")
}
