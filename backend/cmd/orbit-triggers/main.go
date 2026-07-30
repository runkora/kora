// The orbit-triggers binary owns the platform's front door for starting work:
// every way something can begin in Orbit arrives here.
//
// It serves inbound webhook ingestion, the trigger management API, the manual
// fire endpoint, and runs the cron scheduler for time-driven triggers. All four
// kinds feed one shared pipeline that records a delivery, matches it against the
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
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc/credentials"

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

	controller, policyClient, err := initController(ctx, logger, db, redis, meter)
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}

	// Publish the Triggers scope vocabulary so the policy service can validate
	// triggers:read / triggers:write when they are granted. Best-effort and
	// retried in the background: the policy service may still be starting.
	go registerScopes(ctx, logger, policyClient)
	// Then keep it published: the policy service's registration store is in
	// memory, so its restart would otherwise leave us silently unregistered.
	go runRegistrationLoop(ctx, logger, policyClient, tc.Current.ReregistrationInterval())

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
) (platformapi.Controller, *policyclient.Client, error) {
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

	policyClient := policyclient.NewClient(
		tc.Current.Policy.PolicyServiceInternalURL,
		newSelfTokenSource(identityClient),
		logger,
	)

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

	// Inbound webhook path prefix: basePath + /webhooks/. Registered sources
	// post to this address, so it must match the api VirtualService route.
	svc := triggers.NewService(
		repo,
		triggers.NewRegistry(),
		cipher,
		policyClient,
		agentsClient,
		identityClient,
		tc.Current.Server.BasePath+"/webhooks/",
		logger,
		metrics,
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

	return triggers.NewController(svc, auth, policyEval, routeScopes(), tc.Current.Server.BasePath, logger), policyClient, nil
}

// newSelfTokenSource returns a token func for the policy client that exchanges
// this pod's SVID for a platform token, caching it until shortly before expiry.
//
// The exchange is a network call on the mesh, so caching keeps it off the hot
// path; the early refresh margin avoids handing out a token that expires
// mid-request.
func newSelfTokenSource(identityClient *identityclient.Client) func(context.Context) (string, error) {
	const refreshMargin = 30 * time.Second

	// Guarded: the policy client calls this from concurrent request handlers.
	var (
		mu     sync.Mutex
		cached string
		expiry time.Time
	)
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()

		if cached != "" && time.Now().Before(expiry.Add(-refreshMargin)) {
			return cached, nil
		}
		tok, exp, err := identityClient.Exchange(ctx)
		if err != nil {
			return "", fmt.Errorf("exchange SVID for platform token: %w", err)
		}
		cached, expiry = tok, exp
		return cached, nil
	}
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

// registerScopes publishes the route table and vocabulary to the policy service,
// retrying with backoff while it comes up. Registration is idempotent.
//
// It goes through the authenticated policy client rather than the anonymous
// routescopes client: /routes/register requires a bearer token, which the
// client obtains by exchanging this pod's SVID.
func registerScopes(ctx context.Context, logger logging.Logger, policyClient *policyclient.Client) {
	const (
		maxAttempts = 10
		baseDelay   = 2 * time.Second
	)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := policyClient.RegisterRoutes(ctx, "orbit-triggers", routeScopes(), scopeDeclarations()); err == nil {
			logger.Info(ctx, "triggers scope vocabulary registered with policy service")
			return
		} else {
			logger.Warn(ctx, "trigger scope registration attempt failed, retrying",
				logging.Field{Key: "attempt", Value: attempt},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * baseDelay):
		}
	}

	logger.Error(ctx, "trigger scope registration gave up after max attempts")
}

// runRegistrationLoop re-registers routes and scopes on a fixed cadence, so a
// policy-service restart does not leave this service unregistered.
//
// Registration lives in the policy service's memory. Registering once at startup
// therefore holds only until that pod restarts, after which nothing re-publishes
// our route table and every scope we declare is unknown to it. Matches the
// inference gateway and tool gateway, which self-heal the same way for the same
// reason. Registration is idempotent, so a tick that changes nothing costs one
// call.
func runRegistrationLoop(ctx context.Context, logger logging.Logger, policyClient *policyclient.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := policyClient.RegisterRoutes(ctx, "orbit-triggers", routeScopes(), scopeDeclarations()); err != nil {
				logger.Warn(ctx, "periodic trigger scope re-registration failed",
					logging.Field{Key: "error", Value: err.Error()})
				continue
			}
			logger.Debug(ctx, "trigger scope vocabulary re-registered")
		}
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
// returns the meter the Triggers instruments are built on.
//
// It deliberately mirrors observability.Setup's exporter selection, including
// its priority order — an explicit OTLP endpoint always wins, and everything
// else gets a provider that records nothing — so metrics and traces are
// configured by the same two settings and cannot end up pointing at different
// collectors.
//
// It lives here rather than in internal/platform/observability because that
// package is traces-only today and is shared by every binary; adding a metrics
// pipeline to it changes the runtime behavior of twelve other services at once.
// This is the first service to need metrics, and the shape it proves out here is
// what should be lifted into the platform package when the second one does.
func initMeter(ctx context.Context) (metric.Meter, func(), error) {
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(tc.Current.Observability.ServiceName),
		semconv.ServiceVersion(tc.Current.App.Version),
		semconv.DeploymentEnvironment(config.Environment()),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("create metrics resource: %w", err)
	}

	endpoint := observability.DefaultConfig.OTLPEndpoint
	if endpoint == "" {
		// No collector configured: a provider with no reader records nothing and
		// exports nothing. The instruments still exist, so no call site needs a
		// nil check and nothing is written to pod logs.
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
		otel.SetMeterProvider(mp)
		return mp.Meter(tc.Current.Observability.ServiceName), func() { _ = mp.Shutdown(context.Background()) }, nil
	}

	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(endpoint)}
	if observability.DefaultConfig.OTLPInsecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}
	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
	)
	otel.SetMeterProvider(mp)

	return mp.Meter(tc.Current.Observability.ServiceName), func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tc.Current.ShutdownTimeout())
		defer cancel()
		if err := mp.Shutdown(shutdownCtx); err != nil {
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
