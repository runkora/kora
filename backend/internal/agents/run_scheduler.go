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

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const (
	defaultStartBudget = 3 * time.Minute
	// defaultProvisionBudget is how long a run may wait for its agent to finish provisioning.
	defaultProvisionBudget   = 10 * time.Minute
	defaultStartPollInterval = time.Second
	// startAttemptTimeout bounds one attempt to start a claimed run.
	startAttemptTimeout = 30 * time.Second
	// staleRunClaimWindow outlasts an attempt, so only a dead worker's claim goes stale.
	staleRunClaimWindow = 2 * startAttemptTimeout
	// reclaimEvery is how many polls pass between stale-claim sweeps.
	reclaimEvery = 10
)

// Span statuses of a start attempt that returned its run to the queue or failed it.
var (
	errStartReleased = errors.New("run start released")
	errStartFailed   = errors.New("run start failed")
)

// runQueue is the run queue the scheduler and StartRun use.
type runQueue interface {
	QueueRun(ctx context.Context, runID string, notBefore time.Time) error
	ClaimPendingRuns(ctx context.Context, limit int, now time.Time) ([]Run, error)
	ReleaseRunClaim(ctx context.Context, id string, claimedAt, notBefore time.Time) error
	ReclaimStaleRunClaims(ctx context.Context, cutoff time.Time) ([]string, error)
}

// StartScheduler starts pending runs on this replica until ctx ends
// (docs/adr/2026-10-12-run-scheduling.md). WaitScheduler waits for it to stop.
func (s *RunService) StartScheduler(ctx context.Context) {
	done := make(chan struct{})
	s.schedulerDone = done
	slots := make(chan struct{}, max(s.cfg.WorkerConcurrency, 1))
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.pollInterval())
		defer ticker.Stop()
		for tick := 0; ; tick++ {
			select {
			case <-ctx.Done():
				s.attempts.Wait()
				return
			case <-ticker.C:
			}
			if tick%reclaimEvery == 0 {
				s.reclaimStale(ctx)
			}
			s.pollQueue(ctx, slots)
		}
	}()
}

// WaitScheduler blocks until the scheduler and its attempts in flight have stopped, or ctx ends.
func (s *RunService) WaitScheduler(ctx context.Context) {
	if s.schedulerDone == nil {
		return
	}
	select {
	case <-s.schedulerDone:
	case <-ctx.Done():
	}
}

// pollQueue claims as many runs as slots has free and starts each in its own goroutine.
func (s *RunService) pollQueue(ctx context.Context, slots chan struct{}) {
	free := cap(slots) - len(slots)
	if free == 0 {
		return
	}
	now := time.Now()
	// A claim that commits must be read, or its runs wait out the stale window.
	runs, err := s.queue.ClaimPendingRuns(context.WithoutCancel(ctx), free, now)
	if err != nil {
		s.logger.Error(ctx, "claim pending runs failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, run := range runs {
		slots <- struct{}{}
		s.attempts.Go(func() {
			defer func() { <-slots }()
			// Shutdown does not cut an attempt short. A half-made start would leave the run in starting.
			attemptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), startAttemptTimeout)
			defer cancel()
			s.startClaimed(attemptCtx, run, now)
		})
	}
}

