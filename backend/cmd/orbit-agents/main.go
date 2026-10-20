package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agentsConfig "github.com/runkora/kora/backend/cmd/orbit-agents/config"
	"github.com/runkora/kora/backend/internal/agents"
	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/brand"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	policyclient "github.com/runkora/kora/backend/internal/platform/clients/policy"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	smclient "github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/config"
	"github.com/runkora/kora/backend/internal/platform/logging"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func main() {
	var (
		renderMode      = flag.Bool("render", false, "render final manifests to stdout and exit")
		renderPatchDir  = flag.String("patch-dir", agentsConfig.Current.Agents.ManifestPatchDir, "manifest patches directory")
		renderAgentSlug = flag.String("agent-slug", "", "agent type slug to render (required with -render)")
		renderAgentID   = flag.String("agent-type-id", "", "agent type ID for the orbit.io/agent-type label (defaults to render-<slug>)")
		renderTenantID  = flag.String("tenant-id", "example", "tenant ID for the rendered manifests")
		renderNamespace = flag.String("namespace", "tenant-example", "tenant namespace for the rendered manifests")
		renderSandbox   = flag.Bool("sandbox", false, "also render a sample sandbox pod")
	)
	flag.Parse()

	if *renderMode {
		at := agents.AgentType{ID: *renderAgentID, Slug: *renderAgentSlug, TenantID: *renderTenantID}
		if at.ID == "" {
			at.ID = "render-" + at.Slug
		}
		if err := renderManifests(*renderPatchDir, at, *renderNamespace, *renderSandbox); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := realMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func realMain() error {
	if err := agentsConfig.Validate(agentsConfig.Current); err != nil {
		return fmt.Errorf("config: %w", err)
	}
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

	metrics, err := platformmetrics.Setup(ctx, agentsConfig.Current.Metrics, observability.Config{
		ServiceName:    agentsConfig.Current.Observability.ServiceName,
		ServiceVersion: agentsConfig.Current.App.Version,
		Environment:    config.Environment(),
	}, agentsConfig.Current.Observability.ServiceName, logger)
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.Metrics.ShutdownTimeout())
		defer cancel()
		metrics.Shutdown(shutdownCtx, logger)
	}()

	db, redis, err := initStorage(ctx, logger)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer db.Close()
	defer func() { _ = redis.Close() }()

	eval := initPolicyEvaluator(ctx, logger)

	// The identity client authenticates orbit-agents' own service-to-service
	// calls: its mesh-attested exchange yields the platform token used as the
	// bearer for the policy client, the session-manager API, delegation
	// revocation and route registration. One cached token source serves them
	// all, so the exchange stays off the hot path.
	// Required, not optional. Without it orbit-agents has no bearer for the
	// policy client, no bearer for the session-manager API, and no delegation
	// broker, so the process would come up having silently dropped three
	// security controls. Local development runs in Kubernetes with a real
	// identity service (docs/adr/2026-07-10-kubernetes-only-local-development.md),
	// so an empty value is a broken deployment, not a mode.
	if agentsConfig.Current.Platform.IdentityServiceURL == "" {
		return fmt.Errorf("platform.identityServiceURL is required")
	}
	idClient := identityclient.NewClient(agentsConfig.Current.Platform.IdentityServiceURL, logger)
	selfToken := idClient.SelfTokenSource()

	// The product name goes into the brief and default environment, and to session pods as an env var.
	b, err := brand.Load()
	if err != nil {
		return fmt.Errorf("load brand: %w", err)
	}

	server, repo, atSvc, runSvc, draftSvc, err := initServer(ctx, logger, db, storage.NewJTIBlocklist(redis), eval, idClient, selfToken, metrics, b)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	// Register now and keep the registration alive across policy-service
	// restarts; the shared helper owns the retry and re-registration cadence.
	go routescopes.KeepRegistered(ctx, logger, "orbit-agents", initScopeRegistration(logger, selfToken, repo, atSvc))
	go runSvc.StartReaper(ctx)
	go atSvc.ApplyAll(ctx, repo)
	runSvc.StartScheduler(ctx)
	go draftSvc.StartReaper(ctx)
	run(ctx, logger, server, runSvc, metrics)
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

// initStorage opens the Postgres pool and the Redis client. Redis holds the
// shared revoked-JTI blocklist that refuses a revoked delegation before it expires.
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
	logger.Info(ctx, "Storage initialized successfully")
	return db, rc, nil
}

