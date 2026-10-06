package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ic "github.com/clear-route/orbit/cmd/identity/config"
	"github.com/clear-route/orbit/internal/identity"
	"github.com/clear-route/orbit/internal/platform/api"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"go.opentelemetry.io/otel/metric"
)

func main() {
	if err := realMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// realMain contains the full startup sequence. Returning an error from here
// ensures all deferred cleanups run before os.Exit is called in main.
func realMain() error {
	// Use a signal-cancellable context so background goroutines (e.g. the
	// tenant registry file watcher) stop cleanly on SIGTERM / SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize observability with deferred cleanup.
	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer shutdownObservability()

	// Create logger.
	logger := initLogger()
	logger.Info(ctx, "Identity service starting", logging.Field{Key: "version", Value: ic.Current.App.Version})

	// Initialize the metrics pipeline (OTLP, if configured, and/or a
	// Prometheus /metrics endpoint, if enabled) with deferred cleanup.
	meter, metricsSrv, shutdownMeter, err := initMeter(ctx, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer shutdownMeter()

	// Trusted-host allowlist for host-derived OAuth discovery metadata (ADR 2026-07-04).
	api.ConfigureTrustedHosts(ic.Current.PublicHosts)

	// Initialize storage backends (Postgres + Redis) from a single config load.
	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer db.Close()
	defer redis.Close() //nolint:errcheck

	// Create HTTP server with wired controllers.
	server, err := initServer(ctx, logger, db, redis, meter)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	// Run server until context is canceled (SIGTERM / SIGINT).
	run(ctx, logger, server, metricsSrv)
	return nil
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
			fmt.Fprintf(os.Stderr, "Error shutting down observability: %v\n", err)
		}
	}, nil
}

// initMeter sets up the OTel metrics pipeline — an OTLP reader when a
// collector is configured, and/or a Prometheus reader when metrics are
// enabled — and returns the meter instruments are built on, this service's
// dedicated metrics *http.Server (nil when disabled), and a cleanup func.
func initMeter(ctx context.Context, logger logging.Logger) (metric.Meter, *http.Server, func(), error) {
	promExporter, err := platformmetrics.New(ic.Current.Metrics)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	meter, shutdownOtelMetrics, err := observability.SetupMetrics(ctx, observability.Config{
		ServiceName:    ic.Current.Observability.ServiceName,
		ServiceVersion: ic.Current.App.Version,
		Environment:    config.Environment(),
	}, promExporter.Reader())
	if err != nil {
		return nil, nil, nil, err
	}

	metricsSrv := promExporter.Server(logger)
	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ic.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdownOtelMetrics(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down metrics: %v\n", err)
		}
	}
	return meter, metricsSrv, cleanup, nil
}

// initLogger creates the appropriate logger based on environment.
func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
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
	blocklist := identity.NewRedisJTIBlocklistRepository(redis)
	authCodeRepo := identity.NewRedisAuthCodeRepository(redis.Client())
	clientRepo := identity.NewRedisClientRepository(redis.Client())
	patRepo := identity.NewPostgresPATRepository(db.Pool())
	profileRepo := identity.NewPostgresUserProfileRepository(db.Pool())
	prefsRepo := identity.NewPostgresUserPreferencesRepository(db.Pool())
	delegationRepo := identity.NewPostgresDelegationRepository(db.Pool())

	// Policy service integration for PAT grant management and scope validation.
	policySvcURL := ic.Current.PolicyServiceURL

	// The policy client needs a token func, but the token func needs the identity
	// service (to call SystemToken). Break the circular dependency with a lazy
	// closure that is only invoked at request time, after svc is constructed.
	var svc *identity.Service
	policyClient := policyclient.NewClient(policySvcURL, func(ctx context.Context) (string, error) {
		return svc.SystemToken(ctx)
	}, logger)

	bundleEndpoint := policySvcURL + "/api/v1/policy/bundles"
	policyEval := policy.NewEvaluator(bundleEndpoint, 30*time.Second, logger)
	policyEval.Start(ctx)

	// XFCCPrincipalExtractor reads the Envoy-injected x-forwarded-client-cert
	// header after mTLS termination. In local development, callers simply set
	// the header directly — no stub or special dev path needed
	// (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md, docs/adr/2026-03-05-universal-gateway-policy-enforcement.md, docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md).
	extractor := &identity.XFCCPrincipalExtractor{}

	svc, err = identity.NewService(
		ctx,
		identityCfg,
		sessionRepo,
		refreshRepo,
		blocklist,
		authCodeRepo,
		clientRepo,
		patRepo,
		profileRepo,
		prefsRepo,
		delegationRepo,
		policyClient,
		policyEval,
		extractor,
		logger,
	)
	if err != nil {
		return nil, err
	}

	// Triggers moved to the orbit-triggers binary: inbound webhook ingestion is
	// the platform's only unauthenticated, internet-reachable surface and must
	// not share a process with the token authority. identity still mints each
	// trigger's SERVICE token, now over the mesh via the internal service-token
	// endpoint. See docs/adr/2026-06-20-extended-trigger-types.md, Decision 1.
	return []api.Controller{
		identity.NewController(svc, policyEval, identityRoutes(), ic.Current.Server.BasePath, logger),
	}, nil
}

