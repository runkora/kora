package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	psc "github.com/clear-route/orbit/cmd/policy-service/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/policyservice/api"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
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
	routeStore := routes.NewStore()

	bundleCfg := bundle.Config{
		JWKSURL:  psc.Current.Bundle.JWKSURL,
		Issuer:   psc.Current.Bundle.Issuer,
		Audience: psc.Current.Bundle.Audience,
	}
	compiler := bundle.NewCompiler(bundleCfg, routeStore)
	bundleSrv := bundle.NewServer(compiler, grantRepo, vocabRepo, roleRepo, logger)

	// Pre-compile bundles for all tenants that have grants.
	if tenantIDs, err := grantRepo.ListDistinctTenants(ctx); err == nil {
		bundleSrv.Bootstrap(ctx, tenantIDs)
	} else {
		logger.Error(ctx, "bundle bootstrap: failed to list tenants", logging.Field{Key: "error", Value: err.Error()})
	}

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	svc := api.NewService(grantRepo, vocabRepo, roleRepo, bundleSrv, routeStore, logger)

	// Self-register the policy service's own routes and platform scope vocabulary.
	// Direct call — no HTTP round-trip needed since the service owns the store.
	if err := svc.RegisterRoutes(ctx, "policy-service", "", []routes.RouteScope{
		{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/grants", Method: "POST", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/grants", Method: "DELETE", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/vocabulary", Method: "GET", RequiredScope: "platform:grants:read"},
		{PathPrefix: "/api/v1/policy/vocabulary", Method: "DELETE", RequiredScope: "platform:grants:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "GET", RequiredScope: "platform:roles:read"},
		{PathPrefix: "/api/v1/policy/roles", Method: "PUT", RequiredScope: "platform:roles:write"},
		{PathPrefix: "/api/v1/policy/roles", Method: "DELETE", RequiredScope: "platform:roles:write"},
	}, []routes.ScopeDeclaration{
		{Scope: "platform:grants:read", ResourceType: "platform", PathSegments: []string{"grants"}, Action: "read", Description: "Read policy grants and vocabulary"},
		{Scope: "platform:grants:write", ResourceType: "platform", PathSegments: []string{"grants"}, Action: "write", Description: "Create and delete policy grants and vocabulary"},
		{Scope: "platform:roles:read", ResourceType: "platform", PathSegments: []string{"roles"}, Action: "read", Description: "Read role→scope mappings"},
		{Scope: "platform:roles:write", ResourceType: "platform", PathSegments: []string{"roles"}, Action: "write", Description: "Create and delete role→scope mappings"},
		{Scope: "platform:agents:read", ResourceType: "platform", PathSegments: []string{"agents"}, Action: "read", Description: "Read agent registrations"},
		{Scope: "platform:tenants:manage", ResourceType: "platform", PathSegments: []string{"tenants"}, Action: "manage", Description: "Manage tenants (platform-admin only)"},
	}); err != nil {
		logger.Error(ctx, "policy-service self-registration failed", logging.Field{Key: "error", Value: err.Error()})
	}

	controller := api.NewController(svc, logger, verifier)
	server := initServer(ctx, logger, controller)

	run(ctx, logger, server)
	return nil
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

func initServer(ctx context.Context, logger logging.Logger, controller *api.Controller) *http.Server {
	apiServer := platformapi.NewAPI(ctx, logger)
	return apiServer.Server(
		ctx,
		psc.Current.Server.Port,
		psc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(psc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Policy service shutdown complete")
}
