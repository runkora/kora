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

	db, err := initStorage(ctx, logger)
	if err != nil {
		logger.Error(ctx, "Failed to initialize storage", logging.Field{Key: "error", Value: err})
		shutdownObservability()
		os.Exit(1) //nolint:gocritic
	}
	defer db.Close()

	server, err := initServer(ctx, logger, db)
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

// initStorage initializes only PostgreSQL — the inference gateway does not use Redis.
func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
	cfg, err := storage.LoadStorageConfig()
	if err != nil {
		return nil, err
	}

	db, err := storage.New(ctx, &cfg.Postgres, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(ctx, "Database initialized successfully")

	return db, nil
}

func initControllers(ctx context.Context, logger logging.Logger, db *storage.DB) ([]api.Controller, error) {
	// ── Provider connectors ───────────────────────────────────────────────────
	// Register factories for every supported provider. Each factory reads its
	// own env vars via LoadConfig — a nil result means the provider is disabled.
	// Adding a new provider is a single line here.
	factories := []inferencegw.ConnectorFactory{
		bedrock.Factory{},
		vertexai.Factory{},
		azure.Factory{},
	}

	connectors := map[inferencegw.ProviderName]inferencegw.ProviderConnector{}
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

	svc := inferencegw.NewService(pol, router, inferencegw.NewNopPricingCache(), usageRepo, logger)

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	return []api.Controller{
		inferencegw.NewController(ctx, svc, logger),
		inferencegw.NewCatalogController(catalog, verifier, logger),
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
		if err := client.Register(ctx, "inference-gateway", inferenceRoutes(), scopeDecls); err != nil {
			logger.Error(ctx, "route registration failed", logging.Field{Key: "error", Value: err.Error()})
		}
	}

	router.SetCatalogUpdateHook(register)
	go register(router.CatalogScopes())
}

// inferenceRoutes returns the static route→scope bindings for the inference gateway.
func inferenceRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/v1/chat/completions", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
		{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"},
		{PathPrefix: "/v1/catalog", Method: "GET", RequiredScope: "platform:catalog:read"},
		{PathPrefix: "/v1/catalog", Method: "POST", RequiredScope: "platform:catalog:write"},
		{PathPrefix: "/v1/catalog", Method: "PUT", RequiredScope: "platform:catalog:write"},
		{PathPrefix: "/v1/catalog", Method: "DELETE", RequiredScope: "platform:catalog:write"},
		// GET /v1/analytics is tenant-wide and scope-gated.
		// GET /v1/analytics/me is personal and omitted here — open to any authenticated caller.
		{PathPrefix: "/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"},
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

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB) (*http.Server, error) {
	apiServer := api.NewAPI(ctx, logger)

	controllers, err := initControllers(ctx, logger, db)
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
