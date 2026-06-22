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
	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/triggers"
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

	// Initialize storage backends (Postgres + Redis) from a single config load.
	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer db.Close()
	defer redis.Close() //nolint:errcheck

	// Create HTTP server with wired controllers.
	server, err := initServer(ctx, logger, db, redis)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	// Run server until context is canceled (SIGTERM / SIGINT).
	run(ctx, logger, server)
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
	blocklist := identity.NewRedisJTIBlocklistRepository(redis.Client())
	patRepo := identity.NewPostgresPATRepository(db.Pool())
	profileRepo := identity.NewPostgresUserProfileRepository(db.Pool())

	// Policy service integration for PAT grant management and scope validation.
	policySvcURL := os.Getenv("ORBIT_POLICY_SERVICE_URL")
	if policySvcURL == "" {
		policySvcURL = "http://orbit-policy-service.orbit-system.svc"
	}

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
		patRepo,
		profileRepo,
		policyClient,
		policyEval,
		extractor,
		logger,
	)
	if err != nil {
		return nil, err
	}

	// Triggers feature (docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md).
	// Hosted here because identity is the only externally-reachable binary and the
	// platform token authority — it mints each webhook source's identity in-process.
	// The package is self-contained: it reaches orbit-agents via a typed client and
	// mints SERVICE tokens via the identity service (the TokenMinter).
	triggerCtrl, err := initTriggersController(ctx, logger, db, svc, policyClient, policyEval)
	if err != nil {
		return nil, err
	}

	// Publish the Triggers scope vocabulary (triggers:read / triggers:write) to
	// the policy service so PATs and custom grants can reference them. Unlike the
	// other services, identity has no identity-proxy sidecar, so it registers via
	// the authenticated policy client (its own SystemToken → SYSTEM principal →
	// global registration) rather than the routescopes client. Runs in the
	// background with retries since the policy service / system token may not be
	// ready at startup.
	go registerTriggerScopes(ctx, logger, policyClient)

	return []api.Controller{
		identity.NewController(svc, policyEval, identityRoutes(), ic.Current.Server.BasePath, logger),
		triggerCtrl,
	}, nil
}

// initTriggersController wires the Triggers feature: the Postgres repository, the
// source-adapter registry, the at-rest secret cipher, the orbit-agents client, and
// the controller. The identity service doubles as the TokenMinter and the auth
// middleware provider.
func initTriggersController(
	ctx context.Context,
	logger logging.Logger,
	db *storage.DB,
	svc *identity.Service,
	policyClient *policyclient.Client,
	policyEval policy.Evaluator,
) (api.Controller, error) {
	cipherKey, devFallback, err := triggers.LoadCipherKey()
	if err != nil {
		return nil, fmt.Errorf("load triggers encryption key: %w", err)
	}
	if devFallback {
		logger.Warn(ctx, "ORBIT_TRIGGERS_ENCRYPTION_KEY not set — using insecure local-dev key; set it in production")
	}
	cipher, err := triggers.NewSecretCipherFromKey(cipherKey)
	if err != nil {
		return nil, fmt.Errorf("init triggers cipher: %w", err)
	}

	agentsURL := os.Getenv("ORBIT_AGENTS_URL")
	if agentsURL == "" {
		agentsURL = "http://orbit-agents.orbit-system.svc"
	}

	repo := triggers.NewPostgresRepository(db.Pool())
	agentsCli := agentsclient.NewClient(agentsURL, logger)
	// Inbound webhook path prefix: /api/v1 (server base) + /webhooks/.
	triggerSvc := triggers.NewService(repo, triggers.NewRegistry(), cipher, policyClient, agentsCli, svc, ic.Current.Server.BasePath+"/webhooks/", logger)

	return triggers.NewController(triggerSvc, svc.AuthMiddleware(logger), policyEval, triggerRoutes(), ic.Current.Server.BasePath, logger), nil
}

// triggerRoutes returns the route→required-scope bindings for the Triggers
// management API, consumed by ScopeEnforcementMiddleware. The public inbound
// webhook endpoint is intentionally absent — it is HMAC-authenticated and
// registered with mux.Handle so it never reaches the scope middleware.
func triggerRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/trigger-sources", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "DELETE", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "DELETE", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-deliveries", Method: "GET", RequiredScope: "triggers:read"},
	}
}

// triggerScopes declares the Triggers scope vocabulary so the policy service can
// publish triggers:read / triggers:write into the vocabulary. Without these
// declarations the scopes exist only as references (route bindings and the
// reserved tenant-admin/platform-admin roles), so they cannot be granted
// explicitly — e.g. PAT creation with triggers:read fails the vocabulary check.
func triggerScopes() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{
		{Scope: "triggers:read", ResourceType: "triggers", PathSegments: []string{}, Action: "read", Description: "View trigger sources, triggers, and delivery history"},
		{Scope: "triggers:write", ResourceType: "triggers", PathSegments: []string{}, Action: "write", Description: "Create, edit, and delete trigger sources and triggers"},
	}
}

// registerTriggerScopes publishes triggerRoutes()/triggerScopes() to the policy
// service via the authenticated policy client, retrying with backoff until it
// succeeds or ctx is canceled (the policy service or the system token may not be
// ready at startup).
func registerTriggerScopes(ctx context.Context, logger logging.Logger, pc *policyclient.Client) {
	const maxAttempts = 15
	maxBackoff := 30 * time.Second
	backoff := 2 * time.Second
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := pc.RegisterRoutes(ctx, "identity", triggerRoutes(), triggerScopes()); err == nil {
			logger.Info(ctx, "triggers scope vocabulary registered with policy service")
			return
		} else {
			lastErr = err
			logger.Warn(ctx, "trigger scope registration attempt failed, retrying",
				logging.Field{Key: "attempt", Value: attempt},
				logging.Field{Key: "error", Value: err.Error()})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	errStr := "unknown"
	if lastErr != nil {
		errStr = lastErr.Error()
	}
	logger.Error(ctx, "trigger scope registration gave up after max attempts",
		logging.Field{Key: "attempts", Value: maxAttempts},
		logging.Field{Key: "last_error", Value: errStr})
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
	}
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
// ctx is expected to be the signal-cancellable context created in main —
// cancellation drives shutdown of both the HTTP server and background
// goroutines (tenant registry watcher, etc.).
func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	// Block until the context is canceled (SIGTERM / SIGINT from
	// signal.NotifyContext in main).
	<-ctx.Done()
	logger.Info(ctx, "Shutting down identity service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ic.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Identity service shutdown complete")
}
