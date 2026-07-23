package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/google/uuid"

	"github.com/clear-route/orbit/internal/agents/sandbox/kubeconfig"
)

// RunServiceConfig holds runtime parameters for RunService.
type RunServiceConfig struct {
	MaxConcurrentRunsPerPod int
	TenantNamespaceFn       func(tenantID string) string // maps tenant ID → k8s namespace
	// StaleRunTimeout is how long a run may stay in active/starting without a state update
	// before the background reaper marks it failed. Defaults to 30 minutes if zero.
	StaleRunTimeout time.Duration
	// ReaperInterval controls how often the reaper polls. Defaults to 5 minutes if zero.
	// Override in tests to speed up reaper cycles.
	ReaperInterval time.Duration
}

// RunService is the single state machine authority for agent runs.
// Every run state transition anywhere in the system goes through RunService.
type RunService struct {
	repo       RunRepository
	atRepo     AgentTypeRepository
	sm         smClient
	k8s        k8sProvisioner
	pubsub     *PubSub
	authz      *Authz
	cfg        RunServiceConfig
	logger     logging.Logger
	resolver   *ScopeResolver    // resolves the runtime-environment brief; nil = no brief
	delegation *DelegationBroker // mints/revokes run delegations; nil = issuer not wired (delegated runs fail closed)
}

// SetScopeResolver installs the resolver used to render the runtime-environment
// brief appended to an agent's system prompt at run start. When unset, runs start
// without the brief.
func (s *RunService) SetScopeResolver(r *ScopeResolver) { s.resolver = r }

// SetDelegationBroker installs the broker that mints a delegation issuance at
// delegated-run start and revokes it on terminal state
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3). When unset,
// starting a run of a delegated agent fails closed with ErrDelegationUnavailable.
func (s *RunService) SetDelegationBroker(b *DelegationBroker) { s.delegation = b }

