// The egress-gateway binary is the per-tenant credential broker that
// terminates TLS for sandbox-resident tools, evaluates policy on outbound
// calls, mints just-in-time credentials, and forwards to the real upstream.
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
//
// Topology: two listeners on two ports.
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
//	                              connections catalog). Plain HTTP, so istio
//	                              treats it like any service port (no CONNECT
//	                              filter chains), unlike the CONNECT port.
//
// Connections are GitOps CRs (Amendment 3): the operator aggregates each
// tenant's Connection CRs into the kora-tenant-connections ConfigMap, which
// the gateway mounts and parses into its in-memory registry (hot-reloaded on
// change). There is no Postgres dependency and no admin connections API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	egc "github.com/runkora/kora/backend/cmd/egress-gateway/config"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/githubapp"
	"github.com/runkora/kora/backend/internal/credentials/adapters/k8sbearer"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/credentials/resolvers/k8sfile"
	"github.com/runkora/kora/backend/internal/egressgw"
	"github.com/runkora/kora/backend/internal/egressgw/cabundle"
	ghclient "github.com/runkora/kora/backend/internal/platform/clients/github"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
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

	metrics, err := platformmetrics.Setup(ctx, egc.Current.Metrics, observability.Config{
		ServiceName:    egc.Current.Observability.ServiceName,
		ServiceVersion: egc.Current.App.Version,
		Environment:    config.Environment(),
	}, egc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), egc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	// ── Policy evaluator ─────────────────────────────────────────────────────
	eval := policy.NewEvaluator(egc.Current.Policy.BundleEndpoint, egc.Current.BundlePollInterval(), logger)
	eval.Start(ctx)

	// ── Connection registry ──────────────────────────────────────────────────
	// Sourced from the operator-aggregated kora-tenant-connections ConfigMap
	// the gateway mounts (Amendment 3), parsed + hot-reloaded from disk. No
	// Postgres dependency.
	connRegistry, err := credentials.LoadConfigMapRegistry(ctx, egc.Current.Secrets.ConnectionRegistryPath, logger)
	if err != nil {
		return fmt.Errorf("init connection registry: %w", err)
	}

	// ── Adapter registry ─────────────────────────────────────────────────────
	// Every supported credential mechanism is registered here. A connection
	// naming an unregistered adapter is rejected at request time with
	// credentials.ErrUnsupportedAdapter, never a panic.
	secretResolver, err := k8sfile.New(ctx, egc.Current.Secrets.ConnectionSecretRoot, logger)
	if err != nil {
		return fmt.Errorf("init secret resolver: %w", err)
	}
	registry := egressgw.NewAdapterRegistry()
	registry.Register(githubapp.New(
		ghclient.NewClient(ghclient.DefaultBaseURL, logger),
		secretResolver,
		logger,
	))
	// Static ServiceAccount bearer token, reusing the same k8sfile secret
	// resolver instance as githubapp. No minting, no cache.
	registry.Register(k8sbearer.New(secretResolver, logger))

	// ── Per-tenant CA bundle ─────────────────────────────────────────────────
	caBundle, err := cabundle.LoadEager(ctx, egc.Current.Secrets.TenantCARoot, logger)
	if err != nil {
		return fmt.Errorf("init tenant CA bundle: %w", err)
	}

	// ── Broker + handlers ────────────────────────────────────────────────────
	// The passthrough adapter is passed to the broker rather than registered,
	// so a Connection CR cannot name it. It serves hosts with no connection,
	// which the egress whitelist alone authorizes.
	broker := egressgw.NewBroker(eval, connRegistry, registry, passthrough.New(), logger)
	forward, err := upstreamTransport()
	if err != nil {
		return fmt.Errorf("build upstream transport: %w", err)
	}
	// The product name reaches the denial messages agents read.
	b, err := brand.Load()
	if err != nil {
		return fmt.Errorf("load brand: %w", err)
	}
	proxyHandler := egressgw.NewProxyHandler(broker, forward, b, logger)
	connectHandler := egressgw.NewConnectHandler(caBundle, proxyHandler, connRegistry, logger)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())

	// The shared revoked-JTI blocklist rejects a revoked delegation before its
	// token expires (docs/adr/2026-07-23-delegated-agent-identity.md).
	redis, err := initRedis(ctx, logger)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}
	defer func() { _ = redis.Close() }()

	// ── Scope publisher ──────────────────────────────────────────────────────
	selfToken := identityclient.NewClient(egc.Current.Identity.InternalURL, logger).SelfTokenSource()
	registerScopes := initScopeRegistration(logger, selfToken, connRegistry, registry)

	// ── Admin/read HTTP server (health + read-only connections) ──────────────
	// Served on its OWN plain-HTTP port (AdminHTTPPort), separate from the
	// CONNECT port. The CONNECT port carries istio CONNECT-specific config
	// (EnvoyFilter + "http-connect" Service port + mTLS-from-sandbox filter
	// chains), so a plain HTTP GET there fails to match the CONNECT filter
	// chains. This listener has no CONNECT config, so istio handles it like any
	// service port and the frontend BFF can issue a normal HTTP GET.
	//
	// The only controller is the read-only connections catalog. Its own
	// Middleware() enforces auth on GET /connections; the health endpoint is
	// wired outside the controller mux and stays open.
	connectionsReadController := egressgw.NewConnectionsReadController(connRegistry, registry, verifier, logger)
	apiServer := platformapi.NewAPI(ctx, logger)
	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

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
	// CONNECT bypasses the admin server's middleware, because the request body
	// of a CONNECT is a TLS stream that logging and tracing must not touch. It
	// still flows through AuthMiddleware so the broker sees verified claims.
	// The delegation middleware adds the run's verified delegator, which every
	// connection check intersects with, and refuses a revoked delegation.
	// ConnectHandler itself rejects any non-CONNECT verb with an error response.
	connectChain := mw.MiddlewareChain(
		mw.AuthMiddleware(verifier, logger),
		mw.DelegationMiddlewareWithBlocklist(logger, verifier, storage.NewJTIBlocklist(redis)),
	)
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
	// (ListByTenant("") returns every connection in the mounted registry),
	// republish on every registry reload (a Connection CR / Secret change
	// re-aggregated by the operator), and republish on a fixed cadence so the
	// vocabulary survives a policy-service restart. Its registration store is
	// in memory, and an unregistered route is denied by Layer 1 enforcement.
	// The reload hook and the self-heal loop may overlap; that is harmless,
	// because the policy service's registration endpoint is idempotent.
	republish := routescopes.RepublishOnEvent(logger, egressgw.ServiceName, registerScopes)
	connRegistry.OnReload(func(context.Context) {
		// Drop every cached upstream transport so a rotated ca_cert (or any
		// other Connection/Secret change) takes effect on the next forward
		// without a gateway restart.
		proxyHandler.Transports().Invalidate()
		republish()
	})
	go connRegistry.Watch(ctx)
	go caBundle.Watch(ctx)
	go routescopes.KeepRegistered(ctx, logger, egressgw.ServiceName, registerScopes)

	go func() {
		logger.Info(ctx, "Starting CONNECT server", logging.Field{Key: "addr", Value: connectSrv.Addr})
		if err := connectSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "CONNECT server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	go func() {
		logger.Info(ctx, "Starting admin HTTP server", logging.Field{Key: "addr", Value: adminHTTP.Addr})
		if err := adminHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "admin HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

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
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Egress gateway shutdown complete")
	return nil
}

