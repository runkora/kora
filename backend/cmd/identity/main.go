package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	ic "github.com/clear-route/orbit/cmd/identity/config"
	"github.com/clear-route/orbit/internal/identity"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
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
	logger.Info(ctx, "Identity service starting", logging.Field{Key: "version", Value: ic.Current.App.Version})

	// Initialize storage backends (Postgres + Redis) from a single config load.
	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: explicit cleanup needed before exit
	}
	defer db.Close()
	defer redis.Close() //nolint:errcheck

	// Create HTTP server with wired controllers.
	server, err := initServer(ctx, logger, db, redis)
	if err != nil {
		logger.Error(ctx, "Failed to initialize server", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1)
	}

	// Run server with graceful shutdown.
	run(ctx, logger, server)
}

// initObservability sets up OTEL tracing and returns a cleanup function.
func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     ic.Current.Observability.ServiceName,
		ServiceVersion:  ic.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: ic.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ic.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}, nil
}

// initLogger creates the appropriate logger based on environment.
func initLogger() logging.Logger {
	if config.Platform.Observability.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// initStorage loads all storage configuration from a single YAML file and initializes
// both the PostgreSQL connection pool and the Redis client.
func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, *storage.RedisClient, error) {
	cfg, err := storage.LoadStorageConfig()
	if err != nil {
		return nil, nil, err
	}

	db, err := storage.New(ctx, &cfg.Postgres, logger)
	if err != nil {
		return nil, nil, err
	}
	logger.Info(ctx, "Database initialized successfully")

	rc, err := storage.NewRedis(ctx, &cfg.Redis, logger)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	logger.Info(ctx, "Redis initialized successfully")

	return db, rc, nil
}

// initControllers wires up repositories, services, and controllers for the
// identity service. ctx is passed through because the identity service performs
// OIDC provider discovery eagerly during construction.
func initControllers(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient) ([]api.Controller, error) {
	identityCfg, err := identity.LoadConfig()
	if err != nil {
		return nil, err
	}

	sessionRepo := identity.NewPostgresSessionRepository(db.Pool())
	refreshRepo := identity.NewPostgresRefreshTokenRepository(db.Pool())
	blocklist := identity.NewRedisJTIBlocklistRepository(redis.Client())

	// XFCCPrincipalExtractor reads the Envoy-injected x-forwarded-client-cert
	// header after mTLS termination. In local development, callers simply set
	// the header directly — no stub or special dev path needed
	// (ADR-0010, ADR-0011, ADR-0015).
	extractor := &identity.XFCCPrincipalExtractor{}

	svc, err := identity.NewService(
		ctx,
		identityCfg,
		sessionRepo,
		refreshRepo,
		blocklist,
		extractor,
		logger,
	)
	if err != nil {
		return nil, err
	}

	return []api.Controller{
		identity.NewController(svc, logger),
	}, nil
}

// initServer creates and configures the HTTP server with middleware and controllers.
func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, db, redis)
	if err != nil {
		return nil, err
	}

	// Middleware order: Tracing (provides context) → Logging (uses trace context).
	server := apiServer.Server(
		ctx,
		ic.Current.Server.Port,
		ic.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(ic.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)

	return server, nil
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
	logger.Info(ctx, "Shutting down identity service...")

	shutdownCtx, cancel := context.WithTimeout(ctx, ic.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Identity service shutdown complete")
}
