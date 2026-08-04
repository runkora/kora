package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/platform/api"
	identityclient "github.com/clear-route/orbit/internal/platform/clients/identity"
	policyclient "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	smclient "github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/observability"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/storage"
	"github.com/clear-route/orbit/internal/platform/token"
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

	shutdownObs, err := initObservability(ctx)
	if err != nil {
		return fmt.Errorf("setup observability: %w", err)
	}
	defer shutdownObs()

	logger := initLogger()
	logger.Info(ctx, "orbit-agents starting",
		logging.Field{Key: "version", Value: agentsConfig.Current.App.Version},
	)

	db, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer db.Close()

	eval := initPolicyEvaluator(ctx, logger)

	server, repo, atSvc, runSvc, draftSvc, err := initServer(ctx, logger, db, eval)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	// Register now and keep the registration alive across policy-service
	// restarts; the shared helper owns the retry and re-registration cadence.
	if register := initScopeRegistration(logger, repo, atSvc); register != nil {
		go routescopes.KeepRegistered(ctx, logger, "orbit-agents", register)
	}
	go runSvc.StartReaper(ctx)
	go draftSvc.StartReaper(ctx)
	run(ctx, logger, server)
	return nil
}

// initPolicyEvaluator constructs an in-process OPA evaluator and starts the
// background bundle poll. The evaluator is consulted by the agents authz layer
// on every public request — zero network hops on the eval hot path. See docs/adr/2026-06-04-orbit-agents-authorization-model.md.
func initPolicyEvaluator(ctx context.Context, logger logging.Logger) policy.Evaluator {
	cfg := agentsConfig.Current
	eval := policy.NewEvaluator(cfg.Policy.BundleEndpoint, cfg.Policy.BundlePollInterval(), logger)
	eval.Start(ctx)
	return eval
}

