package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	igc "github.com/clear-route/orbit/cmd/inference-gateway/config"
	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/azure"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bedrock"
	"github.com/clear-route/orbit/internal/inferencegw/providers/vertexai"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/platform/token"
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

	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic
	}
	defer db.Close()
	defer redis.Close()

	server, err := initServer(ctx, logger, db, redis)
	if err != nil {
		logger.Error(ctx, "Failed to initialize server", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1)
	}

	run(ctx, logger, server)
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

func initControllers(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient) ([]api.Controller, error) {
	// ── Provider connectors ───────────────────────────────────────────────────
	// Register factories for every supported provider. Each factory reads its
	// own env vars via LoadConfig — a nil result means the provider is disabled.
	// Adding a new provider is a single line here.
	//
	// Bedrock is special: when BEDROCK_ACCOUNTS is set, MultiFactory registers
	// one connector per named account (bedrock:<alias>). When BEDROCK_ACCOUNTS
	// is unset, the standard Factory single-account path is used instead.
	factories := []inferencegw.ConnectorFactory{
		vertexai.Factory{},
		azure.Factory{},
	}

	connectors := map[inferencegw.ProviderName]inferencegw.ProviderConnector{}

	// Multi-account Bedrock: attempt multi-factory first.
	bedrockMulti, errMulti := bedrock.MultiFactory{}.Build(logger)
	if errMulti != nil {
		return nil, fmt.Errorf("bedrock multi-account: %w", errMulti)
	}
	if len(bedrockMulti) > 0 {
		for _, nc := range bedrockMulti {
			connectors[nc.Name] = nc.Connector
			logger.Info(ctx, "provider enabled", logging.Field{Key: "provider", Value: string(nc.Name)})
		}
	} else {
		// Single-account Bedrock fallback.
		c, err := bedrock.Factory{}.Build(logger)
		if err != nil {
			return nil, fmt.Errorf("provider bedrock: %w", err)
		}
		if c != nil {
			connectors[c.Name()] = c
			logger.Info(ctx, "provider enabled", logging.Field{Key: "provider", Value: string(c.Name())})
		}
	}

	for _, f := range factories {
		c, err := f.Build(logger)
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", f.Name(), err)
		}
		if c != nil {
			connectors[f.Name()] = c
			logger.Info(ctx, "provider enabled", logging.Field{Key: "provider", Value: string(f.Name())})
		}
	}

	if len(connectors) == 0 {
		logger.Warn(ctx, "warning: no providers enabled — all inference requests will fail")
	}

	// ── Domain layer ──────────────────────────────────────────────────────────
	router := inferencegw.NewRoutingEngine(connectors)
	pol := policy.NewEvaluator(igc.Current.Policy.BundleEndpoint, igc.Current.Policy.BundlePollInterval(), logger)
	pol.Start(ctx)

	usageRepo := inferencegw.NewPostgresInferenceUsageRepository(db.Pool())
	catalogRepo := inferencegw.NewPostgresCatalogRepository(db.Pool())
	pricingCatalogRepo := inferencegw.NewPostgresPricingCatalogRepository(db.Pool())
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

	// Warm up the provider model discovery cache in the background. A failure is
	// non-fatal — the cache will be empty until POST /catalog/provider-models/refresh
	// is called or the gateway restarts.
	go func() {
		if err := catalog.WarmupProviderModels(ctx); err != nil {
			logger.Warn(ctx, "provider model warm-up failed — provider catalog will be empty until refreshed",
				logging.Field{Key: "error", Value: err.Error()})
		} else {
			logger.Info(ctx, "provider model cache warmed up")
		}
	}()

	// Wire scope registration: once now (after catalog load), and again on
	// every LoadVirtualCatalog call triggered by admin catalog writes.
	initScopeRegistration(ctx, logger, router)

	pricingCache := inferencegw.NewDatabasePricingCache(pricingCatalogRepo, igc.Current.Pricing.RefreshInterval(), logger)
	go pricingCache.Start(ctx)

	pricingSyncWorker := inferencegw.NewPricingSyncWorker(
		inferencegw.NewLiteLLMPricingSource(igc.Current.Pricing.SourceURL),
		pricingCatalogRepo,
		igc.Current.Pricing.SyncInterval(),
		logger,
	)
	go pricingSyncWorker.Start(ctx)

	budgetCounter := inferencegw.NewRedisBudgetCounter(redis, usageRepo, logger)
	svc := inferencegw.NewService(pol, router, pricingCache, usageRepo, budgetCounter, logger)

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	return []api.Controller{
		inferencegw.NewController(ctx, svc, pol, inferenceRoutes(), igc.Current.Server.BasePath, logger),
		inferencegw.NewCatalogController(catalog, pricingCatalogRepo, verifier, pol, inferenceRoutes(), igc.Current.Server.BasePath, logger),
	}, nil
}

