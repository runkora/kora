// The tool-gateway binary governs MCP tool access. One image runs in two modes,
// deployed as two separate Deployments that scale independently:
//
//   - publisher: mounts the operator-rendered kora-mcp-servers registry,
//     introspects each server's tool surface (initialize + tools/list), publishes
//     the discovered tools as mcp:<server>:<tool>:use scopes, and writes the tool
//     catalog.
//   - proxy: the request-path PEP (per-tool authorization) and the Streamable-HTTP
//     proxy to the upstream MCP server.
//
// The publisher must run as replicas=1: scope publication is single-writer by
// deployment topology because DeleteOrphans is destructive (it revokes pruned
// scopes from grants/roles), so two concurrent writers could race. The proxy is
// stateless on the request path and scales horizontally.
//
// The mode is selected per Deployment by the config file the chart renders for
// it. See docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	tgc "github.com/runkora/kora/backend/cmd/tool-gateway/config"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/toolgateway"
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

	metrics, err := platformmetrics.Setup(ctx, tgc.Current.Metrics, observability.Config{
		ServiceName:    tgc.Current.Observability.ServiceName,
		ServiceVersion: tgc.Current.App.Version,
		Environment:    config.Environment(),
	}, tgc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	// Trusted-host allowlist for the host-derived RFC 9728 protected-resource
	// metadata and 401 challenge (ADR 2026-07-04).
	platformapi.ConfigureTrustedHosts(tgc.Current.PublicHosts)
	proxyMode := tgc.Current.IsProxyMode()
	logger.Info(ctx, "Tool gateway starting",
		logging.Field{Key: "version", Value: tgc.Current.App.Version},
		logging.Field{Key: "mode", Value: map[bool]string{true: "proxy", false: "publisher"}[proxyMode]})

	// ── MCP server registry ──────────────────────────────────────────────────
	// Sourced from the operator-aggregated kora-mcp-servers ConfigMap the
	// gateway mounts, parsed + hot-reloaded from disk. Both modes read it.
	registry, err := toolgateway.LoadRegistry(ctx, tgc.Current.Registry.ServersPath, logger)
	if err != nil {
		return fmt.Errorf("init MCP server registry: %w", err)
	}

	// ── Storage (catalog) ────────────────────────────────────────────────────
	db, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	defer db.Close()
	catalogRepo := toolgateway.NewPostgresCatalog(db.Pool())

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())
	// Both modes build upstream requests from the same credential lookup.
	creds := toolgateway.NewUpstreamCredentials(tgc.Current.Registry.SecretsDir)
	controllers := []platformapi.Controller{toolgateway.NewCatalogController(catalogRepo, registry, verifier, logger)}

	if proxyMode {
		// ── Request-path PEP + proxy ─────────────────────────────────────────
		eval := policy.NewEvaluator(tgc.Current.Policy.BundleEndpoint, tgc.Current.BundlePollInterval(), logger)
		eval.Start(ctx)
		// The shared revoked-JTI blocklist rejects a revoked delegation before
		// its token expires (docs/adr/2026-07-23-delegated-agent-identity.md).
		redis, err := initRedis(ctx, logger)
		if err != nil {
			return fmt.Errorf("init redis: %w", err)
		}
		defer func() { _ = redis.Close() }()
		blocklist := storage.NewJTIBlocklist(redis)
		proxy := toolgateway.NewProxy(creds, tgc.Current.App.Version, logger)
		// MCP OAuth (RFC 9728): the MCP route challenges unauthenticated clients
		// toward the protected-resource metadata, which the metadata controller
		// serves and which points at the identity authorization server. All URLs
		// are derived from the request host — only the base path is configured.
		basePath := tgc.Current.Server.BasePath
		controllers = append(controllers,
			toolgateway.NewMCPController(verifier, eval, catalogRepo, registry, proxy, blocklist, basePath, logger),
			toolgateway.NewOAuthMetadataController(basePath, logger),
		)
		go registry.Watch(ctx) // keep the mounted registry fresh for server resolution
	} else {
		// ── Publisher: introspection + scope publishing + catalog write ──────
		introspector := toolgateway.NewIntrospector(creds, tgc.Current.App.Version, logger)
		selfToken := identityclient.NewClient(tgc.Current.Identity.InternalURL, logger).SelfTokenSource()
		scopeClient := routescopes.NewClient(tgc.Current.Policy.PolicyServiceInternalURL, selfToken, logger)
		publisher := toolgateway.NewPublisher(registry, introspector, scopeClient, logger)

		// Re-introspect servers, publish scopes (prune-guarded), rebuild the
		// catalog. Serialized so the reload hook and timer never race.
		//
		// force=true (registry reload, and the initial boot cycle) introspects
		// every server unconditionally; force=false (a periodic timer tick)
		// introspects only servers due per their own ReintrospectIntervalSeconds
		// (0 = reload-only, honored by Introspector.Sync).
		var cycleMu sync.Mutex
		cycle := func(ctx context.Context, force bool) {
			cycleMu.Lock()
			defer cycleMu.Unlock()
			introspector.Sync(ctx, registry.Servers(), force)
			if pubErr := publisher.Publish(ctx); pubErr != nil {
				logger.Error(ctx, "MCP scope publish failed", logging.Field{Key: "error", Value: pubErr.Error()})
			}
			if replaceErr := catalogRepo.Replace(ctx, toolgateway.CatalogToolsFromSnapshots(introspector.Snapshot())); replaceErr != nil {
				logger.Error(ctx, "MCP catalog rebuild failed", logging.Field{Key: "error", Value: replaceErr.Error()})
			}
		}
		registry.OnReload(func(ctx context.Context) { cycle(ctx, true) })
		go registry.Watch(ctx)
		ready := func() bool { return introspector.AllSeenOrRetained(registry.Servers()) }
		go runIntrospectionLoop(ctx, cycle, tgc.Current.ReintrospectInterval(), catchUpInterval, ready, logger)
	}

	// ── HTTP server (health + catalog [+ MCP proxy in proxy mode]) ───────────
	apiServer := platformapi.NewAPI(ctx, logger)
	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())
	srv := apiServer.Server(
		ctx,
		tgc.Current.Server.HTTPPort,
		tgc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(),
			mw.LoggingMiddleware(logger),
			// Rewrites /run/{run_id}/v1/mcp/… → /v1/mcp/… with the run identity
			// in context — the same mechanism the inference gateway mounts, so
			// per-run MCP configs carry run identity for delegation binding.
			// See docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4.
			mw.NewMiddleware(mw.StripRunPrefix),
		),
		controllers...,
	)
	go func() {
		logger.Info(ctx, "Starting tool-gateway HTTP server", logging.Field{Key: "addr", Value: srv.Addr})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "tool-gateway HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down tool gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.ShutdownTimeout())
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "tool-gateway HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Tool gateway shutdown complete")
	return nil
}

