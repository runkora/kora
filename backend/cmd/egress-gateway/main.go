// The egress-gateway binary is the per-tenant credential broker that
// terminates TLS for sandbox-resident tools, evaluates policy on outbound
// calls, mints just-in-time credentials, and forwards to the real upstream.
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
//
// Topology:
//
//	tool (HTTPS_PROXY=gateway)
//	  CONNECT host:443 →  egress-gateway :8085
//	                        method dispatcher
//	                          CONNECT  → ConnectHandler
//	                            terminate TLS via per-tenant CA leaf
//	                            ProxyHandler → Broker.Authorize
//	                              policy.Check connection:<name>:<action>
//	                              adapter.Resolve (mint)
//	                            inject minted credential, forward upstream
//	                          others   → admin connections API
//	                            POST/GET/DELETE /api/v1/connections
//
// Migrations are not run by this binary — they live in
// backend/cmd/egress-gateway/migrations/ and are applied via `make migrate-up`
// (operator-managed).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	egc "github.com/clear-route/orbit/cmd/egress-gateway/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/credentials/resolvers/k8sfile"
	"github.com/clear-route/orbit/internal/egressgw"
	"github.com/clear-route/orbit/internal/egressgw/cabundle"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
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
	logger.Info(ctx, "Egress gateway starting", logging.Field{Key: "version", Value: egc.Current.App.Version})

	// ── Storage ──────────────────────────────────────────────────────────────
	db, err := initDatabase(ctx, logger)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	defer db.Close()

	// ── Policy evaluator ─────────────────────────────────────────────────────
	eval := policy.NewEvaluator(egc.Current.Policy.BundleEndpoint, egc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	// ── Connection registry ──────────────────────────────────────────────────
	connRepo := credentials.NewPostgresRepository(db.Pool())

	// ── Adapter registry ─────────────────────────────────────────────────────
	// Every supported credential mechanism is registered here. A connection
	// naming an unregistered adapter is rejected at request time with
	// credentials.ErrUnsupportedAdapter — never panics.
	secretResolver, err := k8sfile.New(egc.Current.Secrets.ConnectionSecretRoot, logger)
	if err != nil {
		return fmt.Errorf("init secret resolver: %w", err)
	}
	registry := egressgw.NewAdapterRegistry()
	registry.Register(githubapp.New(
		ghclient.NewClient(ghclient.DefaultBaseURL, logger),
		secretResolver,
		logger,
	))
	// No-auth passthrough: governed egress to public/unauthenticated upstreams
	// (no credential minted or injected). Needs no secret resolver.
	registry.Register(passthrough.New())

	// ── Per-tenant CA bundle ─────────────────────────────────────────────────
	caBundle, err := cabundle.LoadEager(ctx, egc.Current.Secrets.TenantCARoot, logger)
	if err != nil {
		return fmt.Errorf("init tenant CA bundle: %w", err)
	}

	// ── Broker + handlers ────────────────────────────────────────────────────
	broker := egressgw.NewBroker(eval, connRepo, registry, logger)
	proxyHandler := egressgw.NewProxyHandler(broker, http.DefaultTransport, logger)
	connectHandler := egressgw.NewConnectHandler(caBundle, proxyHandler, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// ── Admin API ────────────────────────────────────────────────────────────
	adminController := egressgw.NewAdminController(connRepo, verifier, eval, egc.Current.Server.BasePath, logger)
	apiServer := platformapi.NewAPI(ctx, logger)
	adminHTTP := apiServer.Server(
		ctx,
		egc.Current.Server.HTTPPort,
		egc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(egc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		adminController,
	)

	// ── Method-dispatch top-level handler ────────────────────────────────────
	// CONNECT requests bypass the admin server's middleware (the request body
	// of a CONNECT is a TLS stream, which logging/tracing middleware must not
	// touch); other verbs flow through the admin server normally. CONNECT
	// still goes through AuthMiddleware so the broker sees verified claims.
	connectChain := mw.MiddlewareChain(mw.AuthMiddleware(verifier, logger))
	authedConnect := connectChain(connectHandler)
	dispatcher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			authedConnect.ServeHTTP(w, r)
			return
		}
		adminHTTP.Handler.ServeHTTP(w, r)
	})

	httpSrv := &http.Server{
		Addr:    adminHTTP.Addr,
		Handler: dispatcher,
		// CONNECT must support hijacking; net/http's stdlib server does this
		// automatically. ReadHeaderTimeout protects against slowloris on the
		// CONNECT preamble.
		ReadHeaderTimeout: 10 * 1e9, // 10s
	}

	// ── Scope vocabulary registration ────────────────────────────────────────
	// Publish AdminRoutes + connection scopes to the policy service. Connection
	// CRUD that adds new scopes will need to re-register; that's a follow-up
	// hook on the connection repository.
	go registerScopes(ctx, connRepo, registry, logger)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: httpSrv.Addr})
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down egress gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), egc.Current.ShutdownTimeout())
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Egress gateway shutdown complete")
	return nil
}

// initDatabase creates the Postgres pool used by the connection registry.
func initDatabase(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
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

// registerScopes publishes the gateway's admin routes + connection scope
// vocabulary to the policy service. It runs in a goroutine because the
// routescopes client retries until the identity-proxy sidecar is ready.
func registerScopes(ctx context.Context, repo credentials.ConnectionRepository, registry *egressgw.AdapterRegistry, logger logging.Logger) {
	client := routescopes.NewClient(egc.Current.Policy.PolicyServiceInternalURL, logger)

	conns, err := repo.ListByTenant(ctx, "")
	if err != nil {
		// ListByTenant("") returns an empty result by design — scope
		// publication still proceeds with admin scopes only on first start.
		logger.Warn(ctx, "list connections for scope registration failed",
			logging.Field{Key: "error", Value: err.Error()})
	}

	if err := client.Register(ctx, egressgw.ServiceName, egressgw.AdminRoutes(), egressgw.AllScopes(conns, registry)); err != nil {
		logger.Error(ctx, "scope registration failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	logger.Info(ctx, "scope vocabulary registered with policy service",
		logging.Field{Key: "service", Value: egressgw.ServiceName},
		logging.Field{Key: "connections", Value: len(conns)})
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     egc.Current.Observability.ServiceName,
		ServiceVersion:  egc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: egc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), egc.Current.ShutdownTimeout())
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
