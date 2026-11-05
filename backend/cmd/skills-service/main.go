package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	sc "github.com/runkora/kora/backend/cmd/skills-service/config"
	"github.com/runkora/kora/backend/internal/platform/api"
	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/skills"
)

func main() {
	ctx := context.Background()
	logger := initLogger()

	shutdownObservability, err := initObservability(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to setup observability", logging.Field{Key: "error", Value: err.Error()})
		os.Exit(1)
	}

	logger.Info(ctx, "skills-service starting", logging.Field{Key: "version", Value: sc.Current.App.Version})
	if err = sc.Current.Validate(); err != nil {
		logger.Error(ctx, "invalid config", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1)
	}

	metrics, err := platformmetrics.Setup(ctx, sc.Current.Metrics, observability.Config{
		ServiceName:    sc.Current.Observability.ServiceName,
		ServiceVersion: sc.Current.App.Version,
		Environment:    config.Environment(),
	}, sc.Current.Observability.ServiceName, logger)
	if err != nil {
		logger.Error(ctx, "failed to setup metrics", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1)
	}

	redis, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "failed to initialize storage", logging.Field{Key: "error", Value: err.Error()})
		shutdownObservability()
		os.Exit(1)
	}

	gh := ghclient.NewClient(sc.Current.GitHub.APIBaseURL, logger)
	registry, err := skills.NewRegistry(ctx, sc.Current.Marketplace.SourcesPath, sc.Current.Marketplace.KeysDir, gh, logger)
	if err != nil {
		logger.Error(ctx, "failed to initialize marketplace registry", logging.Field{Key: "error", Value: err.Error()})
		_ = redis.Close()
		shutdownObservability()
		os.Exit(1)
	}

	// backgroundCtx ends after the HTTP server stops. The policy bundle poll
	// stops with it.
	backgroundCtx, stopBackground := context.WithCancel(ctx)

	go registry.Watch(backgroundCtx)

	server, register := initServer(backgroundCtx, logger, redis, registry, gh, metrics)

	exitCode := run(ctx, logger, server, register, metrics)
	stopBackground()

	metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), sc.Current.Metrics.ShutdownTimeout())
	metrics.Shutdown(metricsCtx, logger)
	cancelMetrics()

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

func initControllers(ctx context.Context, logger logging.Logger, redis *storage.RedisClient, registry *skills.Registry, gh *ghclient.Client) []api.Controller {
	limits := sc.Current.Marketplace.Limits
	crawler := skills.NewCrawler(gh, skills.Limits{
		SkillsPerMarketplace:       limits.SkillsPerMarketplace,
		FilesPerSkill:              limits.FilesPerSkill,
		BytesPerFile:               limits.BytesPerFile,
		SkillMdBytesPerMarketplace: limits.SkillMdBytesPerMarketplace,
	}, logger)
	cache := skills.NewCache(redis, sc.Current.CacheTTL(), sc.Current.FailureTTL())

	pol := policy.NewEvaluator(
		sc.Current.Policy.BundleEndpoint,
		sc.Current.Policy.BundlePollInterval(),
		logger,
	)
	pol.Start(ctx)

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	svc := skills.NewService(registry, cache, crawler, sc.Current.CacheTTL(), logger)

	// Shared revoked-JTI blocklist: rejects logged-out platform tokens and
	// revoked delegation tokens before natural expiry.
	jtiBlocklist := storage.NewJTIBlocklist(redis)

	return []api.Controller{skills.NewController(logger, svc, verifier, pol, jtiBlocklist, sc.Current.Server.BasePath)}
}

// initScopeRegistration returns a function that registers the skills
// route and scope vocabulary with the policy service. The caller passes it to
// routescopes.KeepRegistered, which owns the initial registration and the
// periodic self-heal loop.
func initScopeRegistration(logger logging.Logger) func(context.Context) error {
	identityClient := identityclient.NewClient(sc.Current.Identity.InternalURL, logger)
	selfToken := identityClient.SelfTokenSource()

	client := routescopes.NewClient(
		sc.Current.Policy.PolicyServiceURL,
		selfToken,
		logger,
	)

	return func(registerCtx context.Context) error {
		scopedRoutes := routescopes.FilterScopedRoutes(
			skills.MarketplaceRoutes(sc.Current.Server.BasePath),
		)

		if err := client.Register(
			registerCtx,
			"skills-service",
			skills.Routes(sc.Current.Server.BasePath),
			skills.Scopes(),
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
	gh *ghclient.Client,
	metrics *platformmetrics.Bootstrap,
) (*http.Server, func(context.Context) error) {
	apiServer := api.NewAPI(ctx, logger)
	// Per route, after RoutePattern has set the route on the request context.
	apiServer.SetMetricsMiddleware(metrics.Middleware())
	controllers := initControllers(ctx, logger, redis, registry, gh)

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

	return server, register
}

func run(
	ctx context.Context,
	logger logging.Logger,
	server *http.Server,
	register func(context.Context) error,
	metrics *platformmetrics.Bootstrap,
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

	metrics.Start(ctx, logger)

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
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "skills-service shutdown complete")
	return 0
}
