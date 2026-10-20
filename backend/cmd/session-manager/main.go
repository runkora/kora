package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	smConfig "github.com/runkora/kora/backend/cmd/session-manager/config"
	"github.com/runkora/kora/backend/internal/agents/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func main() {
	ctx := context.Background()

	// kora-agents sets KORA_PRODUCT_NAME on session pods, which the chart does not render.
	// Load the brand first so a bad name exits before observability starts.
	b, err := brand.Load()
	if err != nil {
		log.Fatalf("Failed to load brand: %v", err)
	}

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		log.Fatalf("Failed to setup observability: %v", err)
	}
	defer shutdownObs()

	logger := initLogger()
	logger.Info(ctx, "Session-manager starting",
		logging.Field{Key: "version", Value: smConfig.Current.App.Version},
	)

	// fatal flushes observability (the deferred shutdown above won't run
	// after os.Exit) and exits.
	fatal := func(msg string, err error) {
		logger.Error(ctx, msg, logging.Field{Key: "error", Value: err})
		shutdownObs()
		os.Exit(1)
	}

	metrics, err := platformmetrics.Setup(ctx, smConfig.Current.Metrics, observability.Config{
		ServiceName:    smConfig.Current.Observability.ServiceName,
		ServiceVersion: smConfig.Current.App.Version,
		Environment:    config.Environment(),
	}, smConfig.Current.Observability.ServiceName, logger)
	if err != nil {
		fatal("Failed to setup metrics", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), smConfig.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	server, registry, sessionMetrics := initServer(ctx, logger, metrics, b)
	// Close is nil-safe, so this runs unconditionally — mirroring the
	// metrics.Shutdown defer above, this unregisters the sessions-active
	// observable-gauge callback and closes the sandbox-provisioning/
	// loop-guard domain metrics on shutdown.
	defer func() {
		if err := sessionMetrics.Close(); err != nil {
			logger.Error(ctx, "Error shutting down session-manager metrics", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	run(ctx, logger, server, registry, metrics)
}

// registerSessionMetrics registers the observable gauge tracking the number
// of currently active sessions on this pod (orbit.session_manager.sessions_active{tenant_id}),
// backed by the existing Registry.ActiveCountByTenant(). The instrument name
// is dotted, like every other service's (instrumentServiceName strips a
// leading "orbit-" and the OTel/Prometheus bridge converts the dots to
// underscores at export time), rather than a raw underscored string built by
// hand. tenant_id is a safe label under the 2026-10-20 amendment to
// docs/adr/2026-10-07-domain-metrics-instrumentation.md (bounded, slow-growing
// tenant count); one series is emitted per tenant with at least one active
// session, rather than a single global count.
//
// The returned metric.Registration lets the caller unregister this callback
// on shutdown, mirroring every other service's domain Metrics.Close.
func registerSessionMetrics(meter metric.Meter, registry *sessionmanager.Registry) (metric.Registration, error) {
	gauge, err := meter.Int64ObservableGauge(
		"orbit.session_manager.sessions_active",
		metric.WithDescription("Number of currently active sessions on this session-manager pod"),
	)
	if err != nil {
		return nil, fmt.Errorf("create sessions active gauge: %w", err)
	}

	registration, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for tenantID, count := range registry.ActiveCountByTenant() {
			o.ObserveInt64(gauge, int64(count), metric.WithAttributes(attribute.String("tenant_id", tenantID)))
		}
		return nil
	}, gauge)
	if err != nil {
		return nil, fmt.Errorf("register sessions active callback: %w", err)
	}
	return registration, nil
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     smConfig.Current.Observability.ServiceName,
		ServiceVersion:  smConfig.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: smConfig.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), smConfig.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			log.Printf("Error shutting down observability: %v", err)
		}
	}, nil
}

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
}

// sessionManagerMetrics bundles everything initServer builds on the meter
// that needs to be unregistered/closed on shutdown: the sessions-active
// observable-gauge registration, and the sessionmanager package's own domain
// Metrics (sandbox-provisioning and loop-guard instruments).
type sessionManagerMetrics struct {
	sessionsActive metric.Registration
	domain         *sessionmanager.Metrics
}

// Close unregisters the sessions-active callback and closes the domain
// Metrics. Nil-safe: either field may be nil (e.g. registration failed).
func (m *sessionManagerMetrics) Close() error {
	if m == nil {
		return nil
	}
	if m.sessionsActive != nil {
		if err := m.sessionsActive.Unregister(); err != nil {
			return fmt.Errorf("unregister sessions-active callback: %w", err)
		}
	}
	return m.domain.Close()
}