func initObservability(ctx context.Context) (func(), error) {
	shutdown, err := observability.Setup(ctx, observability.Config{
		ServiceName:     agentsConfig.Current.Observability.ServiceName,
		ServiceVersion:  agentsConfig.Current.App.Version,
		Environment:     config.Environment(),
		ShutdownTimeout: agentsConfig.Current.ShutdownTimeout(),
	})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.ShutdownTimeout())
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

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, eval policy.Evaluator) (*http.Server, agents.AgentTypeRepository, *agents.AgentTypeService, *agents.RunService, *agents.DraftService, error) {
	apiServer := api.NewAPI(ctx, logger)

	cfg := agentsConfig.Current

	// The identity client authenticates orbit-agents' own service-to-service
	// calls: its mesh-attested exchange yields the SERVICE platform token used
	// as the bearer for the policy client and for delegation revocation. One
	// cached token source serves both, so the exchange stays off the hot path.
	var idClient *identityclient.Client
	var selfToken func(context.Context) (string, error)
	if cfg.Platform.IdentityServiceURL != "" {
		idClient = identityclient.NewClient(cfg.Platform.IdentityServiceURL, logger)
		selfToken = idClient.SelfTokenSource()
	}

	// Build a policy client for grant management at agent provision/deprovision time.
	// When policyServiceURL is empty (local dev), the provisioner skips grant management.
	var pc *policyclient.Client
	if cfg.Platform.PolicyServiceURL != "" && idClient != nil {
		pc = policyclient.NewClient(cfg.Platform.PolicyServiceURL, selfToken, logger)
	}

	manifestOverrides, malformedOverrideKeys := agents.ParseManifestOverrides(os.Environ())
	for _, key := range malformedOverrideKeys {
		logger.Warn(ctx, "ignoring malformed ORBIT_MANIFEST override: expected ORBIT_MANIFEST__Kind__name__path",
			logging.Field{Key: "key", Value: key},
		)
	}

	k8s, err := agents.NewK8sProvisioner(cfg.Kubernetes.InCluster, logger, agents.K8sProvisionerConfig{
		SessionManagerImage:       cfg.Agents.SessionManagerImage,
		SandboxImage:              cfg.Agents.SandboxImage,
		SandboxGRPCPort:           int32(cfg.Agents.SandboxGRPCPort), //nolint:gosec
		GitAuthorName:             cfg.Agents.SandboxGitAuthorName,
		GitAuthorEmail:            cfg.Agents.SandboxGitAuthorEmail,
		SessionManagerPostgresSSL: cfg.Agents.SessionManager.PostgresSSLMode,
		TrustDomain:               cfg.Platform.TrustDomain,
		SystemNamespace:           cfg.Kubernetes.SystemNamespace,
		ManifestOverrides:         manifestOverrides,
	}, pc)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize k8s provisioner: %w", err)
	}

	repo := agents.NewPostgresRepository(db.Pool())
	sm := smclient.NewClient(logger)
	pubsub := agents.NewPubSub()

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	authz := agents.NewAuthz(eval)

	atSvc := agents.NewAgentTypeService(repo, repo, k8s, authz, agents.AgentTypeServiceConfig{
		MaxConcurrentRunsPerPod: cfg.Agents.MaxConcurrentRunsPerPod,
		TenantNamespaceFn:       cfg.TenantNamespace,
	}, logger)

	runSvc := agents.NewRunService(repo, repo, sm, k8s, pubsub, authz, agents.RunServiceConfig{
		MaxConcurrentRunsPerPod: cfg.Agents.MaxConcurrentRunsPerPod,
		TenantNamespaceFn:       cfg.TenantNamespace,
	}, logger)

	envSvc := agents.NewEnvironmentService(repo, repo, k8s, agents.EnvironmentServiceConfig{
		TenantNamespaceFn: cfg.TenantNamespace,
	}, logger)

	draftSvc := agents.NewDraftService(repo, authz, agents.DraftServiceConfig{
		ReaperInterval: cfg.Drafts.ReaperInterval(),
		Retention:      cfg.Drafts.RetentionDuration(),
	}, logger)

	controller := agents.NewController(atSvc, runSvc, envSvc, draftSvc, verifier, eval, agentRoutes(), cfg.Server.BasePath, logger)

	// Read-only connection catalog (GET /api/v1/connections). Auth-only, tenant
	// scoped from the verified token — a separate controller so it carries no
	// route→scope enforcement (the agent-types Controller does). Reads the same
	// orbit-tenant-connections ConfigMap the egress-gateway brokers from.
	catalog := agents.NewConnectionCatalog(cfg.Agents.ConnectionRegistryPath, logger)
	catalogController := agents.NewConnectionCatalogController(catalog, verifier, logger)

	// Wire the grant scope resolver: it folds a referenced environment's egress +
	// connectors and each connector's adapter-declared auto-egress into the agent's
	// effective grant (env store + connection catalog back it). The agent type
	// service uses the same env store to validate references at config time.
	resolver := agents.NewScopeResolver(repo, catalog)
	k8s.SetScopeResolver(resolver)
	atSvc.SetEnvironmentLookup(repo)
	atSvc.SetScopeResolver(resolver)
	runSvc.SetScopeResolver(resolver)

	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 3): StartRun on a delegated agent mints a delegation forwarding
	// the CALLER's bearer; terminal run states revoke it with orbit-agents' own
	// exchanged SERVICE token (the same token source the policy client uses).
	// Without an identity service URL the broker stays nil and delegated runs
	// fail closed.
	if idClient != nil {
		broker := agents.NewDelegationBroker(idClient, selfToken, cfg.Platform.TrustDomain, logger)
		runSvc.SetDelegationBroker(broker)
		atSvc.SetDelegationBroker(broker)
	}

	server := apiServer.Server(
		ctx,
		cfg.Server.Port,
		cfg.Server.BasePath,
		mw.MiddlewareChain(
			mw.TracingMiddleware(cfg.Observability.ServiceName),
			mw.LoggingMiddleware(logger),
		),
		controller,
		catalogController,
	)
	return server, repo, atSvc, runSvc, draftSvc, nil
}

// agentTypeSlugLister is the single repository method scope registration needs:
// the slugs that expand into the per-agent-type scope vocabulary.
type agentTypeSlugLister interface {
	ListAllAgentTypeSlugs(ctx context.Context) ([]string, error)
}

