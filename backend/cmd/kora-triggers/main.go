// The kora-triggers binary owns the platform's front door for starting work:
// every way something can begin in kora arrives here.
//
// It serves inbound webhook ingestion, the trigger management API, the manual
// fire endpoint, and runs the cron scheduler for time-driven triggers. All four
// surfaces feed one shared pipeline that records a delivery, matches it against the
// triggers that care, authorizes each match, and starts a run under the
// trigger's own SERVICE principal.
//
// It is deliberately separate from identity. Webhook ingestion is the platform's
// only unauthenticated, internet-reachable surface, and identity is the token
// authority serving OAuth, the device grant and JWKS for everything else; a
// burst of inbound webhooks must not be able to degrade authentication. The
// scheduler and its background loops also do not belong inside a latency-
// sensitive request-serving API.
//
// Tokens are still minted by identity, which remains the sole token authority.
// This binary holds no signing key: it asks identity for each trigger's
// short-lived SERVICE token over the mesh, authenticated by its own SPIRE SVID.
//
// See docs/adr/2026-06-20-extended-trigger-types.md.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/metric"

	tc "github.com/runkora/kora/backend/cmd/kora-triggers/config"
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/brand"
	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	inferenceclient "github.com/runkora/kora/backend/internal/platform/clients/inference"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/triggers"
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
	logger.Info(ctx, "Triggers service starting",
		logging.Field{Key: "version", Value: tc.Current.App.Version})

	metricsBootstrap, err := platformmetrics.Setup(ctx, tc.Current.Metrics, observability.Config{
		ServiceName:    tc.Current.Observability.ServiceName,
		ServiceVersion: tc.Current.App.Version,
		Environment:    config.Environment(),
	}, tc.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metricsBootstrap.Shutdown(shutdownCtx, logger)
	}()

	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	defer db.Close()
	defer redis.Close()

	controllers, scopeClient, triggersMetrics, err := initController(ctx, logger, db, redis, metricsBootstrap.Meter())
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}
	// Close is nil-safe (see (*triggers.Metrics).Close), so this runs
	// unconditionally — mirroring shutdownMeter above.
	defer func() {
		if err := triggersMetrics.Close(); err != nil {
			logger.Error(ctx, "Error shutting down triggers metrics", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	// Publish the Triggers scope vocabulary so the policy service can validate
	// triggers:read / triggers:write when they are granted, and keep it
	// published across policy-service restarts.
	go routescopes.KeepRegistered(ctx, logger, "kora-triggers",
		func(ctx context.Context) error {
			return scopeClient.Register(ctx, "kora-triggers", routeScopes(), scopeDeclarations())
		})

	server := initServer(ctx, logger, controllers, metricsBootstrap)

	run(ctx, logger, server, metricsBootstrap)
	return nil
}

// initController wires the Triggers feature: the Postgres repository, the
// service and its dependencies, the cron scheduler, and the HTTP controllers.
func initController(
	ctx context.Context,
	logger logging.Logger,
	db *storage.DB,
	redis *storage.RedisClient,
	meter metric.Meter,
) ([]platformapi.Controller, *routescopes.Client, *triggers.Metrics, error) {
	cipherKey, err := triggers.LoadCipherKey()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load triggers encryption key: %w", err)
	}
	cipher, err := triggers.NewSecretCipherFromKey(cipherKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init triggers cipher: %w", err)
	}
	// The product name goes into the reasons recorded on routed deliveries.
	b, err := brand.Load()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load brand: %w", err)
	}

	// identity is the sole token authority. This binary holds no signing key:
	// it asks identity for each trigger's SERVICE token, authenticated by the
	// mesh (this pod's SVID presented as the mTLS client cert). The same client
	// exchanges our own SVID for the platform token the policy client needs.
	identityClient := identityclient.NewClient(tc.Current.Identity.InternalURL, logger)
	selfToken := identityClient.SelfTokenSource()

	policyClient := policyclient.NewClient(tc.Current.Policy.PolicyServiceInternalURL, selfToken, logger)
	scopeClient := routescopes.NewClient(tc.Current.Policy.PolicyServiceInternalURL, selfToken, logger)

	policyEval := policy.NewEvaluator(
		tc.Current.Policy.BundleEndpoint,
		tc.Current.BundlePollInterval(),
		logger,
	)
	policyEval.Start(ctx)

	repo := triggers.NewPostgresRepository(db.Pool())
	agentsClient := agentsclient.NewClient(tc.Current.Agents.InternalURL, logger)
	inferenceClient := inferenceclient.NewClient(tc.Current.Inference.InternalURL, logger)

	// Instruments are built once and injected into everything that records:
	// the service, the scheduler and the retention job. Failing to build them
	// is not fatal — a service that refuses to start because a gauge could not
	// be registered has turned its own observability into an outage — but it is
	// loud, because unmeasured is not the state anyone signed up for.
	// RegisterDomain is the shared helper every service's main.go uses for this.
	metrics := platformmetrics.RegisterDomain(ctx, logger, "orbit-triggers", func() (*triggers.Metrics, error) {
		return triggers.NewMetrics(meter, repo, logger)
	})

	registry, err := triggers.NewRegistry()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init trigger providers: %w", err)
	}

	// One admission controller, shared: it holds no per-caller state, and two
	// instances deciding the same trigger's ceiling would be two answers.
	admission := triggers.NewAdmission(repo, logger, metrics)

	// The operator-written tenant registry carries each tenant's API authoring switch.
	tenants := declarative.NewRegistry(tc.Current.Kubernetes.TenantsPath)

	svc := triggers.NewService(
		repo,
		registry,
		cipher,
		policyClient,
		agentsClient,
		inferenceClient,
		identityClient,
		admission,
		b,
		logger,
		metrics,
	)

	// Creating a trigger reads the agent it binds, or each candidate of a
	// routed trigger, from kora-agents as the caller, who must be able to run
	// it, then provisions the trigger's principal and grants. The evaluator
	// checks that the caller holds a routed trigger's decision model scopes.
	// Enabling or disabling one moves work already in the queue.
	lifecycle := triggers.NewTriggerService(
		repo,
		registry,
		policyClient,
		agentsClient,
		policyEval,
		admission,
		tenants,
		logger,
	)

	// The webhook path prefix is basePath + /webhooks/. Registered sources post
	// to this address, so it must match the api VirtualService route.
	sources := triggers.NewSourceService(
		repo,
		registry,
		cipher,
		policyClient,
		agentsClient,
		tenants,
		tc.Current.Server.BasePath+"/webhooks/",
		logger,
	)

	// The delivery worker is what actually starts runs. Every fire is durably
	// queued first, so one the platform acknowledged survives a restart and
	// starts when the process returns, rather than disappearing in between.
	// Safe on every replica: claims use FOR UPDATE SKIP LOCKED.
	worker := triggers.NewWorker(svc, repo, logger)
	worker.Start(ctx)

	// Payload retention. On a timer rather than on write, because the promise
	// is about data at rest: it has to hold for payloads already stored when a
	// tenant shortens their window, not only for new ones.
	triggers.NewRetentionJob(repo, logger, triggers.DefaultPayloadRetentionDays, metrics).Start(ctx)

	// The cron scheduler fires time-driven triggers. It is safe to run on every
	// replica: each tick inserts its delivery under a unique (trigger_id,
	// scheduled_at) key before dispatching, so a losing replica simply stops.
	// See ADR Decision 2.
	scheduler := triggers.NewScheduler(svc, repo, logger, metrics)
	if err := scheduler.Start(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("start trigger scheduler: %w", err)
	}
	go func(parentCtx context.Context) {
		<-parentCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 10*time.Second)
		defer cancel()
		scheduler.Stop(shutdownCtx)
		// Let an in-flight dispatch finish rather than cutting it in half.
		worker.Wait(shutdownCtx)
	}(ctx)

	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())
	auth := authMiddleware(verifier, storage.NewJTIBlocklist(redis), logger)

	// The operator applies TriggerSource and Trigger resources through the declared-item endpoints.
	declared := declarative.NewController(verifier, logger, map[string]declarative.Applier{
		declarative.KindTriggerSources: sources,
		declarative.KindTriggers:       lifecycle,
	})
	return []platformapi.Controller{
		triggers.NewController(svc, sources, lifecycle, auth, policyEval, routeScopes(), tc.Current.Server.BasePath, logger),
		declared,
	}, scopeClient, metrics, nil
}

