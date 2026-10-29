package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	psc "github.com/runkora/kora/backend/cmd/policy-service/config"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
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
	"github.com/runkora/kora/backend/internal/policyservice"
	"github.com/runkora/kora/backend/internal/policyservice/api"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/principalroles"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/tenantbudget"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
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

	shutdownObservability, err := initObservability(ctx)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer shutdownObservability()

	logger := initLogger()
	logger.Info(ctx, "Policy service starting", logging.Field{Key: "version", Value: psc.Current.App.Version})

	metrics, err := platformmetrics.Setup(ctx, psc.Current.Metrics, observability.Config{
		ServiceName:    psc.Current.Observability.ServiceName,
		ServiceVersion: psc.Current.App.Version,
		Environment:    config.Environment(),
	}, psc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	storageCfg, err := storage.LoadStorageConfig()
	if err != nil {
		return fmt.Errorf("load storage config: %w", err)
	}

	db, err := storage.New(ctx, &storageCfg.Postgres, logger)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	grantRepo := grants.NewPostgresRepository(db.Pool())
	vocabRepo := vocabulary.NewPostgresRepository(db.Pool())
	roleRepo := roles.NewPostgresRepository(db.Pool())
	tenantBudgetRepo := tenantbudget.NewPostgresRepository(db.Pool())
	principalRolesRepo := principalroles.NewPostgresRepository(db.Pool())
	routeStore := routes.NewStore()

	bundleCfg := bundle.Config{
		JWKSURL:            psc.Current.Bundle.JWKSURL,
		Issuer:             psc.Current.Bundle.Issuer,
		Audience:           psc.Current.Bundle.Audience,
		FrontendHost:       psc.Current.Bundle.FrontendHost,
		TelemetryCollector: observability.CollectorAuthority(),
	}
	compiler := bundle.NewCompiler(bundleCfg, routeStore)
	tenantRegistry := declarative.NewRegistry(psc.Current.Bundle.TenantsPath)
	bundleSrv := bundle.NewServer(compiler, grantRepo, vocabRepo, roleRepo, tenantBudgetRepo, principalRolesRepo, tenantRegistry, logger)

	// Pre-compile bundles for every known tenant: registered, with grants, or
	// with routes.
	if tenantIDs, bootErr := bundleSrv.KnownTenantIDs(ctx); bootErr == nil {
		bundleSrv.Bootstrap(ctx, tenantIDs)
	} else {
		logger.Error(ctx, "bundle bootstrap: failed to list tenants", logging.Field{Key: "error", Value: bootErr.Error()})
	}

	// Pre-compile the cross-tenant gateway bundle.
	bundleSrv.BootstrapGateway(ctx)

	// The shared revoked-JTI blocklist refuses a revoked delegation before its
	// token expires (docs/adr/2026-07-23-delegated-agent-identity.md).
	redis, err := storage.NewRedis(ctx, &storageCfg.Redis, logger)
	if err != nil {
		return fmt.Errorf("initialize redis: %w", err)
	}
	defer func() { _ = redis.Close() }()

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	svc := api.NewService(grantRepo, vocabRepo, roleRepo, tenantBudgetRepo, bundleSrv, routeStore, logger)

	// Failing to register these gauges is not fatal — a service that refuses
	// to start because a gauge could not be registered has turned its own
	// observability into an outage — but it is loud, because unmeasured is
	// not the state anyone signed up for. RegisterDomain is the shared helper
	// every service's main.go uses for this.
	policyMetrics := platformmetrics.RegisterDomain(ctx, logger, "policy-service", func() (*policyservice.Metrics, error) {
		return policyservice.NewMetrics(metrics.Meter(), svc, logger)
	})
	defer func() {
		if err := policyMetrics.Close(); err != nil {
			logger.Error(ctx, "Error shutting down policy-service metrics", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	// Self-register the policy service's own routes and platform scope
	// vocabulary, and keep re-registering.
	//
	// The call is direct — no HTTP round-trip, since the service owns the route
	// store — so it goes through KeepRegistered rather than the routescopes
	// client, and needs no token. The reason
	// to use the shared helper anyway is its retry: the route store is
	// in-memory and survives as long as the process, but the scope declarations
	// are upserted into Postgres, so a database blip at boot used to leave the
	// platform:* vocabulary unpublished for the life of the process with
	// nothing but one log line to show for it. The periodic tick then costs one
	// in-process call and keeps the recovery behavior uniform across services.
	go routescopes.KeepRegistered(ctx, logger, "policy-service",
		func(ctx context.Context) error {
			return svc.RegisterRoutes(ctx, "policy-service", "", selfRegisteredRoutes(), selfRegisteredScopes())
		})

	// In-process policy evaluator for scope enforcement. The bundle endpoint is
	// served by this same process; the evaluator polls it via loopback HTTP.
	// Slightly wasteful but keeps ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md) uniform
	// across services with no special-cased in-process bundle wiring.
	bundleEndpoint := fmt.Sprintf("http://localhost:%d/api/v1/policy/bundles", psc.Current.Server.Port)
	policyEval := policy.NewEvaluator(bundleEndpoint, 30*time.Second, logger)
	policyEval.Start(ctx)

	// Declared tenant budgets follow the tenant registry (docs/adr/2026-10-24-declarative-resources.md).
	go svc.RunTenantBudgets(ctx, tenantRegistry)

	controller := api.NewController(svc, logger, verifier, storage.NewJTIBlocklist(redis), policyEval, policyServiceRoutes(), psc.Current.Server.BasePath, tenantRegistry)
	// The operator applies PolicyRole resources through /api/v1/internal/declared/policyroles.
	declaredController := declarative.NewController(verifier, logger, map[string]declarative.Applier{
		declarative.KindPolicyRoles: api.NewRoleApplier(svc),
	})
	server := initServer(ctx, logger, metrics, controller, declaredController)

	run(ctx, logger, server, metrics)
	return nil
}

// selfRegisteredRoutes returns the route → scope bindings the policy service
// publishes for itself into its own route store, from which they are compiled
// into every tenant bundle for Layer 1 Envoy/OPA enforcement.
func selfRegisteredRoutes() []routes.RouteScope {
	return []routes.RouteScope{
		{PathPrefix: "/api/v1/policy/stats", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "POST", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/grants", Method: "PATCH", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/grants", Method: "DELETE", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "GET", RequiredScope: "platform:budgets:read"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "PUT", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "DELETE", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/vocabulary", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/roles", Method: "GET", RequiredScope: "platform:roles:read"},
		{PathPrefix: "/api/v1/policy/roles", Method: "PUT", RequiredScope: "platform:roles:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "PATCH", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "DELETE", RequiredScope: "platform:roles:write"},
	}
}

// selfRegisteredScopes returns the platform scope vocabulary the policy service
// owns. Upserted into the vocabulary table on every registration so grants and
// roles can reference these scopes.
func selfRegisteredScopes() []routes.ScopeDeclaration {
	return []routes.ScopeDeclaration{
		{Scope: "platform:grants:read", ResourceType: "platform", PathSegments: []string{"grants"}, Action: "read", Description: "Read policy grants and vocabulary"},
		{Scope: "platform:grants:write", ResourceType: "platform", PathSegments: []string{"grants"}, Action: "write", Description: "Create and delete policy grants"},
		{Scope: "platform:roles:read", ResourceType: "platform", PathSegments: []string{"roles"}, Action: "read", Description: "Read role→scope mappings"},
		{Scope: "platform:roles:write", ResourceType: "platform", PathSegments: []string{"roles"}, Action: "write", Description: "Create and delete role→scope mappings"},
		{Scope: "platform:budgets:read", ResourceType: "platform", PathSegments: []string{"budgets"}, Action: "read", Description: "Read tenant budget assignments"},
		{Scope: "platform:budgets:write", ResourceType: "platform", PathSegments: []string{"budgets"}, Action: "write", Description: "Manage tenant budget assignments on roles and grants"},
	}
}

// policyServiceRoutes returns the route → required-scope table consumed by
// ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Mirrors the table self-registered
// with the policy-service routestore above, plus entries for routes the
// service hosts but does not currently register (effective-scopes, routes,
// stats, vocabulary GETs). The /policy/bundles/ prefix is bypassed inside
// Controller.Middleware() and so is intentionally omitted.
//
// Entries marked RequiredScope: "" require authentication only and pass
// through after AuthMiddleware. Entries with a non-empty scope go through
// policy.Evaluator.Check against the principal's grants.
func policyServiceRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/policy/stats", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "POST", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/grants", Method: "PATCH", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/grants", Method: "DELETE", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "GET", RequiredScope: "platform:budgets:read"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "PUT", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/tenant-budget", Method: "DELETE", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/vocabulary", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/roles", Method: "GET", RequiredScope: "platform:roles:read"},
		{PathPrefix: "/api/v1/policy/roles", Method: "PUT", RequiredScope: "platform:roles:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "PATCH", RequiredScope: "platform:budgets:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "DELETE", RequiredScope: "platform:roles:write"},
		// /effective-scopes returns the caller's own effective scopes — open to
		// any authenticated principal so they can inspect what they hold.
		{PathPrefix: "/api/v1/policy/effective-scopes", Method: "GET", RequiredScope: ""},
		// /routes is the route registry — readable by anyone authenticated.
		{PathPrefix: "/api/v1/policy/routes", Method: "GET", RequiredScope: ""},
		// /routes/register is called by other platform services at startup with
		// a SYSTEM/SERVICE SPIFFE-derived token; gated by platform:routes:write
		// in the future. The handler limits each caller to its own service, and
		// Istio AuthorizationPolicy restricts this path to in-mesh callers.
		{PathPrefix: "/api/v1/policy/routes/register", Method: "POST", RequiredScope: ""},
	}
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     psc.Current.Observability.ServiceName,
		ServiceVersion:  psc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: psc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
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

func initServer(ctx context.Context, logger logging.Logger, metrics *platformmetrics.Bootstrap, controllers ...platformapi.Controller) *http.Server {
	apiServer := platformapi.NewAPI(ctx, logger)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	return apiServer.Server(
		ctx,
		psc.Current.Server.Port,
		psc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(psc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, metrics *platformmetrics.Bootstrap) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Policy service shutdown complete")
}