// catchUpInterval is how soon the loop re-introspects while some registered
// server has never been reached since boot.
//
// A server reached once keeps its tools in the snapshot, so a later failure is
// invisible to callers and waiting for the configured interval costs nothing.
// A server that has never been reached has nothing retained: the publisher
// registers no scopes for it and the catalog holds no tools, so every session
// started before the next cycle sees an empty tool list. The configured
// interval is minutes, which is the five-minute outage in #880. Ticking sooner
// closes that window, and because the catch-up tick is unforced it re-reaches
// only the servers that are actually due — a healthy server inside its own
// interval is skipped.
const catchUpInterval = 10 * time.Second

// runIntrospectionLoop drives the introspection cycle: once at boot, then on a
// timer. ready reports whether every registered server has tools to serve;
// while it is false the loop waits catchUp instead of the configured interval,
// so a cold start converges in seconds rather than minutes. catchUp is a
// parameter rather than a package var so a test can shorten it without racing
// a running loop.
//
// The wait is recomputed each pass rather than held in a ticker, so the loop
// settles back to the configured interval on its own once every server is
// reached. Nothing blocks on a server that never comes up: an unreachable
// server just keeps the loop on the short interval, and each attempt is still
// bounded by the introspection timeout.
func runIntrospectionLoop(
	ctx context.Context,
	cycle func(context.Context, bool),
	interval, catchUp time.Duration,
	ready func() bool,
	logger logging.Logger,
) {
	cycle(ctx, true)
	for {
		wait := interval
		if !ready() {
			wait = min(catchUp, interval)
			logger.Warn(ctx, "an MCP server has no tools to serve — re-introspecting early",
				logging.Field{Key: "retry_in", Value: wait.String()},
				logging.Field{Key: "configured_interval", Value: interval.String()})
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		logger.Debug(ctx, "periodic re-introspection tick")
		cycle(ctx, false)
	}
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

func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, error) {
	cfg, err := storage.LoadPostgresConfig()
	if err != nil {
		return nil, err
	}
	db, err := storage.New(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(ctx, "Database initialized successfully")
	return db, nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     tgc.Current.Observability.ServiceName,
		ServiceVersion:  tgc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: tgc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.ShutdownTimeout())
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