// reclaimStale releases claims that a dead worker left on pending runs.
func (s *RunService) reclaimStale(ctx context.Context) {
	ids, err := s.queue.ReclaimStaleRunClaims(ctx, time.Now().Add(-staleRunClaimWindow))
	if err != nil {
		s.logger.Error(ctx, "reclaim stale run claims failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, id := range ids {
		s.logger.Warn(ctx, "reclaimed a run whose worker never started it", logging.Field{Key: "run_id", Value: id})
	}
}

func (s *RunService) startBudget() time.Duration {
	if s.cfg.StartBudget > 0 {
		return s.cfg.StartBudget
	}
	return defaultStartBudget
}

func (s *RunService) provisionBudget() time.Duration {
	if s.cfg.ProvisionBudget > 0 {
		return s.cfg.ProvisionBudget
	}
	return defaultProvisionBudget
}

// pollInterval is how often the scheduler claims runs and how long a released run waits.
func (s *RunService) pollInterval() time.Duration {
	if s.cfg.StartPollInterval > 0 {
		return s.cfg.StartPollInterval
	}
	return defaultStartPollInterval
}

// startClaimed makes one attempt to start a run claimed at claimedAt. It never waits for a pod.
// A transient failure returns the run to the queue, and a run past its start or provisioning budget fails.
func (s *RunService) startClaimed(ctx context.Context, run Run, claimedAt time.Time) {
	ctx, span := agentsTracer.Start(ctx, "agents.startClaimed",
		trace.WithAttributes(attribute.String("run_id", run.ID), attribute.String("agent_type_id", run.AgentTypeID)),
	)
	defer span.End()

	at, namespace, pod, ok := s.placeRun(ctx, span, run, claimedAt)
	if !ok {
		return
	}

	podURL := sessionmanager.PodURL(at.Slug, pod, namespace)
	err := s.sm.CreateSession(ctx, podURL, s.sessionRequest(ctx, run, *at))
	switch {
	case err == nil, errors.Is(err, sessionmanager.ErrSessionConflict):
		// Sessions are keyed by run ID, so a conflict means an earlier attempt landed.
	case isRetryableSessionError(err) || ctx.Err() != nil:
		s.closeSession(ctx, podURL, run.ID)
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("create session on pod %d: %w", pod, err))
		return
	default:
		span.RecordError(err)
		s.closeSession(ctx, podURL, run.ID)
		s.failStart(ctx, span, run.ID, RunStateStarting, "The agent pod refused the run.")
		return
	}
	// The session exists now, so the attempt timeout must not strand the run in starting.
	if err = s.Transition(context.WithoutCancel(ctx), run.ID, RunStateStarting, RunStateActive); err != nil && !s.runIsActive(ctx, run.ID) {
		closed := s.closeSession(ctx, podURL, run.ID)
		switch {
		case errors.Is(err, ErrConflict), errors.Is(err, ErrNotFound):
			// Terminated while the session was created.
		case closed:
			s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("set run active: %w", err))
		default:
			// A retry on another pod would leave two live sessions for one run.
			s.failStart(ctx, span, run.ID, RunStateStarting, "The agent pod did not confirm the run.")
		}
		return
	}
	s.tryInitialMessage(ctx, podURL, run.ID, run.InitialMessage)
	s.logger.Info(ctx, "Run started",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "agent_type_id", Value: run.AgentTypeID},
		logging.Field{Key: "pod_ordinal", Value: pod},
	)
}

// placeRun checks the run's agent, picks a pod and assigns it under the claim.
// It reports false when the run was released, failed or taken by another worker.
func (s *RunService) placeRun(ctx context.Context, span trace.Span, run Run, claimedAt time.Time) (*AgentType, string, int, bool) {
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if errors.Is(err, ErrNotFound) {
		s.failStart(ctx, span, run.ID, RunStatePending, "The agent no longer exists.")
		return nil, "", 0, false
	}
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("get agent type: %w", err))
		return nil, "", 0, false
	}
	// The agent can change while the run waits. Re-check what the start depends on.
	switch at.State {
	case AgentTypeStateDeleting:
		s.failStart(ctx, span, run.ID, RunStatePending, "The agent is being deleted.")
		return nil, "", 0, false
	case AgentTypeStateProvisioning:
		if time.Since(run.CreatedAt) > s.provisionBudget() {
			s.failStart(ctx, span, run.ID, RunStatePending, fmt.Sprintf("The agent did not finish provisioning within %s.", s.provisionBudget()))
			return nil, "", 0, false
		}
		s.releaseStart(ctx, span, run.ID, claimedAt, errAgentProvisioning)
		return nil, "", 0, false
	}
	// Provisioning time is not part of the start budget. The agent's last update is when it became active.
	budgetStart := run.CreatedAt
	if at.UpdatedAt.After(budgetStart) {
		budgetStart = at.UpdatedAt
	}
	if time.Since(budgetStart) > s.startBudget() {
		s.failStart(ctx, span, run.ID, RunStatePending, fmt.Sprintf("No agent pod could take the run within %s.", s.startBudget()))
		return nil, "", 0, false
	}
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	pod, err := s.selectPod(ctx, at, namespace)
	if errors.Is(err, errNoReadyPod) {
		// Missing resources, after a cluster reset for example, leave no Ready pod. Create them only then.
		if provErr := s.missing.ProvisionMissing(ctx, *at, namespace); provErr != nil {
			err = fmt.Errorf("ensure agent infrastructure: %w", provErr)
		}
	}
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, err)
		return nil, "", 0, false
	}
	if err = s.repo.AssignPod(ctx, run.ID, claimedAt, pod); errors.Is(err, ErrConflict) {
		// The run was terminated, or another worker holds it now.
		s.logger.Info(ctx, "run left the queue before its pod was assigned", logging.Field{Key: "run_id", Value: run.ID})
		return nil, "", 0, false
	}
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("assign pod: %w", err))
		return nil, "", 0, false
	}
	return at, namespace, pod, true
}