// initScopeRegistration wires the policy-service scope vocabulary for orbit-agents.
// It installs a hook on the agent type service so the vocabulary is re-synced
// whenever an agent type is created or deleted, and returns the register
// function; the caller hands it to routescopes.KeepRegistered, which owns the
// initial registration and the periodic self-heal loop. A nil return means no
// policy service is configured (local dev) and there is nothing to register.
func initScopeRegistration(logger logging.Logger, repo agentTypeSlugLister, atSvc *agents.AgentTypeService) func(context.Context) error {
	if agentsConfig.Current.Platform.PolicyServiceURL == "" {
		return nil
	}
	client := routescopes.NewClient(agentsConfig.Current.Platform.PolicyServiceURL, logger)

	register := func(registerCtx context.Context) error {
		slugs, err := repo.ListAllAgentTypeSlugs(registerCtx)
		if err != nil {
			return fmt.Errorf("list agent type slugs for scope registration: %w", err)
		}
		if err := client.Register(registerCtx, "orbit-agents", routescopes.FilterScopedRoutes(agentRoutes()), agentScopes(slugs)); err != nil {
			return fmt.Errorf("register routes: %w", err)
		}
		return nil
	}

	// The hook fires whenever an agent type is written, which changes the
	// per-slug scopes and so has to be republished without waiting for the next
	// tick. SetAgentTypeHook invokes it synchronously inside the write, so the
	// callback must not block on a policy-service round trip; RepublishOnEvent
	// guarantees that.
	atSvc.SetAgentTypeHook(routescopes.RepublishOnEvent(logger, "orbit-agents", register))

	return register
}

// agentRoutes returns the static route→scope bindings for orbit-agents.
//
// Registered with policy-service at startup (docs/adr/2026-05-11-orbit-agents-route-scope-registration.md) and also consumed at
// request time by ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Order matters:
// resolveRequiredScope iterates this slice and prefers exact-method matches,
// so methods that are subsets of broader prefixes (e.g. PATCH on /agent-types/)
// must have their own explicit entry.
func agentRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		// Agent-type WRITE routes are authenticated-only at the route layer:
		// the required scope is mode-qualified (agents:types:{mode}:write,
		// delegated-agent-identity ADR Decision 1) and the mode comes from the
		// request body on create and the stored row on update/delete — neither
		// is visible to a static route map. The authoritative check lives in
		// AgentTypeService via Authz.checkAgentTypeModeWrite. Empty-scope
		// routes are excluded from policy-service registration (see
		// routescopes.FilterScopedRoutes), the established pattern for locally
		// enforced routes.
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-types", Method: "GET", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-types/", Method: "GET", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-types/", Method: "PATCH", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-types/", Method: "DELETE", RequiredScope: ""},
		// Drafts: sibling prefix (ServeMux ambiguity — see server.go Boot).
		// Draft writes mirror the agent-type write posture: authenticated-only
		// at the route layer, with the service requiring create authority for
		// at least one identity mode (Authz.checkAnyAgentTypeWrite) so a
		// delegated-only creator can still use the builder.
		{PathPrefix: "/api/v1/agent-type-drafts", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-type-drafts", Method: "GET", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-type-drafts/", Method: "GET", RequiredScope: "agents:types:read"},
		{PathPrefix: "/api/v1/agent-type-drafts/", Method: "PATCH", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-type-drafts/", Method: "DELETE", RequiredScope: ""},
		{PathPrefix: "/api/v1/environments", Method: "POST", RequiredScope: "agents:environments:write"},
		{PathPrefix: "/api/v1/environments", Method: "GET", RequiredScope: "agents:environments:read"},
		{PathPrefix: "/api/v1/environments/", Method: "GET", RequiredScope: "agents:environments:read"},
		{PathPrefix: "/api/v1/environments/", Method: "PUT", RequiredScope: "agents:environments:write"},
		{PathPrefix: "/api/v1/environments/", Method: "DELETE", RequiredScope: "agents:environments:write"},
		// Starting a run is authenticated-only at the route layer, on the same
		// reasoning as the agent-type writes above: the scope that governs it is
		// per-agent (agents:runs:{slug}:write), and the slug comes from the agent
		// type named in the request body — invisible to a static route map. The
		// authoritative check is Authz.checkRun, run in RunService.authorizeStart
		// before the first write, so a refused start still leaves nothing behind.
		//
		// A route-level agents:runs:write here would not narrow anything, it
		// would widen: every principal allowed to start any run would need the
		// parent scope, which prefix-covers every per-agent child. That is what
		// made a webhook source's token able to start any agent in its tenant.
		// The parent scope still covers the per-agent check for the admin roles
		// that legitimately hold it.
		{PathPrefix: "/api/v1/runs", Method: "POST", RequiredScope: ""},
		{PathPrefix: "/api/v1/runs", Method: "GET", RequiredScope: "agents:runs:read"},
		{PathPrefix: "/api/v1/runs/", Method: "GET", RequiredScope: "agents:runs:read"},
		{PathPrefix: "/api/v1/runs/", Method: "DELETE", RequiredScope: "agents:runs:write"},
		{PathPrefix: "/api/v1/runs/", Method: "POST", RequiredScope: "agents:runs:write"},
	}
}