// systemPromptWithBrief returns the agent's system prompt with the runtime-
// environment brief appended (governed network + injected credentials), so the
// agent works with its guardrails rather than around them. Best-effort: a nil
// resolver or a resolution error yields the prompt unchanged — it never blocks a run.
func (s *RunService) systemPromptWithBrief(ctx context.Context, at AgentType, runID string) string {
	if s.resolver == nil {
		return at.SystemPrompt
	}
	access, err := s.resolver.EffectiveAccess(ctx, at)
	if err != nil {
		s.logger.Warn(ctx, "render capabilities brief failed; starting run without it",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return at.SystemPrompt
	}
	if access == nil {
		return at.SystemPrompt
	}
	return at.SystemPrompt + renderCapabilitiesBrief(access)
}

// NewRunService creates a RunService with the given dependencies.
func NewRunService(
	repo RunRepository,
	atRepo AgentTypeRepository,
	sm smClient,
	k8s k8sProvisioner,
	pubsub *PubSub,
	authz *Authz,
	cfg RunServiceConfig,
	logger logging.Logger,
) *RunService {
	return &RunService{
		repo:   repo,
		atRepo: atRepo,
		sm:     sm,
		k8s:    k8s,
		pubsub: pubsub,
		authz:  authz,
		cfg:    cfg,
		logger: logger,
	}
}

// StartRun creates a new run and assigns it to a pod. Tenant and created_by are
// derived from the verified platform claims in ctx; request-body values are
// ignored. Returns ErrForbidden if the principal lacks the agents:runs:{slug}:write
// scope; the handler maps this to 404 to avoid leaking existence.
func (s *RunService) StartRun(ctx context.Context, req StartRunRequest) (started *Run, retErr error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.StartRun",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_type_id", req.AgentTypeID),
		),
	)
	defer span.End()

	// Once the run row exists, any later failure (pod assign, session create) must
	// not leave it stuck in pending/starting. Fail it synchronously rather than
	// waiting for the stale-run reaper. A delegation minted before the failure is
	// revoked too — no delegation may outlive a run that never started.
	var createdRunID, mintedJTI string
	defer func() {
		if retErr != nil {
			s.cleanupFailedStart(ctx, createdRunID, mintedJTI)
		}
	}()
	req.TenantID = tenantID
	// created_by is always the verified token subject — the request body cannot set
	// it (prevents attribution spoofing).
	createdBy := ""
	if claims, ok := token.ClaimsFromContext(ctx); ok {
		createdBy = claims.Subject
	}

	at, err := s.atRepo.GetAgentType(ctx, req.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return nil, fmt.Errorf("get agent type: %w", err)
	}
	if at.TenantID != tenantID {
		return nil, ErrNotFound
	}
	if err = s.authz.checkRun(ctx, *at, "write"); err != nil {
		return nil, err
	}

	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 6): only the person — a HUMAN or PAT principal — may start a run
	// of a delegated agent, because the run executes with THEIR authority.
	delegated := at.IdentityMode.OrDefault() == IdentityModeDelegated
	if err = s.authorizeDelegatedStart(ctx, span, delegated); err != nil {
		return nil, err
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	podOrdinal, err := s.providePod(ctx, span, at, namespace)
	if err != nil {
		return nil, err
	}

	run := Run{
		ID:          uuid.NewString(),
		TenantID:    req.TenantID,
		AgentTypeID: req.AgentTypeID,
		State:       RunStatePending,
		CreatedBy:   createdBy,
		// Derived from the agent type (reads join it); carried on the response
		// so run surfaces know the governance posture without a second lookup.
		IdentityMode: at.IdentityMode.OrDefault(),
	}
	span.SetAttributes(attribute.String("run_id", run.ID))

	if err := s.repo.CreateRun(ctx, run); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create run")
		return nil, fmt.Errorf("create run: %w", err)
	}
	createdRunID = run.ID
	if err := s.repo.AssignPod(ctx, run.ID, podOrdinal); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "assign pod")
		return nil, fmt.Errorf("assign pod: %w", err)
	}
	run.State = RunStateStarting
	run.PodOrdinal = podOrdinal

	// Mint the run's delegation from the CALLER's own bearer token (ADR Leg 1):
	// identity never mints delegated authority the caller does not hold, and the
	// response carries only the issuance record — the token never transits
	// orbit-agents. Mint failure fails the run start (fail closed).
	if delegated {
		jti, mintErr := s.mintRunDelegation(ctx, span, run.ID, *at, namespace)
		if mintErr != nil {
			return nil, mintErr
		}
		mintedJTI = jti
		run.DelegationJTI = &jti
	}

	podURL := sessionmanager.PodURL(at.Slug, podOrdinal, namespace)
	smReq := sessionmanager.CreateSessionRequest{
		RunID:        run.ID,
		AgentTypeID:  run.AgentTypeID,
		TenantID:     run.TenantID,
		Framework:    string(at.Framework),
		AllowedTools: at.ToolScopes,
		// One governed MCP endpoint per server the agent has tool grants on.
		MCPServers: at.MCPServers(),
		// The agent's own prompt plus the runtime-environment brief.
		SystemPrompt: s.systemPromptWithBrief(ctx, *at, run.ID),
	}
	if at.ModelID != nil {
		smReq.Model = *at.ModelID
	}
	if err := s.sm.CreateSession(ctx, podURL, smReq); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create session")
		return nil, fmt.Errorf("create session on pod %d: %w", podOrdinal, err)
	}
	if err := s.repo.SetRunState(ctx, run.ID, RunStateStarting, RunStateActive); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "set run active")
		return nil, fmt.Errorf("set run active: %w", err)
	}
	run.State = RunStateActive

	// Optional pre-provisioning: warm the sandbox pod so the first tool call avoids
	// cold-start latency. Runs in the background (see maybeWarmSandbox).
	s.maybeWarmSandbox(ctx, req.ProvisionSandbox, run, at.Slug, namespace, *at)

	s.tryInitialMessage(ctx, podURL, run.ID, at, req.InitialMessage)

	s.logger.Info(ctx, "Run started",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "agent_type_id", Value: run.AgentTypeID},
		logging.Field{Key: "pod_ordinal", Value: run.PodOrdinal},
	)
	return &run, nil
}

