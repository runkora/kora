// orbit-mcp is the kora platform MCP server: the first platform-global MCP
// server. It exposes kora's own management surface (agents, policy, models, …)
// as MCP tools over Streamable HTTP, forwarding the caller's token to each kora
// API (passthrough — the downstream service enforces the real scope; this server
// holds no authority). It is intended to be registered as a global MCPServer and
// fronted by the MCP/tool gateway, but serves standalone for testing.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	omc "github.com/runkora/kora/backend/cmd/orbit-mcp/config"
	"github.com/runkora/kora/backend/internal/orbitmcp"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/clients/orbitjson"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
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
	logger.Info(ctx, "kora MCP starting", logging.Field{Key: "version", Value: omc.Current.App.Version})

	metrics, err := platformmetrics.Setup(ctx, omc.Current.Metrics, observability.Config{
		ServiceName:    omc.Current.Observability.ServiceName,
		ServiceVersion: omc.Current.App.Version,
		Environment:    config.Environment(),
	}, omc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), omc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	// The product name is the server's reported name and appears in tool descriptions.
	b, err := brand.Load()
	if err != nil {
		return fmt.Errorf("load brand: %w", err)
	}

	jc := orbitjson.NewClient(logger)
	srv := orbitmcp.NewServer(orbitmcp.Backends{
		Agents:      omc.Current.Backends.Agents,
		Policy:      omc.Current.Backends.Policy,
		Inference:   omc.Current.Backends.Inference,
		Identity:    omc.Current.Backends.Identity,
		Triggers:    omc.Current.Backends.Triggers,
		ToolGateway: omc.Current.Backends.ToolGateway,
	}, jc, b)

	controller := mcpkit.NewController("/mcp", srv)
	apiServer := platformapi.NewAPI(ctx, logger)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	httpSrv := apiServer.Server(
		ctx,
		omc.Current.Server.HTTPPort,
		omc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(omc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: httpSrv.Addr})
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "HTTP server error", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down orbit-mcp...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), omc.Current.ShutdownTimeout())
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "HTTP server forced shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)
	logger.Info(ctx, "kora MCP shutdown complete")
	return nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     omc.Current.Observability.ServiceName,
		ServiceVersion:  omc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: omc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), omc.Current.ShutdownTimeout())
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
