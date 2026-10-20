package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"

	"github.com/runkora/kora/backend/internal/agents/sandbox/kubeconfig"
)

// RunServiceConfig holds runtime parameters for RunService.
type RunServiceConfig struct {
	MaxConcurrentRunsPerPod int
	// SandboxGuardrails are the install's sandbox provisioning settings, sent
	// with every run (docs/adr/2026-09-29-agent-run-guardrails.md).
	SandboxGuardrails SandboxGuardrails
	// IdleTimeouts are the install's idle timeouts by how a run started; the
	// run's own is sent with its session.
	IdleTimeouts      IdleTimeouts
	TenantNamespaceFn func(tenantID string) string // maps tenant ID → k8s namespace
	// StaleRunTimeout is how long a run may stay pending, starting or active without a state update
	// before the background reaper marks it failed. Defaults to 30 minutes if zero.
	StaleRunTimeout time.Duration
	// ReaperInterval controls how often the reaper polls. Defaults to 5 minutes if zero.
	// Override in tests to speed up reaper cycles.
	ReaperInterval time.Duration
	// StartBudget is how long a run may stay pending, measured from its
	// created_at, before the scheduler fails it. Defaults to 3 minutes.
	StartBudget time.Duration
	// StartPollInterval is how often the scheduler claims pending runs and how
	// long a released run waits before it can be claimed again. Defaults to 1 second.
	StartPollInterval time.Duration
	// WorkerConcurrency is how many runs the scheduler starts at once. Defaults to 1.
	WorkerConcurrency int
	// DefaultEnvironmentSize sizes the built-in default environment.
	DefaultEnvironmentSize EnvironmentSize
	// SandboxMount tells the agent, in its brief, to work in the sandbox's local folder.
	SandboxMount bool
	// Brand supplies the product name for the capabilities brief and default environment.
	Brand brand.Brand
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
	envs       runEnvironmentStore
	images     sandboxImageLookup
	missing    missingProvisioner
	queue      runQueue
	// attempts and schedulerDone track the scheduler started by StartScheduler.
	attempts      sync.WaitGroup
	schedulerDone chan struct{}
}

// missingProvisioner creates an agent type's missing k8s resources without changing existing ones.
type missingProvisioner interface {
	ProvisionMissing(ctx context.Context, at AgentType, namespace string) error
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
	return at.SystemPrompt + renderCapabilitiesBrief(access, runWorkspaceDir(runID), s.cfg.SandboxMount, s.cfg.Brand.ProductName())
}

// ErrRunServiceProductNameEmpty is returned when the run service is built with no product name.
var ErrRunServiceProductNameEmpty = errors.New("run service: product name is empty")

// NewRunService creates a RunService with the given dependencies. It rejects an empty product name.
func NewRunService(
	repo RunRepository,
	atRepo AgentTypeRepository,
	sm smClient,
	k8s k8sProvisioner,
	pubsub *PubSub,
	authz *Authz,
	envs runEnvironmentStore,
	images sandboxImageLookup,
	missing missingProvisioner,
	queue runQueue,
	cfg RunServiceConfig,
	logger logging.Logger,
) (*RunService, error) {
	if strings.TrimSpace(cfg.Brand.ProductName()) == "" {
		return nil, ErrRunServiceProductNameEmpty
	}
	return &RunService{
		repo:    repo,
		atRepo:  atRepo,
		sm:      sm,
		k8s:     k8s,
		pubsub:  pubsub,
		authz:   authz,
		envs:    envs,
		images:  images,
		missing: missing,
		queue:   queue,
		cfg:     cfg,
		logger:  logger,
	}, nil
}

