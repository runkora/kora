// The kora-events binary owns the event log. It relays producer outbox rows
// into the log, serves the read API and the event streams, and deletes
// events past their tenant's retention.
//
// See docs/adr/2026-11-06-event-spine.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	ec "github.com/runkora/kora/backend/cmd/kora-events/config"
	"github.com/runkora/kora/backend/internal/koraevents"
	"github.com/runkora/kora/backend/internal/platform/api"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
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

	if err := ec.Current.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	shutdownObservability, err := observability.Setup(ctx, observability.Config{
		ServiceName:     ec.Current.Observability.ServiceName,
		ServiceVersion:  ec.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: ec.Current.ShutdownTimeout(),
	})
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ec.Current.ShutdownTimeout())
		defer cancel()
		_ = shutdownObservability(shutdownCtx)
	}()

	logger := logging.NewLogger()
	if observability.DefaultConfig.LogFormat == "console" {
		logger = logging.NewDevelopmentLogger()
	}
	logger.Info(ctx, "kora-events starting", logging.Field{Key: "version", Value: ec.Current.App.Version})

	metrics, err := platformmetrics.Setup(ctx, ec.Current.Metrics, observability.Config{
		ServiceName:    ec.Current.Observability.ServiceName,
		ServiceVersion: ec.Current.App.Version,
		Environment:    config.Environment(),
	}, ec.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ec.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	storageCfg, err := storage.LoadStorageConfig()
	if err != nil {
		return fmt.Errorf("load storage config: %w", err)
	}
	db, err := storage.New(ctx, &storageCfg.Postgres, logger)
	if err != nil {
		return fmt.Errorf("init postgres: %w", err)
	}
	defer db.Close()
	redis, err := storage.NewRedis(ctx, &storageCfg.Redis, logger)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	defer func() { _ = redis.Close() }()

	store := koraevents.NewPostgres(db.Pool())
	if err := store.EnsureRelayState(ctx); err != nil {
		return err
	}

	// Every replica relays, fans out and sweeps. Advisory locks serialize the
	// relay and the sweep across replicas.
	listener := koraevents.NewListener(db.Pool(), logger)
	go listener.Run(ctx)
	go koraevents.RunRelay(ctx, store, listener, logger)
	hub := koraevents.NewHub(store, logger)
	if err := hub.Run(ctx, listener); err != nil {
		return fmt.Errorf("start event hub: %w", err)
	}
	tenants := declarative.NewRegistry(ec.Current.Kubernetes.TenantsPath)
	go koraevents.NewRetention(store, tenants, ec.Current.Retention.DefaultDays, logger).Run(ctx)

	eval := policy.NewEvaluator(ec.Current.Policy.BundleEndpoint, ec.Current.BundlePollInterval(), logger)
	eval.Start(ctx)
	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	blocklist := storage.NewJTIBlocklist(redis)
	auth := mw.MiddlewareChain(
		mw.AuthMiddlewareWithRevocation(verifier, blocklist, logger),
		mw.DelegationMiddlewareWithBlocklist(logger, verifier, blocklist),
		mw.ScopeEnforcementMiddleware(eval, koraevents.Routes(), "", logger),
	)

	apiServer := api.NewAPI(ctx, logger)
	apiServer.SetMetricsMiddleware(metrics.Middleware())
	server := apiServer.Server(ctx, ec.Current.Server.Port, ec.Current.Server.BasePath,
		mw.MiddlewareChain(mw.TracingMiddleware(), mw.LoggingMiddleware(logger)),
		koraevents.NewController(store, hub, auth, blocklist, logger),
		koraevents.NewInternalController(store, hub, mw.AuthMiddlewareWithRevocation(verifier, blocklist, logger), blocklist, logger),
	)

	// Publish events:read so the policy service accepts grants of it and the
	// edge enforces it, and keep it published across policy-service restarts.
	identity := identityclient.NewClient(ec.Current.Identity.InternalURL, logger)
	scopes := routescopes.NewClient(ec.Current.Policy.PolicyServiceURL, identity.SelfTokenSource(), logger)
	go routescopes.KeepRegistered(ctx, logger, "kora-events", func(ctx context.Context) error {
		return scopes.Register(ctx, "kora-events", koraevents.Routes(), koraevents.Scopes())
	})

	serverErr := make(chan error, 1)
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()
	metrics.Start(ctx, logger)

	select {
	case err := <-serverErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	logger.Info(ctx, "Shutting down kora-events")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ec.Current.ShutdownTimeout())
	defer cancel()
	// Open streams never finish on their own, so Shutdown waits out the timeout.
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
	}
	metrics.StopServer(shutdownCtx, logger)
	return nil
}
