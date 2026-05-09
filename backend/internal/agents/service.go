package agents

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/google/uuid"
)

var agentsTracer = otel.Tracer("orbit.agents")

// Service orchestrates agent type and run lifecycle.
type Service struct {
	repo          Repository
	k8s           *K8sProvisioner
	sm            *sessionmanager.Client
	pubsub        *PubSub
	logger        logging.Logger
	cfg           ServiceConfig
	agentTypeHook func() // called after agent type create/delete; nil if unset
}

// SetAgentTypeHook installs a callback that is invoked (synchronously, before
// returning) after any agent type is created or deleted. Intended for scope
// re-registration — the hook should launch its own goroutine if needed.
func (s *Service) SetAgentTypeHook(fn func()) { s.agentTypeHook = fn }

// ServiceConfig holds runtime parameters for the service layer.
type ServiceConfig struct {
	MaxConcurrentRunsPerPod int
	TenantNamespaceFn       func(tenantID string) string // maps tenant ID → k8s namespace
}

// NewService creates a Service with the given dependencies.
func NewService(repo Repository, k8s *K8sProvisioner, sm *sessionmanager.Client, pubsub *PubSub, logger logging.Logger, cfg ServiceConfig) *Service {
	return &Service{repo: repo, k8s: k8s, sm: sm, pubsub: pubsub, logger: logger, cfg: cfg}
}

// ── Agent Types ───────────────────────────────────────────────────────────────

// CreateAgentType registers a new agent type and initiates k8s provisioning.
func (s *Service) CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.CreateAgentType",
		trace.WithAttributes(
			attribute.String("tenant_id", req.TenantID),
			attribute.String("slug", req.Slug),
		),
	)
	defer span.End()

	maxRuns := req.MaxConcurrentRunsPerPod
	if maxRuns <= 0 {
		maxRuns = s.cfg.MaxConcurrentRunsPerPod
	}
	framework := req.Framework
	if framework == "" {
		framework = AgentFrameworkClaudeCode
	}

	at := AgentType{
		ID:                      uuid.NewString(),
		TenantID:                req.TenantID,
		Name:                    req.Name,
		Slug:                    req.Slug,
		Description:             req.Description,
		SystemPrompt:            req.SystemPrompt,
		Framework:               framework,
		ToolScopes:              req.ToolScopes,
		GrantScopes:             req.GrantScopes,
		MaxConcurrentRunsPerPod: maxRuns,
		State:                   AgentTypeStateProvisioning,
	}

	if err := s.repo.CreateAgentType(ctx, at); err != nil {
		if errors.Is(err, ErrSlugTaken) {
			return nil, ErrSlugTaken
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "create agent type")
		return nil, fmt.Errorf("create agent type: %w", err)
	}
	span.SetAttributes(attribute.String("agent_type_id", at.ID))

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if err := s.k8s.ProvisionAgentType(ctx, at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "provision k8s resources")
		return nil, fmt.Errorf("provision agent type k8s resources: %w", err)
	}
	if err := s.repo.SetAgentTypeState(ctx, at.ID, AgentTypeStateProvisioning, AgentTypeStateActive); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "set agent type active")
		return nil, fmt.Errorf("set agent type active: %w", err)
	}
	at.State = AgentTypeStateActive

	s.logger.Info(ctx, "Agent type created",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "tenant_id", Value: at.TenantID},
		logging.Field{Key: "slug", Value: at.Slug},
	)
	if s.agentTypeHook != nil {
		s.agentTypeHook()
	}
	return &at, nil
}

func (s *Service) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetAgentType",
		trace.WithAttributes(attribute.String("agent_type_id", id)),
	)
	defer span.End()
	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
	}
	return at, err
}

func (s *Service) ListAgentTypes(ctx context.Context, tenantID string) ([]AgentType, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.ListAgentTypes",
		trace.WithAttributes(attribute.String("tenant_id", tenantID)),
	)
	defer span.End()
	types, err := s.repo.ListAgentTypes(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list agent types")
	}
	return types, err
}