// authorizeStart resolves the target agent type and decides whether this caller
// may start a run of it at all: the type is theirs, its scope permits the start,
// and the identity model allows this kind of principal to initiate it.
//
// Scoped to that one question deliberately. Whether the caller may also attach a
// trigger to the run is a separate decision about a separate thing, made by
// authorizeAttribution: this one gates the action, that one gates a label on an
// action already permitted. Both run before the first write, so a refused start
// leaves nothing behind.
func (s *RunService) authorizeStart(ctx context.Context, span trace.Span, req StartRunRequest, tenantID string) (*AgentType, error) {
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
	if err = s.authorizeDelegatedStart(ctx, span, at.IsDelegated()); err != nil {
		return nil, err
	}
	return at, nil
}

// StartRun writes a pending run and returns it. A RunScheduler starts it on a
// pod (docs/adr/2026-10-12-run-scheduling.md). Tenant and created_by are
// derived from the verified platform claims in ctx; request-body values are
// ignored.
//
// Returns ErrForbidden if Authz.checkRun refuses the write: no
// agents:runs:{slug}:write, no agents:runs:delegated:write for a delegated
// agent, and no owner access. The handler maps that to 403, not 404 and not 500: a refusal is the
// caller's answer, and orbit-triggers relies on being able to tell it apart
// from a broken service — it retries a 403 while a freshly created trigger's
// grant propagates into the policy bundle, which it cannot do if a refusal
// reads as an internal error.
func (s *RunService) StartRun(ctx context.Context, req StartRunRequest) (started *Run, retErr error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.StartRun",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_type_id", req.AgentTypeID),
		),
	)
	defer span.End()

	// Once the run row exists, a later failure fails it at once and revokes a
	// delegation minted before the failure.
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

	// Two separate questions, in order: may this caller start this agent, and
	// may it also say a trigger is why.
	at, err := s.authorizeStart(ctx, span, req, tenantID)
	if err != nil {
		return nil, err
	}
	startedBy, err := s.authorizeAttribution(ctx, span, req)
	if err != nil {
		return nil, err
	}

	run := Run{
		ID:          uuid.NewString(),
		TenantID:    req.TenantID,
		AgentTypeID: req.AgentTypeID,
		State:       RunStatePending,
		PodOrdinal:  -1,
		CreatedBy:   createdBy,
		// Derived from the agent type (reads join it); carried on the response
		// so run surfaces know the governance posture without a second lookup.
		IdentityMode: at.IdentityMode.OrDefault(),
		// Nil unless a trigger started this run, which is what its absence means
		// everywhere downstream.
		StartedBy:      startedBy,
		InitialMessage: req.InitialMessage,
	}
	span.SetAttributes(attribute.String("run_id", run.ID))
	if startedBy != nil {
		span.SetAttributes(attribute.String("trigger_id", startedBy.TriggerID))
	}

	if err := s.repo.CreateRun(ctx, run); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create run")
		return nil, fmt.Errorf("create run: %w", err)
	}
	createdRunID = run.ID

	// Mint the run's delegation from the CALLER's own bearer token (ADR Leg 1):
	// identity never mints delegated authority the caller does not hold, and the
	// response carries only the issuance record — the token never transits
	// orbit-agents. Mint failure fails the run start (fail closed). The caller's
	// token exists only in this request, so the mint happens here.
	if at.IsDelegated() {
		jti, mintErr := s.mintRunDelegation(ctx, span, run.ID, *at, s.cfg.TenantNamespaceFn(at.TenantID))
		if mintErr != nil {
			return nil, mintErr
		}
		mintedJTI = jti
		run.DelegationJTI = &jti
	}

	// Queued last, so the scheduler never claims a run whose delegation is not yet stored.
	if err := s.queue.QueueRun(ctx, run.ID, time.Now().UTC()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "queue run")
		return nil, fmt.Errorf("queue run: %w", err)
	}

	s.logger.Info(ctx, "Run queued",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "agent_type_id", Value: run.AgentTypeID},
	)
	// The stored row carries the timestamps the caller shows.
	if stored, err := s.repo.GetRun(ctx, run.ID); err == nil {
		return stored, nil
	}
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
	if action != "read" || !readsOwnRuns(ctx) || !isRunStarter(ctx, run) {
		if err = s.authz.checkRun(ctx, *at, action); err != nil {
			if errors.Is(err, ErrForbidden) {
				return nil, nil, ErrNotFound
			}
			return nil, nil, err
		}
	}
	// The delegated:read check runs only for a delegated run the caller did not start.
	seeAll := false
	if at.IdentityMode.OrDefault() == IdentityModeDelegated && !isRunStarter(ctx, run) {
		if seeAll, err = s.authz.canReadDelegatedRuns(ctx); err != nil {
			return nil, nil, err
		}
	}
	if err = checkDelegatedRunVisibility(ctx, at, run, seeAll); err != nil {
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

// ListRuns returns a page of the runs the caller may read, newest first, with both visibility rules applied in SQL.
func (s *RunService) ListRuns(ctx context.Context, agentTypeID string, p RunListQuery) (RunPage, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.ListRuns",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_type_id", agentTypeID),
			attribute.Int("limit", p.Limit),
		),
	)
	defer span.End()
	page, err := s.listRuns(ctx, tenantID, agentTypeID, p)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list runs")
	}
	return page, err
}

