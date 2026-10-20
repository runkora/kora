package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel/metric"

	igc "github.com/runkora/kora/backend/cmd/inference-gateway/config"
	"github.com/runkora/kora/backend/internal/inferencegw"
	"github.com/runkora/kora/backend/internal/inferencegw/providers/registry"
	"github.com/runkora/kora/backend/internal/platform/api"
	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
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
	kubeconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	ctx := context.Background()

	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObservability()

	logger := initLogger()
	logger.Info(ctx, "Inference gateway starting", logging.Field{Key: "version", Value: igc.Current.App.Version})

	// fatal flushes observability (the deferred shutdown above won't run
	// after os.Exit) and exits. Used for every startup failure past this
	// point so each one isn't a repeated three-line block.
	fatal := func(msg string, err error) {
		logger.Error(ctx, msg, logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1)
	}

	metrics, err := platformmetrics.Setup(ctx, igc.Current.Metrics, observability.Config{
		ServiceName:    igc.Current.Observability.ServiceName,
		ServiceVersion: igc.Current.App.Version,
		Environment:    config.Environment(),
	}, igc.Current.Observability.ServiceName, logger)
	if err != nil {
		fatal("Failed to setup metrics", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), igc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		fatal("Failed to initialize storage", err)
	}
	defer db.Close()
	defer redis.Close()

	server, register, domainMetrics, err := initServer(ctx, logger, db, redis, metrics)
	if err != nil {
		fatal("Failed to initialize server", err)
	}
	// Close is nil-safe (see (*inferencegw.Metrics).Close), so this runs
	// unconditionally — mirroring shutdownMeter above.
	defer func() {
		if err := domainMetrics.Close(); err != nil {
			logger.Error(ctx, "Error shutting down inference-gateway metrics", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	run(ctx, logger, server, metrics, register)
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     igc.Current.Observability.ServiceName,
		ServiceVersion:  igc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: igc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), igc.Current.ShutdownTimeout())
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

// initStorage initializes PostgreSQL and Redis.
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

func initControllers(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient, meter metric.Meter) ([]api.Controller, func(context.Context) error, *inferencegw.Metrics, error) {
	// ── Provider connectors ───────────────────────────────────────────────────────────────────────────────
	// Providers come from the InferenceProvider resources the gateway watches.
	// The first load seeds the router; the watch keeps it current.
	restCfg, err := kubeconfig.GetConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("kubernetes config: %w", err)
	}
	providers, err := registry.NewWatcher(ctx, restCfg, igc.Current.Providers.Namespace, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	// Prices a compatible upstream publishes with its models are stored on each discovery.
	pricingCatalogRepo := inferencegw.NewPostgresPricingCatalogRepository(db.Pool())
	pricingCache := inferencegw.NewDatabasePricingCache(pricingCatalogRepo, igc.Current.Pricing.RefreshInterval(), logger)
	go pricingCache.Start(ctx)
	prices := inferencegw.NewDiscoveredPrices(pricingCatalogRepo, pricingCache, logger)

	conns, infos := providers.Load(ctx)
	router := inferencegw.NewRoutingEngine(conns, infos, prices.Record)
	go func() {
		if _, discoverErr := router.ListModels(ctx); discoverErr != nil {
			logger.Warn(ctx, "provider model discovery failed", logging.Field{Key: "error", Value: discoverErr.Error()})
		}
	}()
	go func() {
		if watchErr := providers.Run(ctx, router); watchErr != nil {
			logger.Error(ctx, "provider watch stopped", logging.Field{Key: "error", Value: watchErr.Error()})
		}
	}()

	// ── Domain layer ────────────────────────────────────────────────────────────────────────────────
	pol := policy.NewEvaluator(igc.Current.Policy.BundleEndpoint, igc.Current.Policy.BundlePollInterval(), logger)
	pol.Start(ctx)

	usageRepo := inferencegw.NewPostgresInferenceUsageRepository(db.Pool())
	catalogRepo := inferencegw.NewPostgresCatalogRepository(db.Pool())
	catalog := inferencegw.NewCatalogStore(catalogRepo, router)

	// Load the virtual model catalog into the routing engine before accepting
	// traffic. A failure here is non-fatal — the gateway will start with an
	// empty catalog and serve errors until virtual models are configured.
	count, err := catalog.Load(ctx)
	if err != nil {
		logger.Warn(ctx, "virtual model catalog load failed — no models available until catalog is populated",
			logging.Field{Key: "error", Value: err.Error()})
	} else {
		logger.Info(ctx, "virtual model catalog loaded", logging.Field{Key: "models", Value: count})
	}

	// Wire scope registration: once now (after catalog load), and again on
	// every LoadVirtualCatalog call triggered by admin catalog writes.
	selfToken := identityclient.NewClient(igc.Current.Identity.InternalURL, logger).SelfTokenSource()
	register := initScopeRegistration(logger, selfToken, router)

	pricingSyncWorker := inferencegw.NewPricingSyncWorker(
		inferencegw.NewLiteLLMPricingSource(igc.Current.Pricing.SourceURL),
		pricingCatalogRepo,
		igc.Current.Pricing.SyncInterval(),
		logger,
	)
	go pricingSyncWorker.Start(ctx)

	budgetCounter := inferencegw.NewRedisBudgetCounter(redis, usageRepo, logger)
	// Failing to build these is not fatal — see platformmetrics.RegisterDomain.
	domainMetrics := platformmetrics.RegisterDomain(ctx, logger, "inference-gateway", func() (*inferencegw.Metrics, error) {
		return inferencegw.NewMetrics(meter)
	})
	svc := inferencegw.NewService(pol, router, pricingCache, usageRepo, budgetCounter, logger, domainMetrics)

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	// Shared revoked-JTI blocklist: rejects revoked delegation tokens at the
	// PEP before natural expiry (docs/adr/2026-07-23-delegated-agent-identity.md).
	jtiBlocklist := storage.NewJTIBlocklist(redis)
	controllers := []api.Controller{
		inferencegw.NewController(ctx, svc, pol, inferenceRoutes(), igc.Current.Server.BasePath, jtiBlocklist,
			agentsclient.NewClient(igc.Current.Agents.InternalURL, logger), logger),
		inferencegw.NewCatalogController(catalog, router, pricingCatalogRepo, verifier, pol, inferenceRoutes(), igc.Current.Server.BasePath, logger),
	}
	return controllers, register, domainMetrics, nil
}

// initScopeRegistration wires the policy-service scope vocabulary for the
// inference gateway. It installs a hook on the router so the vocabulary is
// re-synced whenever GET /v1/models refreshes the catalog, and returns the
// register function; the caller hands it to routescopes.KeepRegistered, which
// owns the initial registration and the periodic self-heal loop.
func initScopeRegistration(logger logging.Logger, selfToken routescopes.TokenSource, router *inferencegw.RoutingEngine) func(context.Context) error {
	client := routescopes.NewClient(igc.Current.Policy.PolicyServiceURL, selfToken, logger)

	register := func(registerCtx context.Context) error {
		catalogScopes := router.CatalogScopes()
		scopeDecls := inferenceScopes(catalogScopes)
		if err := client.Register(registerCtx, "inference-gateway", inferenceRoutes(), scopeDecls); err != nil {
			return fmt.Errorf("register routes: %w", err)
		}
		return nil
	}

	// The hook fires whenever a catalog load adds or removes a model, so the
	// per-model inference:<provider>:<model>:use scopes change and have to be
	// republished without waiting for the next tick. It re-reads the catalog
	// through register rather than using the CatalogScope argument: the router
	// stores the snapshot before firing, so the two are the same value.
	republish := routescopes.RepublishOnEvent(logger, "inference-gateway", register)
	router.SetCatalogUpdateHook(func([]inferencegw.CatalogScope) { republish() })

	return register
}

// inferenceRoutes returns the static route->scope bindings for the inference gateway.
//
// Used at startup for policy-service route registration (docs/adr/2026-04-14-dynamic-route-scope-registration.md) and at
// request time by ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Entries with empty
// RequiredScope require authentication but no specific scope; the middleware
// passes them through after AuthMiddleware. The policy service registers them
// as authenticated-only routes.
//
// Order does not matter: the longest matching prefix wins, in
// ScopeEnforcementMiddleware and in the rego.
func inferenceRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/embeddings", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/responses", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/systemone", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"},
		{PathPrefix: "/v1/catalog", Method: "GET", RequiredScope: "platform:catalog:read"},
		{PathPrefix: "/v1/catalog", Method: "POST", RequiredScope: "platform:catalog:write"},
		{PathPrefix: "/v1/catalog", Method: "PUT", RequiredScope: "platform:catalog:write"},
		{PathPrefix: "/v1/catalog", Method: "DELETE", RequiredScope: "platform:catalog:write"},
		// GET /v1/analytics/me is personal — any authenticated caller may read
		// their own analytics. Listed before /v1/analytics so the more-specific
		// path wins for the analytics/me request.
		{PathPrefix: "/v1/analytics/me", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"},
		// The /v1/usage tree splits into personal and privileged reads, so each
		// path is listed explicitly rather than inheriting one prefix entry.
		// Personal: the response is derived from the caller's own claims and
		// cannot be widened by a query parameter, so authentication is enough.
		// Privileged: the response aggregates spend across the whole tenant, so
		// it requires platform:analytics:read, the same scope guarding the
		// tenant-wide /v1/analytics reads above.
		//
		// GET /v1/usage/budget is personal — any authenticated caller may check
		// their own budget. Listed before /v1/usage so the more-specific path wins.
		{PathPrefix: "/v1/usage/budget", Method: "GET", RequiredScope: ""},
		// GET /v1/usage/run/{id} is the run chat usage meter. The handler serves
		// it only when orbit-agents would let the caller read the run.
		{PathPrefix: "/v1/usage/run", Method: "GET", RequiredScope: ""},
		// GET /v1/usage/stats is personal — per-PAT aggregates for the calling
		// principal only.
		{PathPrefix: "/v1/usage/stats", Method: "GET", RequiredScope: ""},
		// Agent usage is authenticated-only at the route layer. The governing scope is
		// per-agent (agents:usage:<slug>:read) and Service.ReadableAgents narrows the
		// response to the agents the caller may read. agents:usage:read covers every agent.
		{PathPrefix: "/v1/usage/agent-insights", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/v1/usage/agent-type", Method: "GET", RequiredScope: ""},
		// GET /v1/usage is the caller's own inference usage log; tenant and
		// principal come from the verified claims.
		{PathPrefix: "/v1/usage", Method: "GET", RequiredScope: ""},
		// GET /v1/budgets/principals is the tenant-wide budget status listing.
		// The platform:budgets:* scopes are declared by the policy-service.
		{PathPrefix: "/v1/budgets", Method: "GET", RequiredScope: "platform:budgets:read"},
	}
}