// loadRunForAction loads a run together with its agent type, enforcing tenant
// scoping and an authz check for the given action ("read" or "write"). It returns
// ErrNotFound when the run is missing, belongs to another tenant, or the principal
// is forbidden (forbidden is mapped to not-found to avoid leaking existence). This
// is the single guard shared by GetRun/SendMessage/InterruptRun/TerminateRun;
// errors are recorded on the caller's active span.
func (s *RunService) loadRunForAction(ctx context.Context, runID, action string) (*Run, *AgentType, error) {
	span := trace.SpanFromContext(ctx)
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "get run")
		}
		return nil, nil, err
	}
	if run.TenantID != tenantFromContext(ctx) {
		return nil, nil, ErrNotFound
	}
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return nil, nil, fmt.Errorf("get agent type: %w", err)
	}
	if err := s.authz.checkRun(ctx, *at, action); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	// Delegated identity model (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 6): a delegated agent's run is private to its starter; only a
	// tenant admin may additionally read (and terminate) it. Denies map to
	// ErrNotFound so existence is not leaked. Autonomous runs are unaffected.
	if err := checkDelegatedRunVisibility(ctx, at, run); err != nil {
		return nil, nil, err
	}
	return run, at, nil
}

// GetRun retrieves a run by ID. Enforces tenant scoping against ctx claims and
// runs an authz check; returns ErrNotFound when the run belongs to another
// tenant (avoiding existence leak) and the same on authz deny.
func (s *RunService) GetRun(ctx context.Context, id string) (*Run, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetRun",
		trace.WithAttributes(attribute.String("run_id", id)),
	)
	defer span.End()
	run, _, err := s.loadRunForAction(ctx, id, "read")
	if err != nil {
		return nil, err
	}
	return run, nil
}

// ListRuns returns all runs the principal in ctx may read, optionally filtered
// by agent type. Tenant is derived from ctx claims (never from a query string),
// and the result is filtered by per-agent-type grants via authz.filterRuns.
func (s *RunService) ListRuns(ctx context.Context, agentTypeID string) ([]Run, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.ListRuns",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_type_id", agentTypeID),
		),
	)
	defer span.End()
	// Delegated identity model (Decision 6): non-admin callers see a delegated
	// agent's runs only where created_by matches their verified subject. The
	// filter is applied inside the repository query — never by trimming the
	// result in memory — so counts stay correct. Autonomous runs are unfiltered.
	runs, err := s.repo.ListRuns(ctx, tenantID, agentTypeID, runVisibilityFromContext(ctx))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list runs")
		return nil, err
	}
	// Build a slug resolver scoped to this list — at most one lookup per distinct
	// agent type, regardless of how many runs share it. Misses fail closed in
	// filterRuns, so a deleted agent type does not leak runs.
	slugCache := make(map[string]string)
	resolveSlug := func(agentTypeID string) (string, bool) {
		if slug, ok := slugCache[agentTypeID]; ok {
			return slug, slug != ""
		}
		at, err := s.atRepo.GetAgentType(ctx, agentTypeID)
		if err != nil || at == nil {
			slugCache[agentTypeID] = ""
			return "", false
		}
		slugCache[agentTypeID] = at.Slug
		return at.Slug, true
	}
	return s.authz.filterRuns(ctx, runs, resolveSlug)
}