func (s *RunService) listRuns(ctx context.Context, tenantID, agentTypeID string, p RunListQuery) (RunPage, error) {
	scope, err := s.readableRunScope(ctx, tenantID)
	if err != nil {
		return RunPage{}, err
	}
	scope.AgentTypeID = agentTypeID
	q := p
	q.Scope = scope
	// One row past the page says whether another page follows.
	q.Limit = p.Limit + 1
	runs, err := s.repo.ListRuns(ctx, tenantID, q)
	if err != nil {
		return RunPage{}, fmt.Errorf("list runs: %w", err)
	}
	page := RunPage{Runs: runs}
	if len(runs) > p.Limit {
		page.Runs = runs[:p.Limit]
		last := page.Runs[len(page.Runs)-1]
		page.NextCursor = runCursor{CreatedAt: last.CreatedAt, ID: last.ID}.encode()
	}
	return page, nil
}

// readableRunScope is every agent type in the tenant whose runs the caller may
// read, plus the caller's own runs (readsOwnRuns), under the caller's delegated-run visibility.
func (s *RunService) readableRunScope(ctx context.Context, tenantID string) (RunScope, error) {
	ats, err := s.atRepo.ListAgentTypes(ctx, tenantID)
	if err != nil {
		return RunScope{}, fmt.Errorf("list agent types: %w", err)
	}
	ids, err := s.authz.readableRunAgentTypes(ctx, ats)
	if err != nil {
		return RunScope{}, err
	}
	vis, err := s.runVisibility(ctx)
	if err != nil {
		return RunScope{}, err
	}
	return RunScope{AgentTypeIDs: ids, Vis: vis, Own: readsOwnRuns(ctx)}, nil
}

