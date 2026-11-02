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

		// The route and scope registration lives in the policy service's
		// memory, so it has to be republished on a cadence this gateway's own
		// introspection interval cannot supply: a cycle that changed nothing
		// publishes nothing, and a gateway whose servers are all steady (or
		// whose registry is empty) would otherwise never republish at all.
		// That is the same problem every other binary solves with
		// KeepRegistered, on the same deliberately uniform cadence.
		go routescopes.KeepRegistered(ctx, logger, toolgateway.ServiceName, publisher.Publish)

		// Re-introspect servers, publish scopes (prune-guarded), rebuild the
		// catalog. Serialized so the reload hook and timer never race.
		//
		// force=true (registry reload, and the initial boot cycle) introspects
		// every server unconditionally; force=false (a periodic timer tick)
		// introspects only servers due per their own ReintrospectIntervalSeconds
		// (0 = reload-only, honored by Introspector.Sync).
		cycle := newCycle(
			func(ctx context.Context, force bool) bool { return introspector.Sync(ctx, registry.Servers(), force) },
			publisher.Publish,
			func(ctx context.Context) error {
				return catalogRepo.Replace(ctx, toolgateway.CatalogToolsFromSnapshots(introspector.Snapshot()))
			},
			logger,
		)
		reloaded := make(chan struct{}, 1)
		registry.OnReload(func(ctx context.Context) {
			cycle(ctx, true)
			select {
			case reloaded <- struct{}{}:
			default:
			}
		})
		go registry.Watch(ctx)
		nextRetry := func() (time.Duration, bool) { return introspector.NextRetryIn(registry.Servers()) }
		go runIntrospectionLoop(ctx, cycle, tgc.Current.ReintrospectInterval(), nextRetry, reloaded, logger)
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

// newCycle returns the serialized introspect, publish, rebuild cycle. publish
// and rebuild are skipped when introspect reports no change, and retried when
// either fails.
func newCycle(
	introspect func(ctx context.Context, force bool) bool,
	publish func(context.Context) error,
	rebuild func(context.Context) error,
	logger logging.Logger,
) func(context.Context, bool) {
	var mu sync.Mutex
	republish := true
	return func(ctx context.Context, force bool) {
		mu.Lock()
		defer mu.Unlock()
		// force means boot or a registry reload. Publish computes prune from
		// registry.Loaded(), which can flip with no snapshot change, so a forced
		// cycle must publish even when nothing was introspected.
		if introspect(ctx, force) || force {
			republish = true
		}
		if !republish {
			return
		}
		republish = false
		if err := publish(ctx); err != nil {
			logger.Error(ctx, "MCP scope publish failed", logging.Field{Key: "error", Value: err.Error()})
			republish = true
		}
		if err := rebuild(ctx); err != nil {
			logger.Error(ctx, "MCP catalog rebuild failed", logging.Field{Key: "error", Value: err.Error()})
			republish = true
		}
	}
}

// runIntrospectionLoop runs the cycle at boot, then waits
// min(nextRetry, interval) each pass. A reload recomputes that wait.
func runIntrospectionLoop(
	ctx context.Context,
	cycle func(context.Context, bool),
	interval time.Duration,
	nextRetry func() (time.Duration, bool),
	reloaded <-chan struct{},
	logger logging.Logger,
) {
	cycle(ctx, true)
	for {
		wait := interval
		if d, pending := nextRetry(); pending && d < wait {
			wait = d
			logger.Debug(ctx, "an MCP server is awaiting its retry — waking early",
				logging.Field{Key: "retry_in", Value: wait.String()},
				logging.Field{Key: "configured_interval", Value: interval.String()})
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-reloaded:
			timer.Stop()
			continue
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