// routeScopes binds each management route to the scope it requires.
//
// Fire lives under its own prefix with an authenticated-only entry. The handler
// checks trigger:fire or trigger:<id>:fire itself, since a static prefix cannot
// name the trigger (ADR Decision 6).
func routeScopes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/trigger-sources", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "DELETE", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-providers", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers/routing/candidates", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "DELETE", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-fires", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/trigger-deliveries", Method: "GET", RequiredScope: "triggers:read"},
		// Replaying a delivery starts real work, so it sits with the write
		// scope rather than with reading history.
		{PathPrefix: "/api/v1/trigger-deliveries", Method: "POST", RequiredScope: "triggers:write"},
		// Payload retention: reading the window is reading configuration,
		// shortening it changes what the platform stores.
		{PathPrefix: "/api/v1/trigger-retention", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/trigger-retention", Method: "PUT", RequiredScope: "triggers:write"},
	}
}

// scopeDeclarations publishes the Triggers vocabulary. Without it the policy
// service rejects grants naming these scopes, so a PAT created with
// triggers:read would fail its vocabulary check.
func scopeDeclarations() []routescopes.ScopeDeclaration {
	return []routescopes.ScopeDeclaration{
		{Scope: "triggers:read", ResourceType: "triggers", PathSegments: []string{}, Action: "read", Description: "View trigger sources, triggers, and delivery history"},
		{Scope: "triggers:write", ResourceType: "triggers", PathSegments: []string{}, Action: "write", Description: "Create, edit, and delete trigger sources and triggers"},
		// Per-trigger fire permission, granted as trigger:<trigger-id>:fire.
		// The identifier sits between the resource type and the action because
		// a covering scope keeps the action and drops the path: trigger:fire
		// covers every trigger:<id>:fire. Enforced in the handler, since
		// RouteScope cannot express a path-dependent scope (ADR Decision 6).
		{Scope: "trigger:fire", ResourceType: "trigger", PathSegments: []string{}, Action: "fire", Description: "Fire any manual trigger. Grant trigger:<trigger-id>:fire for one trigger"},
	}
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     tc.Current.Observability.ServiceName,
		ServiceVersion:  tc.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: tc.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.ShutdownTimeout())
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

