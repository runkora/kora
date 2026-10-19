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
	"github.com/clear-route/orbit/internal/platform/brand"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/token"
)

func main() {
	ctx := context.Background()

	// orbit-agents sets ORBIT_PRODUCT_NAME on session pods, which the chart does not render.
	// Load the brand first so a bad name exits before observability starts.
	b, err := brand.Load()
	if err != nil {
		log.Fatalf("Failed to load brand: %v", err)
	}

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObs()

	logger := initLogger()
	logger.Info(ctx, "Session-manager starting",
		logging.Field{Key: "version", Value: smConfig.Current.App.Version},
	)

	// fatal flushes observability (the deferred shutdown above won't run
	// after os.Exit) and exits.
	fatal := func(msg string, err error) {
		logger.Error(ctx, msg, logging.Field{Key: "error", Value: err})
		shutdownObs()
		os.Exit(1)
	}

	metrics, err := platformmetrics.Setup(ctx, smConfig.Current.Metrics, observability.Config{
		ServiceName:    smConfig.Current.Observability.ServiceName,
		ServiceVersion: smConfig.Current.App.Version,
		Environment:    config.Environment(),
	}, smConfig.Current.Observability.ServiceName, logger)
	if err != nil {
		fatal("Failed to setup metrics", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), smConfig.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	server, registry := initServer(ctx, logger, metrics, b)
	run(ctx, logger, server, registry, metrics)
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

func initServer(ctx context.Context, logger logging.Logger, metrics *platformmetrics.Bootstrap, b brand.Brand) (*http.Server, *sessionmanager.Registry) {
	apiServer := api.NewAPI(ctx, logger)

	cfg := smConfig.Current

	var notifier sessionmanager.Notifier = sessionmanager.NoopNotifier{}
	if cfg.OrbitAgents.URL != "" {
		notifier = sessionmanager.NewHTTPNotifier(cfg.OrbitAgents.URL, logger)
	}

	// The session-control API can start, message, interrupt, and kill any run on
	// this pod, so it is always authenticated. There is no opt-out: local
	// development runs in Kubernetes against a real identity service
	// (docs/adr/2026-07-10-kubernetes-only-local-development.md), so a mode that
	// serves this API unauthenticated would exist only to be switched on by
	// mistake.
	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())

	idleTimeout := time.Duration(cfg.Session.IdleTimeoutSecs) * time.Second
	registry := sessionmanager.NewRegistry(logger, cfg.Workspaces.BasePath, notifier, idleTimeout, b)
	controller := sessionmanager.NewController(logger, sessionmanager.ControllerConfig{
		ClaudeBinaryPath:    cfg.Claude.BinaryPath,
		WrapperPath:         cfg.Runner.WrapperPath,
		ShellPath:           cfg.Runner.ShellPath,
		ClaudeBare:          cfg.Claude.Bare,
		SelfPort:            cfg.Server.Port,
		WorkspacesBasePath:  cfg.Workspaces.BasePath,
		CodexBinaryPath:     cfg.Codex.BinaryPath,
		OpenCodeBinaryPath:  cfg.OpenCode.BinaryPath,
		InferenceGatewayURL: cfg.InferenceGateway.URL,
		ToolGatewayURL:      cfg.ToolGateway.URL,
		OTLPEndpoint:        observability.DefaultConfig.OTLPEndpoint,
		NodeMaxOldSpaceMB:   cfg.Runner.NodeMaxOldSpaceMB,
		Brand:               b,
	}, registry, verifier)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	return apiServer.Server(
		ctx,
		cfg.Server.Port,
		cfg.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(cfg.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	), registry
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, registry *sessionmanager.Registry, metrics *platformmetrics.Bootstrap) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	metrics.Start(ctx, logger)

	<-quit
	// The server keeps serving the runs on this pod while they finish. The kubelet
	// stops the pod when its termination grace period ends.
	logger.Info(ctx, "Draining session-manager", logging.Field{Key: "active_sessions", Value: registry.ActiveCount()})
	drainCtx, cancelDrain := ctx, context.CancelFunc(func() {})
	if d, limited := smConfig.Current.DrainTimeout(); limited {
		drainCtx, cancelDrain = context.WithTimeout(ctx, d)
	}
	registry.Drain(drainCtx, time.Second)
	cancelDrain()
	registry.CloseAll(ctx)
	logger.Info(ctx, "Shutting down session-manager...")

	shutdownCtx, cancel := context.WithTimeout(ctx, smConfig.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Session-manager shutdown complete")
}