// DeleteAgentType initiates teardown of a managed agent type.
func (s *Service) DeleteAgentType(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.DeleteAgentType",
		trace.WithAttributes(attribute.String("agent_type_id", id)),
	)
	defer span.End()

	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return fmt.Errorf("get agent type: %w", err)
	}
	span.SetAttributes(attribute.String("slug", at.Slug))

	// Allow retrying a stuck deletion; skip state transition if already deleting.
	if at.State != AgentTypeStateDeleting {
		if err := s.repo.SetAgentTypeState(ctx, id, AgentTypeStateActive, AgentTypeStateDeleting); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "set agent type deleting")
			return fmt.Errorf("set agent type deleting: %w", err)
		}
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if err := s.k8s.DeprovisionAgentType(ctx, *at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "deprovision k8s resources")
		return fmt.Errorf("deprovision agent type k8s resources: %w", err)
	}

	if err := s.repo.DeleteAgentType(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete agent type")
		return err
	}
	if s.agentTypeHook != nil {
		s.agentTypeHook()
	}
	return nil
}

// ── Runs ──────────────────────────────────────────────────────────────────────

// StartRun creates a new run and assigns it to a pod.
func (s *Service) StartRun(ctx context.Context, req StartRunRequest) (*Run, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.StartRun",
		trace.WithAttributes(
			attribute.String("tenant_id", req.TenantID),
			attribute.String("agent_type_id", req.AgentTypeID),
		),
	)
	defer span.End()

	at, err := s.repo.GetAgentType(ctx, req.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return nil, fmt.Errorf("get agent type: %w", err)
	}
	if at.TenantID != req.TenantID {
		return nil, ErrNotFound
	}

	// Ensure k8s resources exist — idempotent, fast no-op if already present.
	// Guards against cluster resets where the DB retains agent types but the
	// StatefulSet/Service/HPA were lost.
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if provErr := s.k8s.ProvisionAgentType(ctx, *at, namespace); provErr != nil {
		span.RecordError(provErr)
		span.SetStatus(codes.Error, "ensure agent infrastructure")
		return nil, fmt.Errorf("ensure agent infrastructure: %w", provErr)
	}

	podOrdinal, err := s.selectPod(ctx, at)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "select pod")
		return nil, fmt.Errorf("select pod: %w", err)
	}

	run := Run{
		ID:          uuid.NewString(),
		TenantID:    req.TenantID,
		AgentTypeID: req.AgentTypeID,
		State:       RunStatePending,
		CreatedBy:   req.CreatedBy,
	}
	span.SetAttributes(attribute.String("run_id", run.ID))

	if err := s.repo.CreateRun(ctx, run); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "create run")
		return nil, fmt.Errorf("create run: %w", err)
	}
	if err := s.repo.AssignPod(ctx, run.ID, podOrdinal); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "assign pod")
		return nil, fmt.Errorf("assign pod: %w", err)
	}
	run.State = RunStateStarting
	run.PodOrdinal = podOrdinal

	podURL := sessionmanager.PodURL(at.Slug, podOrdinal, namespace)
	smReq := sessionmanager.CreateSessionRequest{
		RunID:        run.ID,
		AgentTypeID:  run.AgentTypeID,
		TenantID:     run.TenantID,
		AllowedTools: at.ToolScopes,
		SystemPrompt: at.SystemPrompt,
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

	s.logger.Info(ctx, "Run started",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "agent_type_id", Value: run.AgentTypeID},
		logging.Field{Key: "pod_ordinal", Value: run.PodOrdinal},
	)
	return &run, nil
}

func (s *Service) GetRun(ctx context.Context, id string) (*Run, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetRun",
		trace.WithAttributes(attribute.String("run_id", id)),
	)
	defer span.End()
	run, err := s.repo.GetRun(ctx, id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get run")
	}
	return run, err
}

func (s *Service) ListRuns(ctx context.Context, tenantID, agentTypeID string) ([]Run, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.ListRuns",
		trace.WithAttributes(
			attribute.String("tenant_id", tenantID),
			attribute.String("agent_type_id", agentTypeID),
		),
	)
	defer span.End()
	runs, err := s.repo.ListRuns(ctx, tenantID, agentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list runs")
	}
	return runs, err
}

// TerminateRun signals the session-manager to stop the subprocess and cleans up.
func (s *Service) TerminateRun(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.TerminateRun",
		trace.WithAttributes(attribute.String("run_id", id)),
	)
	defer span.End()

	run, err := s.repo.GetRun(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get run")
		return err
	}
	span.SetAttributes(attribute.String("run_state", string(run.State)))

	if run.State == RunStateActive || run.State == RunStateStarting {
		at, err := s.repo.GetAgentType(ctx, run.AgentTypeID)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "get agent type")
			return fmt.Errorf("get agent type: %w", err)
		}
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

	s.deprovisionSandbox(ctx, run)
	if err := s.repo.DeleteRun(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete run")
		return err
	}
	return nil
}