// authMiddleware authenticates the platform token and a delegated run's
// delegation, each checked against the revoked-JTI blocklist.
func authMiddleware(verifier *platformtoken.Verifier, blocklist mw.JTIBlocklist, logger logging.Logger) mw.Middleware {
	return mw.NewMiddleware(mw.MiddlewareChain(
		mw.AuthMiddlewareWithRevocation(verifier, blocklist, logger),
		mw.DelegationMiddlewareWithBlocklist(logger, verifier, blocklist),
	))
}

// initStorage opens the Postgres pool and the Redis client. Redis backs the JTI
// blocklist so revoked tokens are rejected here as promptly as at identity.
func initStorage(ctx context.Context, logger logging.Logger) (*storage.DB, *storage.RedisClient, error) {
	cfg, err := storage.LoadStorageConfig()
	if err != nil {
		return nil, nil, err
	}

	db, err := storage.New(ctx, &cfg.Postgres, logger)
	if err != nil {
		return nil, nil, err
	}

	rc, err := storage.NewRedis(ctx, &cfg.Redis, logger)
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	return db, rc, nil
}

func initServer(ctx context.Context, logger logging.Logger, controllers []platformapi.Controller, metrics *platformmetrics.Bootstrap) *http.Server {
	apiServer := platformapi.NewAPI(ctx, logger)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

	return apiServer.Server(
		ctx,
		tc.Current.Server.Port,
		tc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(tc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controllers...,
	)
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, metrics *platformmetrics.Bootstrap) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down triggers service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "Triggers service shutdown complete")
}