// agentScopes builds the full scope declaration list from the current agent type slugs.
// Static top-level scopes are always included; per-slug scopes are added dynamically.
func agentScopes(slugs []string) []routescopes.ScopeDeclaration {
	decls := []routescopes.ScopeDeclaration{
		{Scope: "agents:types:read", ResourceType: "agents", PathSegments: []string{"types"}, Action: "read", Description: "View and list agent type definitions"},
		{Scope: "agents:types:write", ResourceType: "agents", PathSegments: []string{"types"}, Action: "write", Description: "Create and mutate agent type definitions of any identity mode (prefix-covers the delegated and autonomous child scopes)"},
		// Mode-qualified creation scopes (delegated-agent-identity ADR,
		// Decision 1): creating an agent confers its authority, so the two
		// identity modes are separately grantable. agents:types:write
		// prefix-covers both via the scope_covers hierarchy, so existing
		// grants and the reserved admin roles keep their current power.
		{Scope: "agents:types:delegated:write", ResourceType: "agents", PathSegments: []string{"types", "delegated"}, Action: "write", Description: "Create and mutate delegated agent types (safe self-service: the agent's grants are only a ceiling on each runner's own access and can never exceed the person running it)"},
		{Scope: "agents:types:autonomous:write", ResourceType: "agents", PathSegments: []string{"types", "autonomous"}, Action: "write", Description: "Create and mutate autonomous agent types (higher privilege: confers standing authority the agent holds on its own, whoever starts it)"},
		{Scope: "agents:runs:read", ResourceType: "agents", PathSegments: []string{"runs"}, Action: "read", Description: "View runs and stream agent events"},
		{Scope: "agents:runs:write", ResourceType: "agents", PathSegments: []string{"runs"}, Action: "write", Description: "Start, message, and terminate runs"},
		{Scope: "agents:environments:read", ResourceType: "agents", PathSegments: []string{"environments"}, Action: "read", Description: "View and list connectivity environments"},
		{Scope: "agents:environments:write", ResourceType: "agents", PathSegments: []string{"environments"}, Action: "write", Description: "Create, edit, and delete connectivity environments"},
	}
	for _, slug := range slugs {
		decls = append(decls,
			routescopes.ScopeDeclaration{
				Scope:        "agents:runs:" + slug + ":read",
				ResourceType: "agents",
				PathSegments: []string{"runs", slug},
				Action:       "read",
				Description:  "View runs and events for the " + slug + " agent type",
			},
			routescopes.ScopeDeclaration{
				Scope:        "agents:runs:" + slug + ":write",
				ResourceType: "agents",
				PathSegments: []string{"runs", slug},
				Action:       "write",
				Description:  "Start, message, and terminate runs for the " + slug + " agent type",
			},
		)
	}
	return decls
}

func run(ctx context.Context, logger logging.Logger, server *http.Server) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()

	<-ctx.Done()
	logger.Info(ctx, "Shutting down orbit-agents...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(ctx, "orbit-agents shutdown complete")
}
