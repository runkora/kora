// The orbit-triggers binary owns the platform's front door for starting work:
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

	tc "github.com/clear-route/orbit/cmd/orbit-triggers/config"
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/triggers"
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

	meter, shutdownMeter, err := initMeter(ctx)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer shutdownMeter()

	logger := initLogger()
	logger.Info(ctx, "Triggers service starting",
		logging.Field{Key: "version", Value: tc.Current.App.Version})

	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	defer db.Close()
	defer redis.Close()

	controller, scopeClient, err := initController(ctx, logger, db, redis, meter)
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}

	// Publish the Triggers scope vocabulary so the policy service can validate
	// triggers:read / triggers:write when they are granted, and keep it
	// published across policy-service restarts.
	go routescopes.KeepRegistered(ctx, logger, "orbit-triggers",
		func(ctx context.Context) error {
			return scopeClient.Register(ctx, "orbit-triggers", routeScopes(), scopeDeclarations())
		})

	run(ctx, logger, initServer(ctx, logger, controller))
	return nil
}

// initController wires the Triggers feature: the Postgres repository, the
// service and its dependencies, the cron scheduler, and the HTTP controller.
func initController(
	ctx context.Context,
	logger logging.Logger,
	db *storage.DB,
	redis *storage.RedisClient,
	meter metric.Meter,
) (platformapi.Controller, *routescopes.Client, error) {
	cipherKey, err := triggers.LoadCipherKey()
	if err != nil {
		return nil, nil, fmt.Errorf("load triggers encryption key: %w", err)
	}
	cipher, err := triggers.NewSecretCipherFromKey(cipherKey)
	if err != nil {
		return nil, nil, fmt.Errorf("init triggers cipher: %w", err)
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

	// Instruments are built once and injected into everything that records:
	// the service, the scheduler and the retention job. Failing to build them
	// is not fatal — a service that refuses to start because a gauge could not
	// be registered has turned its own observability into an outage — but it is
	// loud, because unmeasured is not the state anyone signed up for.
	metrics, err := triggers.NewMetrics(meter, repo, logger)
	if err != nil {
		logger.Error(ctx, "failed to register trigger metrics; running uninstrumented",
			logging.Field{Key: "error", Value: err.Error()})
		metrics = nil
	}

	registry, err := triggers.NewRegistry()
	if err != nil {
		return nil, nil, fmt.Errorf("init trigger providers: %w", err)
	}

	// One admission controller, shared: it holds no per-caller state, and two
	// instances deciding the same trigger's ceiling would be two answers.
	admission := triggers.NewAdmission(repo, logger)

	svc := triggers.NewService(
		repo,
		registry,
		cipher,
		policyClient,
		agentsClient,
		identityClient,
		admission,
		logger,
		metrics,
	)

	// Creating a trigger provisions its principal and grant, then resolves the
	// agent it binds by asking orbit-agents as that principal. Enabling or
	// disabling one moves work already in the queue.
	lifecycle := triggers.NewTriggerService(
		repo,
		registry,
		policyClient,
		agentsClient,
		identityClient,
		admission,
		logger,
	)

	// The webhook path prefix is basePath + /webhooks/. Registered sources post
	// to this address, so it must match the api VirtualService route.
	sources := triggers.NewSourceService(
		repo,
		registry,
		cipher,
		policyClient,
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
		return nil, nil, fmt.Errorf("start trigger scheduler: %w", err)
	}
	go func(parentCtx context.Context) {
		<-parentCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 10*time.Second)
		defer cancel()
		scheduler.Stop(shutdownCtx)
		// Let an in-flight dispatch finish rather than cutting it in half.
		worker.Wait(shutdownCtx)
	}(ctx)

	// Revocation-aware authentication: a revoked token is rejected immediately
	// rather than staying usable until expiry, matching the behavior these
	// routes had while they were served by identity.
	verifier := platformtoken.NewVerifierFromConfig(ctx, platformtoken.LoadConfig())
	auth := mw.AuthMiddlewareWithRevocation(verifier, storage.NewJTIBlocklist(redis), logger)

	return triggers.NewController(svc, sources, lifecycle, auth, policyEval, routeScopes(), tc.Current.Server.BasePath, logger), scopeClient, nil
}

// routeScopes binds each management route to the scope it requires.
//
// The fire endpoint shares the POST /api/v1/triggers prefix with trigger
// creation because RouteScope matches static prefixes only. Per-trigger fire
// authorization is enforced in the handler instead — see ADR Decision 6.
func routeScopes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		{PathPrefix: "/api/v1/trigger-sources", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-sources", Method: "DELETE", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-providers", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers", Method: "POST", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "GET", RequiredScope: "triggers:read"},
		{PathPrefix: "/api/v1/triggers", Method: "PATCH", RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/triggers", Method: "DELETE", RequiredScope: "triggers:write"},
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
		{Scope: "trigger:fire", ResourceType: "trigger", PathSegments: []string{}, Action: "fire", Description: "Fire any manual trigger; grant trigger:<trigger-id>:fire for one"},
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

// initMeter sets up the OpenTelemetry metrics pipeline for this binary and
// returns the meter the Triggers instruments are built on. The pipeline is
// the platform's: exporter selection lives in observability.SetupMetrics, so
// metrics and traces are configured by the same two settings and cannot end
// up pointing at different collectors.
func initMeter(ctx context.Context) (metric.Meter, func(), error) {
	meter, shutdown, err := observability.SetupMetrics(ctx, observability.Config{
		ServiceName:    tc.Current.Observability.ServiceName,
		ServiceVersion: tc.Current.App.Version,
		Environment:    config.Environment(),
	})
	if err != nil {
		return nil, nil, err
	}
	return meter, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.ShutdownTimeout())
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down metrics: %v\n", err)
		}
	}, nil
}

func initLogger() logging.Logger {
	if observability.DefaultConfig.LogFormat == "console" {
		return logging.NewDevelopmentLogger()
	}
	return logging.NewLogger()
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

func initServer(ctx context.Context, logger logging.Logger, controller platformapi.Controller) *http.Server {
	apiServer := platformapi.NewAPI(ctx, logger)
	return apiServer.Server(
		ctx,
		tc.Current.Server.Port,
		tc.Current.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(tc.Current.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
	)
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down triggers service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "Triggers service shutdown complete")
}
