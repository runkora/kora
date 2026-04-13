package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	psc "github.com/clear-route/orbit/cmd/policy-service/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/storage"
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

	// Self-register the policy service's own route→scope mappings directly into
	// the store. The policy service owns the store, so no HTTP round-trip is needed.
	routeStore.Register(&routes.Registration{
		Service: "policy-service",
		Routes: []routes.RouteScope{
			{PathPrefix: "/api/v1/policy/grants", Method: "GET", RequiredScope: "platform:grants:read"},
			{PathPrefix: "/api/v1/policy/grants", Method: "POST", RequiredScope: "platform:grants:write"},
			{PathPrefix: "/api/v1/policy/grants", Method: "DELETE", RequiredScope: "platform:grants:write"},
			{PathPrefix: "/api/v1/policy/vocabulary", Method: "GET", RequiredScope: "platform:grants:read"},
			{PathPrefix: "/api/v1/policy/vocabulary", Method: "POST", RequiredScope: "platform:grants:write"},
			{PathPrefix: "/api/v1/policy/vocabulary", Method: "DELETE", RequiredScope: "platform:grants:write"},
			{PathPrefix: "/api/v1/policy/roles", Method: "GET", RequiredScope: "platform:roles:read"},
			{PathPrefix: "/api/v1/policy/roles", Method: "PUT", RequiredScope: "platform:roles:write"},
			{PathPrefix: "/api/v1/policy/roles", Method: "DELETE", RequiredScope: "platform:roles:write"},
		},
	})

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

	svc := api.NewService(grantRepo, vocabRepo, roleRepo, bundleSrv, routeStore, logger)
	controller := api.NewController(svc, logger)
	server := initServer(ctx, logger, controller)
	internalServer := initInternalServer(psc.Current.Server.InternalPort, api.InternalRegisterHandler(svc))

	run(ctx, logger, server, internalServer)
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

// initInternalServer creates a minimal HTTP server for cluster-internal endpoints.
// The internal port is not exposed via the Kubernetes Service so it is only
// reachable from within the cluster. No auth middleware is applied — the cluster
// network is the trust boundary.
func initInternalServer(port int, registerHandler http.HandlerFunc) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/routes/register", registerHandler)
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func run(ctx context.Context, logger logging.Logger, server, internalServer *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	go func() {
		logger.Info(ctx, "Starting internal HTTP server", logging.Field{Key: "addr", Value: internalServer.Addr})
		if err := internalServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start internal server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down policy service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), psc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	if err := internalServer.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Internal server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Policy service shutdown complete")
}