// sessionRequest is the session-manager request for a run of at.
func (s *RunService) sessionRequest(ctx context.Context, run Run, at AgentType) sessionmanager.CreateSessionRequest {
	req := sessionmanager.CreateSessionRequest{
		RunID:       run.ID,
		AgentTypeID: run.AgentTypeID,
		TenantID:    run.TenantID,
		Framework:   string(at.Framework),
		// One governed MCP endpoint per server the agent has tool grants on.
		MCPServers: at.MCPServers(),
		// The agent's own prompt plus the runtime-environment brief.
		SystemPrompt: s.systemPromptWithBrief(ctx, at, run.ID),
		Guardrails:   runGuardrails(s.cfg.SandboxGuardrails, at.Guardrails),
		// Chosen by how the run started; the session-manager arms its idle timer with it.
		IdleTimeoutSecs: s.cfg.IdleTimeouts.ForRun(run.StartedBy),
		Triggered:       run.StartedBy != nil,
	}
	if at.ModelID != nil {
		req.Model = *at.ModelID
	}
	return req
}

// selectPod errors: no Ready, non-terminating pod, or every such pod is full.
var (
	errNoReadyPod = errors.New("no ready agent pod")
	errNoFreePod  = errors.New("no ready agent pod with a free slot")
)

// errAgentProvisioning releases a run whose agent is still being created.
var errAgentProvisioning = errors.New("agent is still provisioning")

// selectPod picks the least-loaded Ready, non-terminating pod with a free slot.
func (s *RunService) selectPod(ctx context.Context, at *AgentType, namespace string) (int, error) {
	counts, err := s.repo.ActiveRunsPerPod(ctx, at.ID)
	if err != nil {
		return 0, err
	}
	ready, err := s.k8s.ReadyOrdinals(ctx, at.Slug, namespace)
	if err != nil {
		return 0, fmt.Errorf("list ready pods: %w", err)
	}
	if len(ready) == 0 {
		return 0, errNoReadyPod
	}
	best, bestCount := -1, 0
	for _, i := range ready {
		if c := counts[i]; c < at.MaxConcurrentRunsPerPod && (best < 0 || c < bestCount) {
			best, bestCount = i, c
		}
	}
	if best < 0 {
		return 0, errNoFreePod
	}
	return best, nil
}

// releaseStart returns a claimed run to the queue after a transient failure.
func (s *RunService) releaseStart(ctx context.Context, span trace.Span, runID string, claimedAt time.Time, cause error) {
	span.RecordError(cause)
	span.SetStatus(codes.Error, errStartReleased.Error())
	ctx = context.WithoutCancel(ctx)
	if err := s.queue.ReleaseRunClaim(ctx, runID, claimedAt, time.Now().Add(s.pollInterval())); err != nil {
		s.logger.Warn(ctx, "release run claim failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	s.logger.Debug(ctx, "run start will retry",
		logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: cause.Error()})
}

// failStart records why a run could not start and fails it.
func (s *RunService) failStart(ctx context.Context, span trace.Span, runID string, from RunState, reason string) {
	span.SetStatus(codes.Error, errStartFailed.Error())
	ctx = context.WithoutCancel(ctx)
	// A run terminated meanwhile fails the transition and gets no error event.
	if err := s.Transition(ctx, runID, from, RunStateFailed); err != nil {
		s.logger.Warn(ctx, "fail run after start failure failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	payload, _ := json.Marshal(map[string]string{"code": "start_failed", "message": reason})
	if err := s.PublishEvent(ctx, runID, PushedEvent{Type: "run_error", Payload: payload}); err != nil {
		s.logger.Warn(ctx, "record run start failure failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

// closeSession removes a session the scheduler may have created on a pod and
// reports whether the pod has none for the run.
func (s *RunService) closeSession(ctx context.Context, podURL, runID string) bool {
	ctx = context.WithoutCancel(ctx)
	if err := s.sm.CloseSession(ctx, podURL, runID); err != nil && !errors.Is(err, sessionmanager.ErrSessionNotFound) {
		s.logger.Warn(ctx, "close session after a failed start failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
		return false
	}
	return true
}

// runIsActive reports whether a run is active. A transition can commit and still return an error.
func (s *RunService) runIsActive(ctx context.Context, runID string) bool {
	run, err := s.repo.GetRun(context.WithoutCancel(ctx), runID)
	return err == nil && run.State == RunStateActive
}