func initServer(ctx context.Context, logger logging.Logger, db *storage.DB, blocklist mw.JTIBlocklist, eval policy.Evaluator, idClient *identityclient.Client, selfToken func(context.Context) (string, error), metrics *platformmetrics.Bootstrap, b brand.Brand) (*http.Server, *agents.PostgresRepository, *agents.AgentTypeService, *agents.RunService, *agents.DraftService, error) {
	apiServer := api.NewAPI(ctx, logger)

	cfg := agentsConfig.Current

	// Required for the same reason as the identity service. An empty value used
	// to mean "skip grant management", so agent types would provision with no
	// grants written, and initScopeRegistration returned nil, so this service
	// never published its routes. Under fail-closed Layer 1 enforcement an
	// unregistered route denies every request, so a blank string took out both
	// grant management and the entire agents API, silently.
	if cfg.Platform.PolicyServiceURL == "" {
		return nil, nil, nil, nil, nil, fmt.Errorf("platform.policyServiceURL is required")
	}
	pc := policyclient.NewClient(cfg.Platform.PolicyServiceURL, selfToken, logger)

	patchDir := cfg.Agents.ManifestPatchDir

	// Legacy env-var overrides were removed in favor of the ConfigMap-mounted
	// patch directory (docs/adr/2026-09-15-manifest-patches-configmap.md).
	// Operators who set ORBIT_MANIFEST__* directly on the pod spec would
	// silently lose their customization, so warn loudly at startup with the
	// migration path.
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ORBIT_MANIFEST__") {
			key, _, _ := strings.Cut(e, "=")
			logger.Warn(ctx,
				"legacy ORBIT_MANIFEST__ env var is ignored: migrate it to a patch file under "+patchDir+" (docs/adr/2026-09-15-manifest-patches-configmap.md)",
				logging.Field{Key: "key", Value: key},
			)
		}
	}

	patches, workloads, err := loadProvisioning(cfg, patchDir, b)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	logger.Info(ctx, "loaded provisioning config",
		logging.Field{Key: "patch_dir", Value: patchDir},
		logging.Field{Key: "patch_count", Value: len(patches)},
		logging.Field{Key: "workloads_file", Value: cfg.Agents.WorkloadsFile},
	)

	k8s, err := agents.NewK8sProvisioner(cfg.Kubernetes.InCluster, logger, provisionerConfig(cfg, patches, workloads, b), pc)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize k8s provisioner: %w", err)
	}

	repo := agents.NewPostgresRepository(db.Pool())
	// The session-manager on each agent pod authenticates its caller and checks
	// the principal is orbit-agents, so the same self-token source that bears
	// the policy client's calls bears these.
	sm := smclient.NewClient(logger, selfToken)
	pubsub := agents.NewPubSub()

	verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())
	authz := agents.NewAuthz(eval)

	atSvc := agents.NewAgentTypeService(repo, repo, k8s, authz, agents.AgentTypeServiceConfig{
		MaxConcurrentRunsPerPod: cfg.Agents.MaxConcurrentRunsPerPod,
		TenantNamespaceFn:       cfg.TenantNamespace,
	}, logger)

	sizes := environmentSizes(cfg)
	if validateErr := sizes.Validate(); validateErr != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("agents.environments: %w", validateErr)
	}
	runSvc, err := agents.NewRunService(repo, repo, sm, k8s, pubsub, authz, repo, k8s, k8s, repo, agents.RunServiceConfig{
		DefaultEnvironmentSize:  sizes.Default,
		MaxConcurrentRunsPerPod: cfg.Agents.MaxConcurrentRunsPerPod,
		TenantNamespaceFn:       cfg.TenantNamespace,
		SandboxGuardrails:       sandboxGuardrails(cfg),
		IdleTimeouts: agents.IdleTimeouts{
			Manual:  cfg.Agents.IdleTimeout.ManualSecs,
			Trigger: cfg.Agents.IdleTimeout.TriggerSecs,
		},
		SandboxMount:      cfg.Agents.SandboxMount,
		StartBudget:       time.Duration(cfg.Agents.Scheduling.StartBudgetSecs) * time.Second,
		StartPollInterval: time.Duration(cfg.Agents.Scheduling.PollIntervalSecs) * time.Second,
		WorkerConcurrency: cfg.Agents.Scheduling.WorkerConcurrency,
		Brand:             b,
	}, logger)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize run service: %w", err)
	}

	envSvc := agents.NewEnvironmentService(repo, repo, k8s, k8s, atSvc, agents.EnvironmentServiceConfig{
		TenantNamespaceFn: cfg.TenantNamespace,
		EnvironmentSizes:  sizes,
	}, logger)

	draftSvc := agents.NewDraftService(repo, authz, agents.DraftServiceConfig{
		ReaperInterval: cfg.Drafts.ReaperInterval(),
		Retention:      cfg.Drafts.RetentionDuration(),
	}, logger)

	controller := agents.NewController(atSvc, runSvc, envSvc, draftSvc, verifier, blocklist, eval, agentRoutes(), cfg.Server.BasePath, logger)

	// Read-only connection catalog (GET /api/v1/connections). Auth-only, tenant
	// scoped from the verified token — a separate controller so it carries no
	// route→scope enforcement (the agent-types Controller does). Reads the same
	// orbit-tenant-connections ConfigMap the egress-gateway brokers from.
	catalog := agents.NewConnectionCatalog(cfg.Agents.ConnectionRegistryPath, logger)
	catalogController := agents.NewConnectionCatalogController(catalog, verifier, logger)

	// The scope resolver adds the attached environments' egress, connectors and connector auto-egress to each agent's grant.
	// The agent type service validates attachments against the same environment store.
	resolver := agents.NewScopeResolver(repo, catalog)
	k8s.SetScopeResolver(resolver)
	atSvc.SetEnvironmentLookup(repo)
	atSvc.SetScopeResolver(resolver)
	// An autonomous agent's grant may not exceed the scopes of whoever creates or changes it.
	atSvc.SetCallerScopes(pc)
	runSvc.SetScopeResolver(resolver)

	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 3): StartRun on a delegated agent mints a delegation forwarding
	// the CALLER's bearer; terminal run states revoke it with orbit-agents' own
	// exchanged token (the same token source the policy client uses).
	broker := agents.NewDelegationBroker(idClient, selfToken, cfg.Platform.TrustDomain, logger)
	runSvc.SetDelegationBroker(broker)
	atSvc.SetDelegationBroker(broker)

	// Per-route, not in the global chain below: it must run after RoutePattern
	// has set the route on the request context. See Mux.HandleWithMiddleware.
	apiServer.SetMetricsMiddleware(metrics.Middleware())

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

