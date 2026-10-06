// The tool-gateway binary governs MCP tool access. One image runs in two modes,
// deployed as two separate Deployments that scale independently:
//
//   - publisher: mounts the operator-rendered orbit-mcp-servers registry,
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

	tgc "github.com/clear-route/orbit/cmd/tool-gateway/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	platformmetrics "github.com/clear-route/orbit/internal/platform/metrics"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/toolgateway"
	"go.opentelemetry.io/otel/metric"
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

	_, metricsSrv, metricsMW, shutdownMeter, err := initMetrics(ctx, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer shutdownMeter()

	// Trusted-host allowlist for the host-derived RFC 9728 protected-resource
	// metadata and 401 challenge (ADR 2026-07-04).
	platformapi.ConfigureTrustedHosts(tgc.Current.PublicHosts)
	proxyMode := tgc.Current.IsProxyMode()
	logger.Info(ctx, "Tool gateway starting",
		logging.Field{Key: "version", Value: tgc.Current.App.Version},
		logging.Field{Key: "mode", Value: map[bool]string{true: "proxy", false: "publisher"}[proxyMode]})

	// ── MCP server registry ──────────────────────────────────────────────────
	// Sourced from the operator-aggregated orbit-mcp-servers ConfigMap the
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
	controllers := []platformapi.Controller{toolgateway.NewCatalogController(catalogRepo, registry, verifier, logger)}

	if proxyMode {
		// ── Request-path PEP + proxy ─────────────────────────────────────────
		eval := policy.NewEvaluator(tgc.Current.Policy.BundleEndpoint, tgc.Current.BundlePollInterval(), logger)
		eval.Start(ctx)
		proxy := toolgateway.NewProxy(tgc.Current.App.Version, logger)
		// MCP OAuth (RFC 9728): the MCP route challenges unauthenticated clients
		// toward the protected-resource metadata, which the metadata controller
		// serves and which points at the identity authorization server. All URLs
		// are derived from the request host — only the base path is configured.
		basePath := tgc.Current.Server.BasePath
		controllers = append(controllers,
			toolgateway.NewMCPController(verifier, eval, catalogRepo, registry, proxy, basePath, logger),
			toolgateway.NewOAuthMetadataController(basePath, logger),
		)
		go registry.Watch(ctx) // keep the mounted registry fresh for server resolution
	} else {
		// ── Publisher: introspection + scope publishing + catalog write ──────
		introspector := toolgateway.NewIntrospector(nil, tgc.Current.App.Version, logger)
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
		go runIntrospectionLoop(ctx, cycle, tgc.Current.ReintrospectInterval(), logger)
	}

	// ── HTTP server (health + catalog [+ MCP proxy in proxy mode]) ───────────
	apiServer := platformapi.NewAPI(ctx, logger)
	srv := apiServer.Server(
		ctx,
		tgc.Current.Server.HTTPPort,
		tgc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(tgc.Current.Observability.ServiceName),
			metricsMW,
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
	startMetricsServer(ctx, logger, metricsSrv)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down tool gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.ShutdownTimeout())
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "tool-gateway HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	shutdownMetricsServer(shutdownCtx, logger, metricsSrv)

	logger.Info(ctx, "Tool gateway shutdown complete")
	return nil
}

// initMetrics sets up the OTel metrics pipeline — an OTLP reader when a
// collector is configured, and/or a Prometheus reader when metrics are
// enabled — and returns this service's dedicated metrics *http.Server (nil
// when disabled), the HTTP request middleware built on the same meter, and a
// cleanup func. Bundling the middleware here, rather than returning the bare
// meter, keeps realMain to a single error check for the whole pipeline.
func initMetrics(ctx context.Context, logger logging.Logger) (metric.Meter, *http.Server, mw.Middleware, func(), error) {
	promExporter, err := platformmetrics.New(tgc.Current.Metrics)
	if err != nil {
		return nil, nil, mw.Middleware{}, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	meter, shutdownOtelMetrics, err := observability.SetupMetrics(ctx, observability.Config{
		ServiceName:    tgc.Current.Observability.ServiceName,
		ServiceVersion: tgc.Current.App.Version,
		Environment:    config.Environment(),
	}, promExporter.Reader())
	if err != nil {
		return nil, nil, mw.Middleware{}, nil, err
	}

	metricsMW, err := mw.MetricsMiddleware(meter, tgc.Current.Observability.ServiceName)
	if err != nil {
		return nil, nil, mw.Middleware{}, nil, fmt.Errorf("create metrics middleware: %w", err)
	}

	metricsSrv := promExporter.Server(logger)
	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdownOtelMetrics(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down metrics: %v\n", err)
		}
	}
	return meter, metricsSrv, metricsMW, cleanup, nil
}

// startMetricsServer starts srv in its own goroutine, or does nothing when
// metrics are disabled (srv is nil).
func startMetricsServer(ctx context.Context, logger logging.Logger, srv *http.Server) {
	if srv == nil {
		return
	}
	go func() {
		logger.Info(ctx, "Starting metrics server", logging.Field{Key: "addr", Value: srv.Addr})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start metrics server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
}

// shutdownMetricsServer gracefully stops srv, or does nothing when metrics
// are disabled (srv is nil).
func shutdownMetricsServer(ctx context.Context, logger logging.Logger, srv *http.Server) {
	if srv == nil {
		return
	}
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error(ctx, "Metrics server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
}

// runIntrospectionLoop runs the introspection cycle once (forced) at startup
// and then on a timer (unforced — each server's own reintrospect interval
// applies) until ctx is canceled.
func runIntrospectionLoop(ctx context.Context, cycle func(context.Context, bool), interval time.Duration, logger logging.Logger) {
	cycle(ctx, true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Debug(ctx, "periodic re-introspection tick")
			cycle(ctx, false)
		}
	}
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