// TerminateRun signals the session-manager to stop the subprocess and cleans up.
// Enforces tenant scoping and authz write on the run's agent type.
func (s *RunService) TerminateRun(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.TerminateRun",
		trace.WithAttributes(attribute.String("run_id", id)),
	)
	defer span.End()

	run, at, err := s.loadRunForAction(ctx, id, "write")
	if err != nil {
		return err
	}
	span.SetAttributes(attribute.String("run_state", string(run.State)))

	if run.State == RunStateActive || run.State == RunStateStarting {
		namespace := s.cfg.TenantNamespaceFn(run.TenantID)
		podURL := sessionmanager.PodURL(at.Slug, run.PodOrdinal, namespace)
		if err := s.sm.CloseSession(ctx, podURL, id); err != nil && !errors.Is(err, sessionmanager.ErrSessionNotFound) {
			// Pod may be gone (agent type deleted, pod crashed); log and proceed to DB cleanup.
			s.logger.Warn(ctx, "session-manager unreachable during termination, proceeding with DB cleanup",
				logging.Field{Key: "run_id", Value: id},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}

	// Termination is a terminal state: tombstone the run's delegation issuance
	// before the row (and its recorded JTI) is deleted.
	s.revokeRunDelegation(ctx, run)
	s.deprovisionSandbox(ctx, run)
	if err := s.repo.DeleteRun(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete run")
		return err
	}
	return nil
}

// Transition is the canonical state machine authority for run state changes.
// It updates DB state via CAS, publishes a run_state SSE event, and on terminal
// states (completed/failed) deprovisions the sandbox pod.
func (s *RunService) Transition(ctx context.Context, runID string, from, to RunState) error {
	ctx, span := agentsTracer.Start(ctx, "agents.Transition",
		trace.WithAttributes(
			attribute.String("run_id", runID),
			attribute.String("from", string(from)),
			attribute.String("to", string(to)),
		),
	)
	defer span.End()

	if err := s.repo.SetRunState(ctx, runID, from, to); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "set run state")
		return fmt.Errorf("transition run state %s→%s: %w", from, to, err)
	}
	s.logger.Info(ctx, "Run state transition",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "from", Value: string(from)},
		logging.Field{Key: "to", Value: string(to)},
	)
	payload := []byte(`{"state":"` + string(to) + `"}`)
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload})
	if err != nil {
		// Log and continue — publishing as an ephemeral (seqNo=0) event so live
		// subscribers still see the transition. The DB write failure is durable
		// state loss that we surface in traces; if we returned early here the
		// caller would believe the transition didn't happen, even though
		// SetRunState above already committed it.
		span.RecordError(err)
		s.logger.Error(ctx, "failed to persist run_state event",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "from", Value: string(from)},
			logging.Field{Key: "to", Value: string(to)},
			logging.Field{Key: "error", Value: err.Error()},
		)
		seqNo = 0
	}
	s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo})

	if to == RunStateCompleted || to == RunStateFailed || to == RunStateBudgetExhausted {
		run, err := s.repo.GetRun(ctx, runID)
		if err != nil {
			s.logger.Warn(ctx, "get run for sandbox cleanup failed",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil
		}
		// The delegation ends with the run (delegated-agent-identity ADR,
		// Decision 3): tombstone the issuance so no access survives terminal state.
		s.revokeRunDelegation(ctx, run)
		s.deprovisionSandbox(ctx, run)
	}
	return nil
}

// TransitionRunState applies a state transition signaled by the session-manager callback.
// It delegates to Transition, which is the single state machine authority.
// The name TransitionRunState is part of the session-manager HTTP callback contract.
func (s *RunService) TransitionRunState(ctx context.Context, runID string, from, to RunState) error {
	return s.Transition(ctx, runID, from, to)
}

// deprovisionSandbox deletes the sandbox pod for a run. Best-effort — logs on
// failure but never returns an error so callers are not blocked.
func (s *RunService) deprovisionSandbox(ctx context.Context, run *Run) {
	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	if err := s.k8s.DeleteSandbox(ctx, run.ID, namespace); err != nil {
		s.logger.Warn(ctx, "Sandbox pod deletion failed",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.logger.Info(ctx, "Sandbox pod deleted",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "namespace", Value: namespace},
	)
}

// PublishEvent fans out a pushed event from a session-manager pod to live SSE subscribers.
func (s *RunService) PublishEvent(_ context.Context, runID string, ev PushedEvent) {
	s.pubsub.Publish(runID, ev)
}

// ListRunEvents returns persisted events for a run after the given sequence number.
func (s *RunService) ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error) {
	return s.repo.ListRunEvents(ctx, runID, afterSeqNo)
}

// SubscribeRun registers an SSE subscriber for the given run. See PubSub.Subscribe.
func (s *RunService) SubscribeRun(runID string) (<-chan PushedEvent, func()) {
	return s.pubsub.Subscribe(runID)
}

// SendMessage relays a user message to the active session-manager for this run.
// Enforces tenant scoping and authz write on the run's agent type.
func (s *RunService) SendMessage(ctx context.Context, runID, content string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.SendMessage",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	run, at, err := s.loadRunForAction(ctx, runID, "write")
	if err != nil {
		return err
	}
	// Steer constraint (Decision 6): only the starter may message a delegated
	// run — tenant admins included, because the message executes with the
	// starter's authority under the starter's name.
	if err = checkDelegatedRunSteer(ctx, at, run); err != nil {
		return err
	}
	if run.State != RunStateActive && run.State != RunStateIdle {
		return fmt.Errorf("%w (state: %s)", ErrRunNotAcceptingMessages, run.State)
	}

	if err = s.repo.AppendRunMessage(ctx, RunMessage{RunID: runID, Content: content}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "append run message")
		return fmt.Errorf("append run message: %w", err)
	}

	// When sending to an idle run (resumed after interrupt), proactively transition
	// idle → active so the frontend sees isBusy=true and shows the stop button
	// immediately. The session-manager's disarmIdle only fires when idleTimer != nil
	// (i.e. after a full turn), so resumed sessions never get this transition otherwise.
	if run.State == RunStateIdle {
		if transErr := s.Transition(ctx, runID, RunStateIdle, RunStateActive); transErr != nil {
			s.logger.Warn(ctx, "failed to activate run before message",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: transErr.Error()},
			)
		}
	}

	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	podURL := sessionmanager.PodURL(at.Slug, run.PodOrdinal, namespace)
	if err := s.sm.SendMessage(ctx, podURL, runID, content); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "forward message to session-manager")
		return fmt.Errorf("forward message to pod %d: %w", run.PodOrdinal, err)
	}

	s.emitHumanMessage(ctx, runID, content)
	return nil
}

