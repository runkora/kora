
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	sc "github.com/clear-route/orbit/cmd/skills-service/config"
	"github.com/clear-route/orbit/internal/platform/api"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/skills"
)

func main() {
	ctx := context.Background()
	logger := initLogger()

	if err := sc.Current.ApplyEnvOverrides(); err != nil {
		logger.Error(ctx, "invalid config", logging.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	shutdownObservability, err := initObservability(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to setup observability", logging.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	logger.Info(ctx, "skills-service starting", logging.Field{Key: "version", Value: sc.Current.App.Version})

	redis, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to initialize storage", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1)
	}

	registry, err := initRegistry(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to initialize marketplace registry", logging.Field{Key: "error", Value: err.Error()})
		_ = redis.Close()
		shutdownObservability()
		os.Exit(1)
	}

	// backgroundCtx ends after the HTTP server stops. The policy bundle poll
	// stops with it.
	backgroundCtx, stopBackground := context.WithCancel(ctx)

	server, register, err := initServer(backgroundCtx, logger, redis, registry)
	if err != nil {
		logger.Error(ctx, "failed to initialize server", logging.Field{Key: "error", Value: err.Error()})
		stopBackground()
		_ = redis.Close()
		shutdownObservability()
		os.Exit(1)
	}

	exitCode := run(ctx, logger, server, register)
	stopBackground()

	if err := redis.Close(); err != nil {
		logger.Error(ctx, "failed to close redis", logging.Field{Key: "error", Value: err.Error()})
	}
	shutdownObservability()
	os.Exit(exitCode)
}

func initObservability(ctx context.Context, logger logging.Logger) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     sc.Current.Observability.ServiceName,
		ServiceVersion:  sc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: sc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), sc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			logger.Error(
				shutdownCtx,
				"error shutting down observability",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}, nil
}

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

func initStorage(ctx context.Context, logger logging.Logger) (*storage.RedisClient, error) {
	cfg, err := storage.LoadRedisConfig()
	if err != nil {
		return nil, err
	}

	redis, err := storage.NewRedis(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	logger.Info(ctx, "redis initialized successfully")
	return redis, nil
}

func initRegistry(_ context.Context, logger logging.Logger) (*skills.Registry, error) {
	return skills.NewRegistry(sc.Current.Marketplace.SourcesConfigPath, logger)
}

func initControllers(
	ctx context.Context,
	logger logging.Logger,
	redis *storage.RedisClient,
	registry *skills.Registry,
) ([]api.Controller, error) {
	installationID, err := strconv.ParseInt(sc.Current.GitHub.InstallationID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid GITHUB_APP_INSTALLATION_ID: %w", err)
	}

	gh := ghclient.NewClient(sc.Current.GitHub.APIBaseURL, logger)

	// Single GitHub App token provider — all marketplaces share the same credentials.
	defaultTokens := skills.NewTokenProvider(
		gh,
		sc.Current.GitHub.AppID,
		installationID,
		sc.Current.GitHub.PrivateKeyPath,
	)

	crawler := skills.NewCrawler(gh, defaultTokens, logger)
	cache := skills.NewCache(redis, sc.Current.CacheTTL())

	pol := policy.NewEvaluator(
		sc.Current.Policy.BundleEndpoint,
		sc.Current.Policy.BundlePollInterval(),
		logger,
	)
	pol.Start(ctx)

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	svc := skills.NewService(registry, cache, crawler, logger)

	controller := skills.NewController(
		logger,
		svc,
		registry,
		verifier,
		pol,
		sc.Current.Server.BasePath,
		sc.Current.Marketplace.LocalTenantID,
	)

	return []api.Controller{controller}, nil
}

// initScopeRegistration returns a function that registers the marketplace
// route/scope vocabulary with the policy service. The caller passes it to
// routescopes.KeepRegistered, which owns the initial registration and the
// periodic self-heal loop.
func initScopeRegistration(logger logging.Logger) func(context.Context) error {
	client := routescopes.NewClient(sc.Current.Policy.PolicyServiceURL, logger)

	return func(registerCtx context.Context) error {
		scopedRoutes := routescopes.FilterScopedRoutes(
			skills.MarketplaceRoutes(sc.Current.Server.BasePath),
		)

		if err := client.Register(
			registerCtx,
			"skills-service",
			scopedRoutes,
			skills.MarketplaceScopes(),
		); err != nil {
			return fmt.Errorf("register routes: %w", err)
		}

		return nil
	}
}

func initServer(
	ctx context.Context,
	logger logging.Logger,
	redis *storage.RedisClient,
	registry *skills.Registry,
) (*http.Server, func(context.Context) error, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, redis, registry)
	if err != nil {
		return nil, nil, err
	}

	var register func(context.Context) error

	if sc.Current.Policy.RegisterRoutes {
		register = initScopeRegistration(logger)
	}

	server := apiServer.Server(
		ctx,
		sc.Current.Server.Port,
		sc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(sc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)

	return server, register, nil
}

func run(
	ctx context.Context,
	logger logging.Logger,
	server *http.Server,
	register func(context.Context) error,
) int {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)

	go func() {
		logger.Info(
			ctx,
			"starting HTTP server",
			logging.Field{Key: "addr", Value: server.Addr},
		)

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}

		serverErr <- nil
	}()

	if register != nil {
		// Register now and keep the vocabulary alive across policy-service restarts.
		go routescopes.KeepRegistered(ctx, logger, "skills-service", register)
	}

	select {
	case err := <-serverErr:
		if err != nil {
			logger.Error(
				ctx,
				"failed to start server",
				logging.Field{Key: "error", Value: err.Error()},
			)
			return 1
		}
	case <-quit:
		logger.Info(ctx, "shutting down skills-service...")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, sc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(
			ctx,
			"server forced to shutdown",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}

	logger.Info(ctx, "skills-service shutdown complete")
	return 0
}