// inferenceScopes builds the full scope declaration list from the current catalog.
// Static top-level scopes are always included; per-family and per-model scopes
// are derived from the live catalog so they stay current as providers change.
func inferenceScopes(catalogScopes []inferencegw.CatalogScope) []routescopes.ScopeDeclaration {
	decls := []routescopes.ScopeDeclaration{
		{Scope: "inference:invoke", ResourceType: "inference", PathSegments: []string{}, Action: "invoke", Description: "Call the inference endpoints"},
		{Scope: "inference:use", ResourceType: "inference", PathSegments: []string{}, Action: "use", Description: "Use any model across all providers"},
		{Scope: "inference:list", ResourceType: "inference", PathSegments: []string{}, Action: "list", Description: "List the models you can use"},
		{Scope: "platform:catalog:read", ResourceType: "platform", PathSegments: []string{"catalog"}, Action: "read", Description: "Read the virtual model catalog and provider model settings"},
		{Scope: "platform:catalog:write", ResourceType: "platform", PathSegments: []string{"catalog"}, Action: "write", Description: "Manage the virtual model catalog and provider model settings"},
		{Scope: "platform:analytics:read", ResourceType: "platform", PathSegments: []string{"analytics"}, Action: "read", Description: "Read tenant-wide inference analytics"},
	}
	for _, cs := range catalogScopes {
		decls = append(decls, routescopes.ScopeDeclaration{
			Scope:        cs.Scope,
			ResourceType: "inference",
			PathSegments: cs.PathSegments,
			Action:       "use",
			Description:  cs.Description,
		})
	}
	return decls
}

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient, metrics *platformmetrics.Bootstrap) (*http.Server, func(context.Context) error, *inferencegw.Metrics, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, register, domainMetrics, err := initControllers(ctx, logger, db, redis, metrics.Meter())
	if err != nil {
		return nil, nil, nil, err
	}

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	server := apiServer.Server(
		ctx,
		igc.Current.Server.Port,
		igc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(igc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
			// Rewrites /run/{run_id}/v1/... -> /v1/... with the run identity in
			// context, ahead of all routing and enforcement. See
			// docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
			mw.NewMiddleware(mw.StripRunPrefix),
		),
		controllers...,
	)

	return server, register, domainMetrics, nil
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, metrics *platformmetrics.Bootstrap, register func(context.Context) error) {
	runCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info(runCtx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	metrics.Start(runCtx, logger)

	// Register now and keep the registration alive across policy-service
	// restarts; the shared helper owns the retry and re-registration cadence.
	go routescopes.KeepRegistered(runCtx, logger, "inference-gateway", register)

	<-runCtx.Done()
	logger.Info(ctx, "Shutting down inference gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), igc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Inference gateway shutdown complete")
}