// tryInitialMessage sends the initial message to the session-manager and, on
// success, emits it to the event stream. The session-manager generates the run's
// task summary from this first message over its own authorized inference path
// (see internal/agents/sessionmanager/summarizer.go) and reports it back via the
// /internal/runs/{id}/summary callback. Best-effort: a send failure is logged but
// does not abort the run.
func (s *RunService) tryInitialMessage(ctx context.Context, podURL, runID string, _ *AgentType, msg string) {
	if msg == "" {
		return
	}
	if err := s.sm.SendMessage(ctx, podURL, runID, msg); err != nil {
		s.logger.Warn(ctx, "failed to send initial message",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.emitHumanMessage(ctx, runID, msg)
}

// emitHumanMessage persists a human message to run_events and fans it out via
// PubSub so live SSE subscribers and future replays both see it.
func (s *RunService) emitHumanMessage(ctx context.Context, runID, content string) {
	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return
	}
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{
		RunID:     runID,
		EventType: "human_message",
		Payload:   payload,
	})
	if err != nil {
		s.logger.Warn(ctx, "Failed to persist human_message event",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.pubsub.Publish(runID, PushedEvent{Type: "human_message", Payload: payload, SequenceNo: seqNo})
}

// InterruptRun signals the session-manager to send SIGINT to the Claude subprocess.
// Enforces tenant scoping and authz write on the run's agent type.
func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.InterruptRun",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	run, at, err := s.loadRunForAction(ctx, runID, "write")
	if err != nil {
		return err
	}
	// Interrupt steers the run (it redirects the agent mid-turn), so it follows
	// the same starter-only rule as SendMessage. Terminate does not: admins keep
	// governance termination via loadRunForAction's visibility rule alone.
	if err := checkDelegatedRunSteer(ctx, at, run); err != nil {
		return err
	}
	if run.State != RunStateActive {
		return fmt.Errorf("%w (state: %s)", ErrRunNotAcceptingMessages, run.State)
	}
	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	podURL := sessionmanager.PodURL(at.Slug, run.PodOrdinal, namespace)
	if err := s.sm.InterruptSession(ctx, podURL, runID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "interrupt session")
		return fmt.Errorf("interrupt session on pod %d: %w", run.PodOrdinal, err)
	}
	return nil
}

// AuthorizeAgentCallback verifies that the agent principal in ctx owns the
// given run before an internal callback (state transition, event push, sandbox
// provisioning) is allowed to act on it.
//
// Internal endpoints are authenticated by the agent's platform JWT, which the
// identity-proxy ext_proc sidecar injects on every outbound request from the
// agent pod (docs/adr/2026-06-04-orbit-agents-authorization-model.md). That token alone proves "an agent in this mesh", not
// "the agent that owns this run" — so this method binds the verified identity
// to the specific run: the caller must be an AGENT principal whose tenant
// matches the run's tenant and whose agent-type slug matches the run's agent
// type. Without this, any agent pod could drive another agent's runs or cross
// tenant boundaries via the otherwise-unauthenticated callback surface.
//
// Returns ErrNotFound if the run does not exist (existence is not leaked) and
// ErrForbidden on any identity mismatch.
func (s *RunService) AuthorizeAgentCallback(ctx context.Context, runID string) error {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil || claims.Orbit.PrincipalType != token.PrincipalTypeAgent {
		return ErrForbidden
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.TenantID != claims.Orbit.TenantID {
		return ErrForbidden
	}
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		return fmt.Errorf("get agent type for callback authz: %w", err)
	}
	if agentSlugFromClaims(claims) != at.Slug {
		return ErrForbidden
	}
	return nil
}

