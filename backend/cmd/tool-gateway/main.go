// The tool-gateway binary governs MCP tool access. In PUBLISHER mode (this
// phase) it mounts the operator-rendered orbit-mcp-servers registry,
// introspects each server's tool surface (initialize + tools/list), and
// (Phase 3.3) publishes the discovered tools as mcp:<server>:<tool>:use scopes.
// The request-path PEP + Streamable-HTTP proxy (Phase 4) live in the same
// binary as a separate concern.
//
// The publisher runs as a replicas=1 deployment: scope publication is
// single-writer by deployment topology because DeleteOrphans is destructive
// (it revokes pruned scopes from grants/roles). See
// docs/adr/2026-06-29-mcp-server-registration-and-introspection.md.
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
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/toolgateway"
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
	logger.Info(ctx, "Tool gateway starting (publisher mode)",
		logging.Field{Key: "version", Value: tgc.Current.App.Version})

	// ── MCP server registry ──────────────────────────────────────────────────
	// Sourced from the operator-aggregated orbit-mcp-servers ConfigMap the
	// gateway mounts, parsed + hot-reloaded from disk.
	registry, err := toolgateway.LoadRegistry(ctx, tgc.Current.Registry.ServersPath, logger)
	if err != nil {
		return fmt.Errorf("init MCP server registry: %w", err)
	}

	// ── Introspection worker ─────────────────────────────────────────────────
	introspector := toolgateway.NewIntrospector(nil, tgc.Current.App.Version, logger)

	// ── Introspection cycle ──────────────────────────────────────────────────
	// Re-introspect every registered server. Serialized so the reload hook and
	// the timer never run a cycle concurrently. Phase 3.3 extends this to
	// publish the discovered tools' scopes after introspection.
	var cycleMu sync.Mutex
	cycle := func(ctx context.Context) {
		cycleMu.Lock()
		defer cycleMu.Unlock()
		introspector.Sync(ctx, registry.Servers())
	}

	registry.OnReload(cycle)
	go registry.Watch(ctx)
	go runIntrospectionLoop(ctx, cycle, tgc.Current.ReintrospectInterval(), logger)

	// ── Health server ────────────────────────────────────────────────────────
	apiServer := platformapi.NewAPI(ctx, logger)
	srv := apiServer.Server(
		ctx,
		tgc.Current.Server.HTTPPort,
		tgc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(tgc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
	)
	go func() {
		logger.Info(ctx, "Starting tool-gateway HTTP server", logging.Field{Key: "addr", Value: srv.Addr})
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "tool-gateway HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down tool gateway...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), tgc.Current.ShutdownTimeout())
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "tool-gateway HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Tool gateway shutdown complete")
	return nil
}

// runIntrospectionLoop runs the introspection cycle once at startup and then on
// a timer until ctx is canceled.
func runIntrospectionLoop(ctx context.Context, cycle func(context.Context), interval time.Duration, logger logging.Logger) {
	cycle(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Debug(ctx, "periodic re-introspection tick")
			cycle(ctx)
		}
	}
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