// identityRoutes returns the static route → required-scope bindings for the
// identity service. The map covers ONLY the authenticated routes (PAT
// management, user profile resolution) — the unauthenticated routes (device,
// session, exchange, JWKS, tenant discovery) are registered directly with
// mux.Handle in handlers.go Boot and never reach ScopeEnforcementMiddleware.
//
// Today every entry is RequiredScope: "" — meaning "any authenticated non-PAT
// caller is allowed". This preserves current behavior. Follow-up PRs can
// tighten scopes (e.g. platform:users:read for admin-only user listing,
// platform:pats:write for cross-user PAT management).
func identityRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/identity/tokens", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/tokens", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/tokens", Method: "PATCH", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/tokens", Method: "DELETE", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/users", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/preferences", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/preferences", Method: "PUT", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/preferences", Method: "DELETE", RequiredScope: ""},
		// Delegation issuer (docs/adr/2026-07-23-delegated-agent-identity.md).
		// RequiredScope "" = any authenticated caller; the service layer
		// enforces the HUMAN/PAT mint constraint and revoke authorization.
		// GET /identity/delegation/run/{run_id} is intentionally absent — it is
		// XFCC-authenticated and registered with mux.Handle, never reaching the
		// scope middleware (same treatment as /identity/exchange).
		{PathPrefix: "/api/v1/identity/delegation", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/delegation", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/identity/delegation", Method: "DELETE", RequiredScope: ""},
	}
}

// initServer creates and configures the HTTP server with middleware and controllers.
func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient, meter metric.Meter) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, db, redis)
	if err != nil {
		return nil, err
	}

	metricsMW, err := mw.MetricsMiddleware(meter, ic.Current.Observability.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("create metrics middleware: %w", err)
	}

	// Middleware order: Tracing (provides context) → Metrics → Logging (uses trace context).
	server := apiServer.Server(
		ctx,
		ic.Current.Server.Port,
		ic.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(ic.Current.Observability.ServiceName),
			metricsMW,
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)

	return server, nil
}

// run starts the HTTP server and handles graceful shutdown.
// ctx is expected to be the signal-cancellable context created in main —
// cancellation drives shutdown of both the HTTP server and background
// goroutines (tenant registry watcher, etc.). metricsSrv is nil when the
// Prometheus endpoint is disabled.
func run(ctx context.Context, logger logging.Logger, server, metricsSrv *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	if metricsSrv != nil {
		go func() {
			logger.Info(ctx, "Starting metrics server", logging.Field{Key: "addr", Value: metricsSrv.Addr})
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error(ctx, "Failed to start metrics server", logging.Field{Key: "error", Value: err.Error()})
			}
		}()
	}

	// Block until the context is canceled (SIGTERM / SIGINT from
	// signal.NotifyContext in main).
	<-ctx.Done()
	logger.Info(ctx, "Shutting down identity service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ic.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error(ctx, "Metrics server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
		}
	}

	logger.Info(ctx, "Identity service shutdown complete")
}