// initScopeRegistration wires the policy-service scope vocabulary for the
// inference gateway. It registers once immediately (in a background goroutine,
// since the routescopes client retries until the identity-proxy sidecar is
// ready) and installs a hook on the router so the vocabulary is re-synced
// whenever GET /v1/models refreshes the catalog.
func initScopeRegistration(ctx context.Context, logger logging.Logger, router *inferencegw.RoutingEngine) {
	client := routescopes.NewClient(igc.Current.Policy.PolicyServiceURL, logger)

	register := func(catalogScopes []inferencegw.CatalogScope) {
		scopeDecls := inferenceScopes(catalogScopes)
		// Drop routes whose RequiredScope is empty — those are
		// "authenticated-only" and enforced locally by ScopeEnforcementMiddleware,
		// not by OPA. Registering them with empty scopes would deny everyone
		// when Layer 1 sidecar enforcement is enabled in the future.
		scopedRoutes := filterScopedRoutes(inferenceRoutes())
		if err := client.Register(ctx, "inference-gateway", scopedRoutes, scopeDecls); err != nil {
			logger.Error(ctx, "route registration failed", logging.Field{Key: "error", Value: err.Error()})
		}
	}

	router.SetCatalogUpdateHook(register)
	go register(router.CatalogScopes())
}

// filterScopedRoutes returns the subset of routes that require a non-empty
// scope. Used to exclude "authenticated-only" routes from policy-service
// registration; those are enforced by the local ScopeEnforcementMiddleware.
func filterScopedRoutes(in []routescopes.RouteScope) []routescopes.RouteScope {
	out := make([]routescopes.RouteScope, 0, len(in))
	for _, r := range in {
		if r.RequiredScope != "" {
			out = append(out, r)
		}
	}
	return out
}

// inferenceRoutes returns the static route→scope bindings for the inference gateway.
//
// Used at startup for policy-service route registration (docs/adr/2026-04-14-dynamic-route-scope-registration.md) and at
// request time by ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Entries with empty
// RequiredScope require authentication but no specific scope; the middleware
// passes them through after AuthMiddleware. They are filtered out by the
// policy registration in registerRoutes.
//
// Order matters: ScopeEnforcementMiddleware iterates this slice and prefers
// the more-specific (sub-path) entry over the broader prefix for the same
// method, so list narrower paths before broader ones.
func inferenceRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/embeddings", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/responses", Method: "POST", RequiredScope: "inference:invoke"},
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
		// GET /v1/usage/budget is personal — any authenticated caller may check
		// their own budget. Listed before /v1/usage so the more-specific path wins.
		{PathPrefix: "/v1/usage/budget", Method: "GET", RequiredScope: ""},
		// GET /v1/usage/run/{id} is the run chat usage meter — any authenticated
		// caller in the tenant; spend is tenant-scoped in the query. More-specific
		// than /v1/usage, so listed before it.
		{PathPrefix: "/v1/usage/run", Method: "GET", RequiredScope: ""},
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
		{Scope: "inference:list", ResourceType: "inference", PathSegments: []string{}, Action: "list", Description: "List available models"},
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

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, redis *storage.RedisClient) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, db, redis)
	if err != nil {
		return nil, err
	}

	server := apiServer.Server(
		ctx,
		igc.Current.Server.Port,
		igc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(igc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
			// Rewrites /run/{run_id}/v1/… → /v1/… with the run identity in
			// context, ahead of all routing and enforcement. See
			// docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
			mw.NewMiddleware(inferencegw.StripRunPrefix),
		),
		controllers...,
	)

	return server, nil
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
	logger.Info(ctx, "Shutting down inference gateway...")

	shutdownCtx, cancel := context.WithTimeout(ctx, igc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Inference gateway shutdown complete")
}