func initServer(ctx context.Context, logger logging.Logger, metrics *platformmetrics.Bootstrap, b brand.Brand) (*http.Server, *sessionmanager.Registry, *sessionManagerMetrics) {
	apiServer := api.NewAPI(ctx, logger)

	cfg := smConfig.Current

	var notifier sessionmanager.Notifier = sessionmanager.NoopNotifier{}
	if cfg.KoraAgents.URL != "" {
		notifier = sessionmanager.NewHTTPNotifier(cfg.KoraAgents.URL, logger)
	}

	// The session-control API can start, message, interrupt, and kill any run on
	// this pod, so it is always authenticated. There is no opt-out: local
	// development runs in Kubernetes against a real identity service
	// (docs/adr/2026-07-10-kubernetes-only-local-development.md), so a mode that
	// serves this API unauthenticated would exist only to be switched on by
	// mistake.
	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())

	idleTimeout := time.Duration(cfg.Session.IdleTimeoutSecs) * time.Second
	registry := sessionmanager.NewRegistry(logger, cfg.Workspaces.BasePath, notifier, idleTimeout, b)
	sessionsActiveReg, err := registerSessionMetrics(metrics.Meter(), registry)
	if err != nil {
		logger.Error(ctx, "failed to register session-manager active-sessions gauge; running uninstrumented",
			logging.Field{Key: "error", Value: err.Error()})
	}
	// Sandbox-provisioning and loop-guard domain metrics
	// (docs/adr/2026-10-07-domain-metrics-instrumentation.md), filed under the
	// orbit-agents row of that ADR's catalog even though this binary emits them:
	// sandbox provisioning and the loop guard are both internal/agents/sessionmanager,
	// the package this ADR's table names for them. RegisterDomain is the shared
	// helper every service's main.go uses for this.
	smMetrics := platformmetrics.RegisterDomain(ctx, logger, "session-manager", func() (*sessionmanager.Metrics, error) {
		return sessionmanager.NewMetrics(metrics.Meter())
	})
	registry.SetMetrics(smMetrics)
	controller := sessionmanager.NewController(logger, sessionmanager.ControllerConfig{
		ClaudeBinaryPath:    cfg.Claude.BinaryPath,
		WrapperPath:         cfg.Runner.WrapperPath,
		ShellPath:           cfg.Runner.ShellPath,
		ClaudeBare:          cfg.Claude.Bare,
		SelfPort:            cfg.Server.Port,
		WorkspacesBasePath:  cfg.Workspaces.BasePath,
		CodexBinaryPath:     cfg.Codex.BinaryPath,
		OpenCodeBinaryPath:  cfg.OpenCode.BinaryPath,
		InferenceGatewayURL: cfg.InferenceGateway.URL,
		ToolGatewayURL:      cfg.ToolGateway.URL,
		OTLPEndpoint:        observability.DefaultConfig.OTLPEndpoint,
		NodeMaxOldSpaceMB:   cfg.Runner.NodeMaxOldSpaceMB,
		Brand:               b,
	}, registry, verifier)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	return apiServer.Server(
		ctx,
		cfg.Server.Port,
		cfg.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(cfg.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	), registry, &sessionManagerMetrics{sessionsActive: sessionsActiveReg, domain: smMetrics}
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, registry *sessionmanager.Registry, metrics *platformmetrics.Bootstrap) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	metrics.Start(ctx, logger)

	<-quit
	// The server keeps serving the runs on this pod while they finish. The kubelet
	// stops the pod when its termination grace period ends.
	logger.Info(ctx, "Draining session-manager", logging.Field{Key: "active_sessions", Value: registry.ActiveCount()})
	drainCtx, cancelDrain := ctx, context.CancelFunc(func() {})
	if d, limited := smConfig.Current.DrainTimeout(); limited {
		drainCtx, cancelDrain = context.WithTimeout(ctx, d)
	}
	registry.Drain(drainCtx, time.Second)
	cancelDrain()
	registry.CloseAll(ctx)
	logger.Info(ctx, "Shutting down session-manager...")

	shutdownCtx, cancel := context.WithTimeout(ctx, smConfig.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Session-manager shutdown complete")
}
