package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/platform/api"
	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	smclient "github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
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

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer shutdownObs()

	logger := initLogger()
	logger.Info(ctx, "orbit-agents starting",
		logging.Field{Key: "version", Value: agentsConfig.Current.App.Version},
	)

	db, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer db.Close()

	server, err := initServer(ctx, logger, db)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	run(ctx, logger, server)
	return nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     agentsConfig.Current.Observability.ServiceName,
		ServiceVersion:  agentsConfig.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: agentsConfig.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.ShutdownTimeout())
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

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	cfg := agentsConfig.Current

	// Build a policy client for grant management at agent provision/deprovision time.
	// When policyServiceURL is empty (local dev), the provisioner skips grant management.
	var pc *policyclient.Client
	if cfg.Platform.PolicyServiceURL != "" {
		idClient := identityclient.NewClient(cfg.Platform.IdentityServiceURL, logger)
		pc = policyclient.NewClient(cfg.Platform.PolicyServiceURL, func(ctx context.Context) (string, error) {
			tok, _, err := idClient.Exchange(ctx)
			return tok, err
		}, logger)
	}

	k8s, err := agents.NewK8sProvisioner(cfg.Kubernetes.InCluster, logger, agents.K8sProvisionerConfig{
		SessionManagerImage:       cfg.Agents.SessionManagerImage,
		SandboxImage:              cfg.Agents.SandboxImage,
		SandboxGRPCPort:           int32(cfg.Agents.SandboxGRPCPort), //nolint:gosec
		SessionManagerPostgresSSL: cfg.Agents.SessionManager.PostgresSSLMode,
		TrustDomain:               cfg.Platform.TrustDomain,
		SystemNamespace:           cfg.Kubernetes.SystemNamespace,
	}, pc)
	if err != nil {
		return nil, fmt.Errorf("initialize k8s provisioner: %w", err)
	}

	repo := agents.NewPostgresRepository(db.Pool())
	sm := smclient.NewClient(logger)
	pubsub := agents.NewPubSub()
	svc := agents.NewService(repo, k8s, sm, pubsub, logger, agents.ServiceConfig{
		MaxConcurrentRunsPerPod: cfg.Agents.MaxConcurrentRunsPerPod,
		TenantNamespaceFn:       cfg.TenantNamespace,
	})
	controller := agents.NewController(svc, logger)

	return apiServer.Server(
		ctx,
		cfg.Server.Port,
		cfg.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(cfg.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	), nil
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down orbit-agents...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "orbit-agents shutdown complete")
}