// TransitionRunState applies a state transition signaled by the session-manager callback.
// It also persists a run_state event and pushes it to live SSE subscribers.
func (s *Service) TransitionRunState(ctx context.Context, runID string, from, to RunState) error {
	ctx, span := agentsTracer.Start(ctx, "agents.TransitionRunState",
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
	seqNo, _ := s.repo.AppendRunEvent(ctx, RunEvent{RunID: runID, EventType: "run_state", Payload: payload})
	s.pubsub.Publish(runID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo})

	if to == RunStateCompleted || to == RunStateFailed {
		run, err := s.repo.GetRun(ctx, runID)
		if err != nil {
			s.logger.Warn(ctx, "get run for sandbox cleanup failed",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			return nil
		}
		s.deprovisionSandbox(ctx, run)
	}
	return nil
}

// deprovisionSandbox deletes the sandbox pod for a run. Best-effort — logs on
// failure but never returns an error so callers are not blocked.
func (s *Service) deprovisionSandbox(ctx context.Context, run *Run) {
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
// Durable events (message_complete, tool_use, tool_result) are written to the DB by the
// session-manager before this is called; orbit-agents skips re-writing them and only fans
// out to SSE. Ephemeral events (text_delta, thinking_delta, message_stop) are push-only.
func (s *Service) PublishEvent(ctx context.Context, runID string, ev PushedEvent) {
	s.pubsub.Publish(runID, ev)
}

// ListRunEvents returns persisted events for a run after the given sequence number.
func (s *Service) ListRunEvents(ctx context.Context, runID string, afterSeqNo int64) ([]RunEvent, error) {
	return s.repo.ListRunEvents(ctx, runID, afterSeqNo)
}

// SubscribeRun registers an SSE subscriber for the given run. See PubSub.Subscribe.
func (s *Service) SubscribeRun(runID string) (<-chan PushedEvent, func()) {
	return s.pubsub.Subscribe(runID)
}

// SendMessage relays a user message to the active session-manager for this run.
func (s *Service) SendMessage(ctx context.Context, runID, content string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.SendMessage",
		trace.WithAttributes(attribute.String("run_id", runID)),
	)
	defer span.End()

	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get run")
		return err
	}
	if run.State != RunStateActive {
		return fmt.Errorf("run is not active (state: %s)", run.State)
	}

	if err = s.repo.AppendRunMessage(ctx, RunMessage{RunID: runID, Content: content}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "append run message")
		return fmt.Errorf("append run message: %w", err)
	}

	at, err := s.repo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return fmt.Errorf("get agent type: %w", err)
	}
	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	podURL := sessionmanager.PodURL(at.Slug, run.PodOrdinal, namespace)
	if err := s.sm.SendMessage(ctx, podURL, runID, content); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "forward message to session-manager")
		return fmt.Errorf("forward message to pod %d: %w", run.PodOrdinal, err)
	}

	return nil
}

// ProvisionSandbox is called by the session-manager (via orbit-runtime-mcp) to
// request a sandbox pod for a run. Returns the gRPC endpoint ("podIP:port").
// The session-manager writes this to $WORKSPACE/.sandbox/endpoint so orbit-shell
// routes subsequent Bash commands to the sandbox.
func (s *Service) ProvisionSandbox(ctx context.Context, runID string) (string, error) {
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

	at, err := s.repo.GetAgentType(ctx, run.AgentTypeID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return "", fmt.Errorf("get agent type: %w", err)
	}

	namespace := s.cfg.TenantNamespaceFn(run.TenantID)
	endpoint, err := s.k8s.ProvisionSandbox(ctx, *run, at.Slug, namespace)
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

// ── Pod selection ─────────────────────────────────────────────────────────────

// selectPod picks the least-loaded pod ordinal for a new run.
func (s *Service) selectPod(ctx context.Context, at *AgentType) (int, error) {
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

// ── Request types ─────────────────────────────────────────────────────────────

type CreateAgentTypeRequest struct {
	TenantID                string         `json:"tenant_id"`
	Name                    string         `json:"name"`
	Slug                    string         `json:"slug"`
	Description             string         `json:"description"`
	SystemPrompt            string         `json:"system_prompt"`
	Framework               AgentFramework `json:"framework"`
	ToolScopes              []string       `json:"tool_scopes"`
	GrantScopes             []string       `json:"grant_scopes"`
	MaxConcurrentRunsPerPod int            `json:"max_concurrent_runs_per_pod"`
}

type StartRunRequest struct {
	TenantID    string `json:"tenant_id"`
	AgentTypeID string `json:"agent_type_id"`
	CreatedBy   string `json:"created_by"`
}