// RunOutcomes counts the caller's visible completed and failed runs in [from, to).
func (s *RunService) RunOutcomes(ctx context.Context, from, to time.Time) (RunOutcomes, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.RunOutcomes",
		trace.WithAttributes(attribute.String("tenant_id", tenantID)),
	)
	defer span.End()
	scope, err := s.readableRunScope(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "run scope")
		return RunOutcomes{}, err
	}
	out, err := s.repo.CountRunOutcomes(ctx, tenantID, scope, from, to)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "count run outcomes")
	}
	return out, err
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

	// An idle run still holds its session, and a draining pod waits for it.
	if run.State == RunStateActive || run.State == RunStateStarting || run.State == RunStateIdle {
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
	at := eventTime()
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload, CreatedAt: at})
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
	s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo, At: at})

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
//
// idle to active is idempotent: SendMessage already makes it for a user
// message, and the session-manager, which cannot tell a message turn from a
// turn the agent started itself, reports it again when the turn begins.
func (s *RunService) TransitionRunState(ctx context.Context, runID string, from, to RunState) error {
	err := s.Transition(ctx, runID, from, to)
	if errors.Is(err, ErrConflict) && from == RunStateIdle && to == RunStateActive {
		if run, getErr := s.repo.GetRun(ctx, runID); getErr == nil && run.State == RunStateActive {
			return nil
		}
	}
	return err
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

// PublishEvent persists a pushed event from a session-manager pod and fans it
// out to live SSE subscribers. Durable types are written to run_events and
// carry the assigned sequence number; replay-only types are persisted and
// withheld from subscribers, who receive the same content through the delta
// events. A failed write is returned so the pushing session-manager learns the
// event was not recorded.
func (s *RunService) PublishEvent(ctx context.Context, runID string, ev PushedEvent) error {
	var writeErr error
	ev.At = eventTime()
	if durableEventType(ev.Type) {
		seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: ev.Type, Payload: ev.Payload, CreatedAt: ev.At})
		if err != nil {
			// Fan out anyway, unsequenced, and report the failure. Subscribers keep
			// seeing the run while the database is down, and the pusher learns the
			// row is missing. Same shape as a failed run_state write.
			writeErr = fmt.Errorf("persist %s event: %w", ev.Type, err)
		} else {
			ev.SequenceNo = seqNo
		}
	}
	if !replayOnlyEventType(ev.Type) {
		s.pubsub.Publish(runID, ev)
	}
	return writeErr
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
	if run.State == RunStatePending || run.State == RunStateStarting {
		return ErrRunStarting
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
	// A person's message makes the turn manual: a trigger run takes the manual idle timeout from here on.
	if err := s.sm.SendMessage(ctx, podURL, runID, content, s.cfg.IdleTimeouts.ForPersonMessage(run.StartedBy), true); err != nil {
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
func (s *RunService) tryInitialMessage(ctx context.Context, podURL, runID, msg string) {
	if msg == "" {
		return
	}
	// The first message is the run's task, not a person's turn: the timeout set at creation stands.
	if err := s.sm.SendMessage(ctx, podURL, runID, msg, 0, false); err != nil {
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
	at := eventTime()
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{
		RunID:     runID,
		EventType: "human_message",
		Payload:   payload,
		CreatedAt: at,
	})
	if err != nil {
		s.logger.Warn(ctx, "Failed to persist human_message event",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	s.pubsub.Publish(runID, PushedEvent{Type: "human_message", Payload: payload, SequenceNo: seqNo, At: at})
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
	// the same starter-only rule as SendMessage. Terminate does not: it follows
	// the visibility rule in loadRunForAction.
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
	if !ok || claims == nil || claims.Kora.PrincipalType != token.PrincipalTypeAgent {
		return ErrForbidden
	}
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.TenantID != claims.Kora.TenantID {
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

// ErrNoSandbox reports that a run has no sandbox. Distinct from a lookup
// failure: the caller routes work locally for the former and refuses for the
// latter.
var ErrNoSandbox = errors.New("run has no sandbox")

// GetSandbox returns the run's sandbox without creating one. The session-manager
// asks when its own memory has none, which happens after a restart, and the
// answer comes from the live pod rather than the run's workspace, which the
// agent can delete.
func (s *RunService) GetSandbox(ctx context.Context, runID string) (Sandbox, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetSandbox",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get run")
		return Sandbox{}, err
	}

	sandbox, ok, err := s.k8s.LookupSandbox(ctx, runID, s.cfg.TenantNamespaceFn(run.TenantID))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "lookup sandbox")
		return Sandbox{}, err
	}
	if !ok {
		return Sandbox{}, ErrNoSandbox
	}
	span.SetAttributes(attribute.String("endpoint", sandbox.Endpoint))
	return sandbox, nil
}

// ProvisionSandbox is called by the session-manager (via orbit-runtime-mcp) to
// request a sandbox pod for a run. Returns the gRPC endpoint ("podIP:port") and
// the exec token that sandbox requires.
//
// An empty environment keeps the run's current one, or the agent's default before the first sandbox.
// replace switches a live sandbox to the named environment and ends its commands; a pod already in it is reused.
func (s *RunService) ProvisionSandbox(ctx context.Context, runID string, environment string, replace bool) (Sandbox, error) {
	environment = strings.TrimSpace(environment)
	ctx, span := agentsTracer.Start(ctx, "agents.ProvisionSandbox",
		trace.WithAttributes(
			attribute.String("run_id", runID),
			attribute.String("environment", environment),
			attribute.Bool("replace", replace),
		),
	)
	defer span.End()

	if replace && environment == "" {
		return Sandbox{}, failSpan(span, errResolveEnvironment, ErrEnvironmentRequired)
	}
	run, at, err := s.runAndAgent(ctx, runID)
	if err != nil {
		return Sandbox{}, failSpan(span, errLoadRun, err)
	}

	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	// No kubeconfig beats no sandbox, so an unresolved registry still provisions.
	kubeClusters, _ := s.sandboxKubeClusters(ctx, *at)

	spec, switching, err := s.chooseSandbox(ctx, span, *run, *at, namespace, environment, replace)
	if err != nil {
		return Sandbox{}, err
	}
	spec.KubeClusters = kubeClusters

	if switching {
		if err = s.k8s.DeleteSandbox(ctx, runID, namespace); err != nil {
			return Sandbox{}, failSpan(span, errProvisionPod, fmt.Errorf("delete sandbox: %w", err))
		}
		// The target is recorded before the start to keep a failed start on the requested environment.
		if err = s.envs.SetRunEnvironment(ctx, runID, environmentIDPtr(spec.EnvironmentID)); err != nil {
			return Sandbox{}, failSpan(span, errProvisionPod, fmt.Errorf("record environment: %w", err))
		}
	}
	sandbox, err := s.k8s.ProvisionSandbox(ctx, *run, at.Slug, namespace, spec)
	if err != nil {
		return Sandbox{}, failSpan(span, errProvisionPod, fmt.Errorf("provision sandbox pod: %w", err))
	}
	// A retry finds the pod an earlier attempt started but did not record.
	if sandbox.Created || recordedEnvironment(*run) != spec.EnvironmentID {
		action := "start"
		if replace {
			action = "replace"
		}
		s.recordSandboxEnvironment(ctx, runID, action, spec)
	}

	sandbox.CanaryHost = s.egressCanaryHost(ctx, *at)

	span.SetAttributes(attribute.String("endpoint", sandbox.Endpoint))
	s.logger.Info(ctx, "Sandbox provisioned",
		logging.Field{Key: "run_id", Value: runID},
		logging.Field{Key: "tenant_id", Value: run.TenantID},
		logging.Field{Key: "environment", Value: spec.Environment},
		logging.Field{Key: "endpoint", Value: sandbox.Endpoint},
	)
	return sandbox, nil
}

// chooseSandbox resolves the environment and spec of the run's next sandbox.
// switching reports a live sandbox in another environment that a replacement removes first.
func (s *RunService) chooseSandbox(ctx context.Context, span trace.Span, run Run, at AgentType, namespace, environment string, replace bool) (spec SandboxSpec, switching bool, err error) {
	envs, err := s.agentEnvironments(ctx, at)
	if err != nil {
		return SandboxSpec{}, false, failSpan(span, errResolveEnvironment, err)
	}
	current, live, err := s.currentEnvironment(ctx, run, namespace)
	if err != nil {
		return SandboxSpec{}, false, failSpan(span, errResolveEnvironment, err)
	}
	env, err := pickEnvironment(envs, environment, current)
	if err != nil {
		return SandboxSpec{}, false, failSpan(span, errResolveEnvironment, err)
	}
	switching = live && env.ID != current
	if switching && !replace {
		return SandboxSpec{}, false, failSpan(span, errResolveEnvironment, fmt.Errorf("%w; call replace_environment to switch to %q", ErrEnvironmentSwitch, env.Slug))
	}
	spec, err = s.sandboxSpec(ctx, namespace, env)
	if err != nil {
		return SandboxSpec{}, false, failSpan(span, errResolveSandboxImage, err)
	}
	return spec, switching, nil
}

// egressCanaryHost picks a host this agent may reach, so the egress probe tests
// the agent's own path. Open egress with no named host probes a fixed host; no
// egress returns empty, which skips the probe.
func (s *RunService) egressCanaryHost(ctx context.Context, at AgentType) string {
	if s.resolver == nil {
		return ""
	}
	access, err := s.resolver.EffectiveAccess(ctx, at)
	if err != nil || access == nil {
		s.logger.Warn(ctx, "Resolving the egress canary host failed; the sandbox skips the probe",
			logging.Field{Key: "agent_type", Value: at.Slug},
			logging.Field{Key: "error", Value: fmt.Sprint(err)},
		)
		return ""
	}
	// Connector hosts first: adapters declare HTTPS APIs, while a whitelisted
	// host may serve something other than HTTPS.
	var hosts []string
	for _, ce := range access.ConnectorEgress {
		hosts = append(hosts, ce.Hosts...)
	}
	hosts = append(hosts, access.EnvironmentHosts...)
	hosts = append(hosts, access.OwnHosts...)
	for _, h := range hosts {
		if !strings.Contains(h, "*") {
			return h
		}
	}
	if access.Open {
		return openEgressCanaryHost
	}
	return ""
}

// openEgressCanaryHost is probed for open-egress agents that name no host: any
// host is allowed, and this one reliably answers HTTPS.
const openEgressCanaryHost = "api.github.com"

// isRetryableSessionError reports whether err means the pod was unreachable or
// not yet serving, as opposed to a request the pod rejected.
func isRetryableSessionError(err error) bool {
	if errors.Is(err, sessionmanager.ErrSessionConflict) {
		return false
	}
	var statusErr *sessionmanager.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusServiceUnavailable
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, syscall.ECONNREFUSED)
}

// StartReaper runs a background goroutine that periodically marks runs stuck in
// pending, starting or active as failed. It exits when ctx is canceled.
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

// publishReapedState persists and publishes a run_state event for a reaper-driven
// terminal transition. ReapStaleRuns already committed the runs.state column, so
// this only records the event (for SSE replay) and notifies live subscribers.
func (s *RunService) publishReapedState(ctx context.Context, runID string, state RunState) {
	payload := []byte(`{"state":"` + string(state) + `"}`)
	at := eventTime()
	seqNo, err := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload, CreatedAt: at})
	if err != nil {
		s.logger.Error(ctx, "reaper: failed to persist run_state event",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "state", Value: string(state)},
			logging.Field{Key: "error", Value: err.Error()},
		)
		seqNo = 0
	}
	s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo, At: at})
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
// sandbox materializes, and reports whether that answer is complete.
//
// ready is false when the connection registry has not loaded, which
// KubeClusters reports as unresolved > 0 and is a different answer from "this
// agent has no Kubernetes connection". The sandbox pod is reused for the whole
// run rather than rebuilt, so an incomplete answer persists.
func (s *RunService) sandboxKubeClusters(ctx context.Context, at AgentType) (clusters []kubeconfig.Cluster, ready bool) {
	clusters, unresolved, err := s.resolver.KubeClusters(ctx, at.TenantID, at.Connectors)
	if err != nil {
		s.logger.Warn(ctx, "resolve kubeconfig clusters for sandbox",
			logging.Field{Key: "agent_type", Value: at.Slug},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil, false
	}
	if unresolved > 0 {
		s.logger.Warn(ctx, "connection registry has not resolved every connector for this sandbox",
			logging.Field{Key: "agent_type", Value: at.Slug},
			logging.Field{Key: "unresolved_connectors", Value: unresolved},
		)
		return clusters, false
	}
	return clusters, true
}