// ProvisionSandbox is called by the session-manager (via orbit-runtime-mcp) to
// request a sandbox pod for a run. Returns the gRPC endpoint ("podIP:port").
func (s *RunService) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.ProvisionSandbox",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get run")
		return "", err
	}

	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return "", fmt.Errorf("get agent type: %w", err)
	}

	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	endpoint, err := s.k8s.ProvisionSandbox(ctx, *run, at.Slug, namespace, s.sandboxKubeClusters(ctx, *at))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "provision sandbox pod")
		return "", fmt.Errorf("provision sandbox pod: %w", err)
	}

	span.SetAttributes(attribute.String("endpoint", endpoint))
	s.logger.Info(ctx, "Sandbox provisioned",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "tenant_id", Value: run.TenantID},
		logging.Field{Key: "endpoint", Value: endpoint},
	)
	return endpoint, nil
}

// providePod ensures the agent type's k8s resources exist (idempotent, fast
// no-op if already present — guards against cluster resets where the DB
// retains agent types but the StatefulSet/Service/HPA were lost) and picks the
// least-loaded pod ordinal for a new run. Errors are recorded on span.
func (s *RunService) providePod(ctx context.Context, span trace.Span, at *AgentType, namespace string) (int, error) {
	if provErr := s.k8s.ProvisionAgentType(ctx, *at, namespace); provErr != nil {
		span.RecordError(provErr)
		span.SetStatus(codes.Error, "ensure agent infrastructure")
		return 0, fmt.Errorf("ensure agent infrastructure: %w", provErr)
	}
	podOrdinal, err := s.selectPod(ctx, at)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "select pod")
		return 0, fmt.Errorf("select pod: %w", err)
	}
	return podOrdinal, nil
}

// selectPod picks the least-loaded pod ordinal for a new run.
func (s *RunService) selectPod(ctx context.Context, at *AgentType) (int, error) {
	counts, err := s.repo.ActiveRunsPerPod(ctx, at.ID)
	if err != nil {
		return 0, err
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	replicaCount, err := s.k8s.ReplicaCount(ctx, at.Slug, namespace)
	if err != nil {
		return 0, fmt.Errorf("get replica count: %w", err)
	}
	if replicaCount < 1 {
		replicaCount = 1
	}

	best, bestCount := 0, -1
	for i := range replicaCount {
		c := counts[i]
		if c >= at.MaxConcurrentRunsPerPod {
			continue
		}
		if bestCount < 0 || c < bestCount {
			best, bestCount = i, c
		}
	}
	if bestCount < 0 {
		return 0, fmt.Errorf("all pods at capacity (%d runs/pod)", at.MaxConcurrentRunsPerPod)
	}
	s.logger.Info(ctx, "Pod selected",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "pod_ordinal", Value: best},
		logging.Field{Key: "active_runs", Value: bestCount},
		logging.Field{Key: "max_runs_per_pod", Value: at.MaxConcurrentRunsPerPod},
	)
	return best, nil
}

// StartReaper runs a background goroutine that periodically marks runs stuck in
// active or starting state as failed. It exits when ctx is canceled.
func (s *RunService) StartReaper(ctx context.Context) {
	staleDuration := s.cfg.StaleRunTimeout
	if staleDuration <= 0 {
		staleDuration = 30 * time.Minute
	}
	interval := s.cfg.ReaperInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			failedIDs, completedIDs, err := s.repo.ReapStaleRuns(ctx, staleDuration)
			if err != nil {
				s.logger.Error(ctx, "stale run reaper failed", logging.Field{Key: "error", Value: err.Error()})
				continue
			}
			total := len(failedIDs) + len(completedIDs)
			if total == 0 {
				continue
			}
			s.logger.Info(ctx, "reaped stale runs",
				logging.Field{Key: "failed_count", Value: len(failedIDs)},
				logging.Field{Key: "completed_count", Value: len(completedIDs)},
				logging.Field{Key: "failed_ids", Value: failedIDs},
				logging.Field{Key: "completed_ids", Value: completedIDs},
			)
			// Emit a run_state event for each terminal transition (failed for
			// active/starting, completed for idle). The reaper bypasses Transition()
			// because ReapStaleRuns already committed the DB state atomically, but it
			// still persists+publishes the event so both live subscribers and SSE
			// replay observe the terminal state. (A bespoke "run_failed" type was not
			// handled by the frontend, leaving reaped runs stuck "busy".)
			for _, id := range failedIDs {
				s.publishReapedState(ctx, id, RunStateFailed)
			}
			for _, id := range completedIDs {
				s.publishReapedState(ctx, id, RunStateCompleted)
			}
			for _, id := range append(append([]string{}, failedIDs...), completedIDs...) {
				run, err := s.repo.GetRun(ctx, id)
				if err != nil {
					s.logger.Warn(ctx, "reaper: get run for sandbox cleanup failed",
						logging.Field{Key: "run_id", Value: id},
						logging.Field{Key: "error", Value: err.Error()},
					)
					continue
				}
				// Reaped runs reached a terminal state outside Transition —
				// tombstone their delegation issuances here too.
				s.revokeRunDelegation(ctx, run)
				s.deprovisionSandbox(ctx, run)
			}
		}
	}
}