// sandboxGuardrails reads the install's sandbox provisioning settings.
func sandboxGuardrails(cfg *agentsConfig.Config) agents.SandboxGuardrails {
	return agents.SandboxGuardrails{
		ProvisionAttempts:    cfg.Agents.Guardrails.SandboxProvisionAttempts,
		ProvisionBackoffSecs: cfg.Agents.Guardrails.SandboxProvisionBackoffSecs,
		ProvisionBudgetSecs:  cfg.Agents.Guardrails.SandboxProvisionBudgetSecs,
	}
}

// environmentSizes converts the configured environment sizes.
func environmentSizes(cfg *agentsConfig.Config) agents.EnvironmentSizes {
	size := func(s agentsConfig.EnvironmentSize) agents.EnvironmentSize {
		return agents.EnvironmentSize{CPU: s.CPU, Memory: s.Memory, Disk: s.Disk}
	}
	return agents.EnvironmentSizes{
		Default: size(cfg.Agents.Environments.DefaultSize),
		Max:     size(cfg.Agents.Environments.MaxSize),
	}
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
// initial registration and the periodic self-heal loop.
func initScopeRegistration(logger logging.Logger, selfToken routescopes.TokenSource, repo agentTypeSlugLister, atSvc *agents.AgentTypeService) func(context.Context) error {
	client := routescopes.NewClient(agentsConfig.Current.Platform.PolicyServiceURL, selfToken, logger)

	register := func(registerCtx context.Context) error {
		slugs, err := repo.ListAllAgentTypeSlugs(registerCtx)
		if err != nil {
			return fmt.Errorf("list agent type slugs for scope registration: %w", err)
		}
		if err := client.Register(registerCtx, "orbit-agents", agentRoutes(), agentScopes(slugs)); err != nil {
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
// request time by ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Exact-method
// routes beat "*" and the longest prefix wins, so a method under a broader
// prefix (e.g. PATCH on /agent-types/) needs its own explicit entry.
func agentRoutes() []routescopes.RouteScope {
	return []routescopes.RouteScope{
		// Agent-type WRITE routes are authenticated-only at the route layer:
		// the required scope is mode-qualified (agents:types:{mode}:write,
		// delegated-agent-identity ADR Decision 1) and the mode comes from the
		// request body on create and the stored row on update/delete — neither
		// is visible to a static route map. The authoritative check lives in
		// AgentTypeService via Authz.checkAgentTypeModeWrite. The policy
		// service registers empty-scope routes as authenticated-only.
		{PathPrefix: "/api/v1/agent-types", Method: "POST", RequiredScope: ""},
		// Agent-type reads are authenticated-only too. The governing scope is
		// per-agent (agents:types:{slug}:read) and the creator may read their own
		// type (Authz.ownerAllowed). AgentTypeService filters lists and checks single reads through
		// Authz.filterAgentTypes and Authz.checkAgentType.
		{PathPrefix: "/api/v1/agent-types", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/agent-types/", Method: "GET", RequiredScope: ""},
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
		{PathPrefix: "/api/v1/sandbox-images", Method: "GET", RequiredScope: "agents:environments:read"},
		{PathPrefix: "/api/v1/environment-sizes", Method: "GET", RequiredScope: "agents:environments:read"},
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
		// Every other run route is authenticated-only for the same reason: the
		// governing scope is per-agent and the agent comes from the stored run.
		// RunService checks single runs through Authz.checkRun and scopes lists,
		// outcomes and stats through Authz.readableRunAgentTypes, plus the
		// caller's own runs. A route-level parent scope would lock out every
		// per-agent holder.
		{PathPrefix: "/api/v1/runs", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/run-outcomes", Method: "GET", RequiredScope: ""},
		// One agent's run counts and durations. A sibling prefix of /api/v1/agent-types/.
		{PathPrefix: "/api/v1/run-stats/", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/runs/", Method: "GET", RequiredScope: ""},
		{PathPrefix: "/api/v1/runs/", Method: "DELETE", RequiredScope: ""},
		{PathPrefix: "/api/v1/runs/", Method: "POST", RequiredScope: ""},
	}
}

// agentScopes builds the full scope declaration list from the current agent type slugs.
// Static top-level scopes are always included; per-slug scopes are added dynamically.
func agentScopes(slugs []string) []routescopes.ScopeDeclaration {
	decls := []routescopes.ScopeDeclaration{
		{Scope: "agents:types:read", ResourceType: "agents", PathSegments: []string{"types"}, Action: "read", Description: "View and list every agent type definition (prefix-covers the per-agent type scopes)"},
		{Scope: "agents:types:write", ResourceType: "agents", PathSegments: []string{"types"}, Action: "write", Description: "Create and mutate agent type definitions of any identity mode (prefix-covers the delegated and autonomous child scopes)"},
		// Mode-qualified creation scopes (delegated-agent-identity ADR,
		// Decision 1): creating an agent confers its authority, so the two
		// identity modes are separately grantable. agents:types:write
		// prefix-covers both via the scope_covers hierarchy, so existing
		// grants and the reserved admin roles keep their current power.
		{Scope: "agents:types:delegated:write", ResourceType: "agents", PathSegments: []string{"types", "delegated"}, Action: "write", Description: "Create and mutate delegated agent types (safe self-service: every model, tool and connection call is bounded by both the agent's grants and the access of the person running it; network egress follows the agent's own posture)"},
		{Scope: "agents:types:autonomous:write", ResourceType: "agents", PathSegments: []string{"types", "autonomous"}, Action: "write", Description: "Create and mutate autonomous agent types (higher privilege: confers standing authority the agent holds on its own, whoever starts it)"},
		{Scope: "agents:runs:read", ResourceType: "agents", PathSegments: []string{"runs"}, Action: "read", Description: "View runs and stream agent events"},
		{Scope: "agents:runs:write", ResourceType: "agents", PathSegments: []string{"runs"}, Action: "write", Description: "Start, message, and terminate runs"},
		// Not a prefix cover of the per-agent run scopes. Authz.checkRun accepts it
		// by name for delegated agents only. The slugs delegated and autonomous are
		// reserved so no agent's scope can take this path.
		{Scope: "agents:runs:delegated:write", ResourceType: "agents", PathSegments: []string{"runs", "delegated"}, Action: "write", Description: "Start runs of any delegated agent and message, interrupt and terminate the runs you started (each run acts with the starter's own access; autonomous agents are not included)"},
		{Scope: "agents:usage:read", ResourceType: "agents", PathSegments: []string{"usage"}, Action: "read", Description: "View every agent's usage, spend and budgets (prefix-covers the per-agent usage scopes)"},
		{Scope: "agents:delegated:read", ResourceType: "agents", PathSegments: []string{"delegated"}, Action: "read", Description: "View the runs of delegated agents that other people started, with their messages, events and outputs"},
		{Scope: "agents:environments:read", ResourceType: "agents", PathSegments: []string{"environments"}, Action: "read", Description: "View and list sandbox environments and the registered sandbox images"},
		{Scope: "agents:environments:write", ResourceType: "agents", PathSegments: []string{"environments"}, Action: "write", Description: "Create, edit, and delete sandbox environments"},
	}
	for _, slug := range slugs {
		decls = append(decls,
			routescopes.ScopeDeclaration{
				Scope:        "agents:types:" + slug + ":read",
				ResourceType: "agents",
				PathSegments: []string{"types", slug},
				Action:       "read",
				Description:  "View the " + slug + " agent type definition",
			},
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
			routescopes.ScopeDeclaration{
				Scope:        "agents:usage:" + slug + ":read",
				ResourceType: "agents",
				PathSegments: []string{"usage", slug},
				Action:       "read",
				Description:  "View usage, spend and budget for the " + slug + " agent type",
			},
		)
	}
	return decls
}

func run(ctx context.Context, logger logging.Logger, server *http.Server, runSvc *agents.RunService, metrics *platformmetrics.Bootstrap) {
	go func() {
		logger.Info(ctx, "Starting HTTP server", logging.Field{Key: "addr", Value: server.Addr})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(ctx, "Failed to start server", logging.Field{Key: "error", Value: err.Error()})
		}
	}()
	metrics.Start(ctx, logger)

	<-ctx.Done()
	logger.Info(ctx, "Shutting down orbit-agents...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), agentsConfig.Current.ShutdownTimeout())
	defer cancel()

	// Let a run start in flight finish rather than leave the run in starting. Open
	// run streams hold Shutdown until its timeout, so the wait runs beside it.
	scheduled := make(chan struct{})
	go func() { runSvc.WaitScheduler(shutdownCtx); close(scheduled) }()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(ctx, "Server forced to shutdown", logging.Field{Key: "error", Value: err.Error()})
	}
	<-scheduled
	metrics.StopServer(shutdownCtx, logger)

	logger.Info(ctx, "orbit-agents shutdown complete")
}