// initScopeRegistration builds the register function that publishes this
// gateway's connection:<name>:<action> vocabulary. It is the one entry point
// for both the startup/self-heal loop and the registry-reload republish, so the
// publisher itself never has to escape this function.
func initScopeRegistration(logger logging.Logger, selfToken routescopes.TokenSource, conns *credentials.ConfigMapRegistry, adapters *egressgw.AdapterRegistry) func(context.Context) error {
	client := routescopes.NewClient(egc.Current.Policy.PolicyServiceInternalURL, selfToken, logger)
	pub := egressgw.NewScopePublisher(conns, adapters, client, logger)
	// tenantID "" publishes the vocabulary for every connection in the mounted
	// registry, which is the whole registry this gateway serves.
	return func(ctx context.Context) error { return pub.Publish(ctx, "") }
}

// upstreamResponseHeaderTimeout bounds how long an upstream may take to return
// response headers. http.DefaultTransport leaves this unset, so a silent
// upstream otherwise holds a forward leg open indefinitely. It is generous
// because git upload-pack on a large repository counts objects before it
// answers; response bodies are not covered, so streaming is unaffected.
const upstreamResponseHeaderTimeout = 3 * time.Minute

// upstreamTransport is the base transport for the forward leg. TransportCache
// clones it per connection that declares a CA and wraps every result in the
// platform tracing transport, so the spans come from there, not here.
func upstreamTransport() (*http.Transport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("http.DefaultTransport is not an *http.Transport")
	}
	t := base.Clone()
	t.ResponseHeaderTimeout = upstreamResponseHeaderTimeout
	// The forward host comes from a sandbox, so the dial refuses the gateway's
	// own loopback and link-local targets. Timeouts match http.DefaultTransport.
	t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: egressgw.RefuseLocalTarget}).DialContext
	return t, nil
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

func initRedis(ctx context.Context, logger logging.Logger) (*storage.RedisClient, error) {
	cfg, err := storage.LoadRedisConfig()
	if err != nil {
		return nil, err
	}
	rc, err := storage.NewRedis(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(ctx, "Redis initialized successfully")
	return rc, nil
}