// maybeWarmSandbox eagerly provisions the run's sandbox pod in the background when
// enabled, so the first tool call avoids cold-start latency. Best-effort and
// idempotent — the session-manager's later on-demand ProvisionSandbox reuses the
// warmed pod. The request ctx ends when StartRun returns, so we detach from its
// cancellation (keeping its trace via WithoutCancel) and cap the warm-up so a stuck
// pod cannot leak the goroutine.
func (s *RunService) maybeWarmSandbox(ctx context.Context, enabled bool, run Run, slug, namespace string, at AgentType) {
	if !enabled {
		return
	}
	// Resolve BEFORE warming, and skip the warm-up entirely if the connection
	// registry has not loaded yet. A warmed pod is reused by the run's later
	// on-demand ProvisionSandbox (it only replaces a dead pod), so a sandbox
	// warmed with an empty ORBIT_KUBE_CLUSTERS would leave the agent without a
	// kubeconfig for the whole run. Skipping simply defers provisioning to the
	// first tool call, by which point the registry is almost certainly mounted —
	// warm-up is a latency optimization, never a correctness requirement.
	kubeClusters, unresolved, err := s.resolver.KubeClusters(ctx, at.TenantID, at.Connectors)
	if err != nil || unresolved > 0 {
		s.logger.Warn(ctx, "skipping sandbox warm-up: connection registry not resolved",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "unresolved_connectors", Value: unresolved},
		)
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer cancel()
		if _, err := s.k8s.ProvisionSandbox(bgCtx, run, slug, namespace, kubeClusters); err != nil {
			s.logger.Warn(bgCtx, "pre-provision sandbox failed",
				logging.Field{Key: "run_id", Value: run.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()
}

// publishReapedState persists and publishes a run_state event for a reaper-driven
// terminal transition. ReapStaleRuns already committed the runs.state column, so
// this only records the event (for SSE replay) and notifies live subscribers.
func (s *RunService) publishReapedState(ctx context.Context, runID string, state RunState) {
	payload := []byte(`{"state":"` + string(state) + `"}`)
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload})
	if err != nil {
		s.logger.Error(ctx, "reaper: failed to persist run_state event",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "state", Value: string(state)},
			logging.Field{Key: "error", Value: err.Error()},
		)
		seqNo = 0
	}
	s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo})
}

// SetRunSummary persists the LLM-generated task summary reported by a
// session-manager pod (via the /internal/runs/{id}/summary callback). The
// session-manager generates it from the run's initial message over the agent's
// own authorized, budget-attributed inference path — the only context where the
// effective model and an inference grant both exist. The write is idempotent
// (set-once): the first summary wins, so a resumed session never overwrites it.
func (s *RunService) SetRunSummary(ctx context.Context, runID, summary string) error {
	if summary == "" {
		return nil
	}
	return s.repo.SetRunSummary(ctx, runID, summary)
}

// sandboxKubeClusters resolves the Kubernetes connections whose kubeconfig the
// sandbox materializes. Best-effort by design: a registry that has not loaded
// yet leaves the agent without a kubeconfig rather than failing the run — the
// on-demand provisioning path runs at first tool call, long after start-up.
func (s *RunService) sandboxKubeClusters(ctx context.Context, at AgentType) []kubeconfig.Cluster {
	clusters, _, err := s.resolver.KubeClusters(ctx, at.TenantID, at.Connectors)
	if err != nil {
		s.logger.Warn(ctx, "resolve kubeconfig clusters for sandbox",
			logging.Field{Key: "agent_type", Value: at.Slug},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	return clusters
}
