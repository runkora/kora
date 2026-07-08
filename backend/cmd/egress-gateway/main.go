// The egress-gateway binary is the per-tenant credential broker that
// terminates TLS for sandbox-resident tools, evaluates policy on outbound
// calls, mints just-in-time credentials, and forwards to the real upstream.
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
//
// Topology — two listeners on two ports:
//
//	tool (transparent CONNECT via sandbox Envoy)
//	  CONNECT host:443 →  egress-gateway :8085 (CONNECT port)
//	                        ConnectHandler (CONNECT only; other verbs rejected)
//	                          terminate TLS via per-tenant CA leaf
//	                          ProxyHandler → Broker.Authorize
//	                            policy.Check connection:<name>:<action>
//	                            adapter.Resolve (mint)
//	                          inject minted credential, forward upstream
//
//	frontend BFF
//	  GET /api/v1/connections → egress-gateway :8086 (admin HTTP port)
//	                              platform API server (health + read-only
//	                              connections catalog). Plain HTTP — istio
//	                              treats it like any service port (no CONNECT
//	                              filter chains), unlike the CONNECT port.
//
// Connections are GitOps CRs (Amendment 3): the operator aggregates each
// tenant's Connection CRs into the orbit-tenant-connections ConfigMap, which
// the gateway mounts and parses into its in-memory registry (hot-reloaded on
// change). There is no Postgres dependency and no admin connections API.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	egc "github.com/clear-route/orbit/cmd/egress-gateway/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/credentials/adapters/k8sbearer"
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

	// ── Policy evaluator ─────────────────────────────────────────────────────
	eval := policy.NewEvaluator(egc.Current.Policy.BundleEndpoint, egc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	// ── Connection registry ──────────────────────────────────────────────────
	// Sourced from the operator-aggregated orbit-tenant-connections ConfigMap
	// the gateway mounts (Amendment 3), parsed + hot-reloaded from disk. No
	// Postgres dependency.
	connRegistry, err := credentials.LoadConfigMapRegistry(ctx, egc.Current.Secrets.ConnectionRegistryPath, logger)
	if err != nil {
		return fmt.Errorf("init connection registry: %w", err)
	}

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
	// Static ServiceAccount bearer token, reusing the same k8sfile secret
	// resolver instance as githubapp. No minting, no cache.
	registry.Register(k8sbearer.New(secretResolver, logger))

	// ── Per-tenant CA bundle ─────────────────────────────────────────────────
	caBundle, err := cabundle.LoadEager(ctx, egc.Current.Secrets.TenantCARoot, logger)
	if err != nil {
		return fmt.Errorf("init tenant CA bundle: %w", err)
	}

	// ── Broker + handlers ────────────────────────────────────────────────────
	broker := egressgw.NewBroker(eval, connRegistry, registry, logger)
	proxyHandler := egressgw.NewProxyHandler(broker, http.DefaultTransport, logger)
	connectHandler := egressgw.NewConnectHandler(caBundle, proxyHandler, connRegistry, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// ── Scope publisher ──────────────────────────────────────────────────────
	// Publishes the connection:<name>:<action> vocabulary from the mounted
	// registry. There is no admin API (connections are GitOps CRs); the
	// publisher runs at startup and on every registry reload.
	scopeClient := routescopes.NewClient(egc.Current.Policy.PolicyServiceInternalURL, logger)
	scopePub := egressgw.NewScopePublisher(connRegistry, registry, scopeClient, logger)

	// ── Admin/read HTTP server (health + read-only connections) ──────────────
	// Served on its OWN plain-HTTP port (AdminHTTPPort), separate from the
	// CONNECT port. The CONNECT port carries istio CONNECT-specific config
	// (EnvoyFilter + "http-connect" Service port + mTLS-from-sandbox filter
	// chains), so a plain HTTP GET there fails to match the CONNECT filter
	// chains. This listener has no CONNECT config, so istio handles it like any
	// service port and the frontend BFF can issue a normal HTTP GET.
	//
	// The only controller is the read-only connections catalog — the admin
	// CRUD API was removed by Amendment 3 (connections are GitOps CRs). Its own
	// Middleware() enforces auth on GET /connections; the health endpoint is
	// wired outside the controller mux and stays open.
	connectionsReadController := egressgw.NewConnectionsReadController(connRegistry, registry, verifier, logger)
	apiServer := platformapi.NewAPI(ctx, logger)
	adminHTTP := apiServer.Server(
		ctx,
		egc.Current.Server.AdminHTTPPort,
		egc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(egc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		connectionsReadController,
	)

	// ── CONNECT listener ─────────────────────────────────────────────────────
	// The CONNECT port serves ONLY CONNECT (sandboxes only ever send CONNECT).
	// CONNECT bypasses the admin server's middleware — the request body of a
	// CONNECT is a TLS stream that logging/tracing middleware must not touch —
	// but still flows through AuthMiddleware so the broker sees verified claims.
	// ConnectHandler itself rejects any non-CONNECT verb with an error response.
	connectChain := mw.MiddlewareChain(mw.AuthMiddleware(verifier, logger))
	authedConnect := connectChain(connectHandler)

	connectSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", egc.Current.Server.HTTPPort),
		Handler: authedConnect,
		// CONNECT must support hijacking; net/http's stdlib server does this
		// automatically. ReadHeaderTimeout protects against slowloris on the
		// CONNECT preamble.
		ReadHeaderTimeout: 10 * time.Second,
	}

	// ── Scope vocabulary registration ────────────────────────────────────────
	// Publish the full connection:<name>:<action> vocabulary at startup
	// (ListByTenant("") returns every connection in the mounted registry), and
	// republish on every registry reload (a Connection CR / Secret change
	// re-aggregated by the operator). Runs in a goroutine because the
	// routescopes client retries until the identity-proxy sidecar is ready.
	connRegistry.OnReload(func(ctx context.Context) {
		// Drop every cached upstream transport so a rotated ca_cert (or any
		// other Connection/Secret change) takes effect on the next forward
		// without a gateway restart.
		proxyHandler.Transports().Invalidate()
		if err := scopePub.Publish(ctx, ""); err != nil {
			logger.Error(ctx, "scope republish after registry reload failed", logging.Field{Key: "error", Value: err.Error()})
		}
	})
	go connRegistry.Watch(ctx)
	go func() {
		if err := scopePub.Publish(ctx, ""); err != nil {
			logger.Error(ctx, "startup scope registration failed", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	go func() {
		logger.Info(ctx, "Starting CONNECT server", logging.Field{Key: "addr", Value: connectSrv.Addr})
		if err := connectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "CONNECT server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	go func() {
		logger.Info(ctx, "Starting admin HTTP server", logging.Field{Key: "addr", Value: adminHTTP.Addr})
		if err := adminHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "admin HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down egress gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), egc.Current.ShutdownTimeout())
	defer cancel()
	if err := connectSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "CONNECT server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	if err := adminHTTP.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "admin HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Egress gateway shutdown complete")
	return nil
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
