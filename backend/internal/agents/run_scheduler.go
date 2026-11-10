package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
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
	// Every attempt continues the trace of the request that started the run.
	ctx = observability.ContextWithTraceParent(ctx, run.TraceParent)
	ctx, span := agentsTracer.Start(observability.WithTenant(observability.WithRun(ctx, run.ID), run.TenantID), "agents.startClaimed",
		trace.WithAttributes(observability.AgentIDKey.String(run.AgentTypeID)),
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
		s.closeSession(ctx, podURL, run.ID)
		s.failStart(ctx, span, err, run.ID, RunStateStarting, "The agent pod refused the run.")
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
			s.failStart(ctx, span, err, run.ID, RunStateStarting, "The agent pod did not confirm the run.")
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
		s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, "The agent no longer exists.")
		return nil, "", 0, false
	}
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("get agent type: %w", err))
		return nil, "", 0, false
	}
	// The agent can change while the run waits. Re-check what the start depends on.
	switch at.State {
	case AgentTypeStateDeleting:
		s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, "The agent is being deleted.")
		return nil, "", 0, false
	case AgentTypeStateProvisioning:
		if time.Since(run.CreatedAt) > s.provisionBudget() {
			s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, fmt.Sprintf("The agent did not finish provisioning within %s.", s.provisionBudget()))
			return nil, "", 0, false
		}
		s.releaseStart(ctx, span, run.ID, claimedAt, errAgentProvisioning)
		return nil, "", 0, false
	}
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	pod, err := s.assignPod(ctx, run, claimedAt, at, namespace)
	switch {
	case err == nil:
		return at, namespace, pod, true
	case errors.Is(err, ErrConflict):
		// The run was terminated, or another worker holds it now.
		s.logger.Info(ctx, "run left the queue before its pod was assigned", logging.Field{Key: "run_id", Value: run.ID})
		return nil, "", 0, false
	case !errors.Is(err, errNoReadyPod):
		// Full pods and the agent's total keep the run queued with no time limit.
		s.releaseStart(ctx, span, run.ID, claimedAt, err)
		return nil, "", 0, false
	}
	// Only a run with no Ready pod is charged the start budget. Provisioning time is not part of it.
	budgetStart := run.CreatedAt
	if at.ActivatedAt.After(budgetStart) {
		budgetStart = at.ActivatedAt
	}
	if time.Since(budgetStart) > s.startBudget() {
		s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, fmt.Sprintf("No agent pod could take the run within %s.", s.startBudget()))
		return nil, "", 0, false
	}
	// Missing resources, after a cluster reset for example, leave no Ready pod. Create them only then.
	if provErr := s.missing.ProvisionMissing(ctx, *at, namespace); provErr != nil {
		err = fmt.Errorf("ensure agent infrastructure: %w", provErr)
	}
	s.releaseStart(ctx, span, run.ID, claimedAt, err)
	return nil, "", 0, false
}

// sessionRequest is the session-manager request for a run of at.
func (s *RunService) sessionRequest(ctx context.Context, run Run, at AgentType) sessionmanager.CreateSessionRequest {
	req := sessionmanager.CreateSessionRequest{
		RunID:       run.ID,
		AgentTypeID: run.AgentTypeID,
		AgentSlug:   at.Slug,
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

// Placement errors: no Ready, non-terminating pod, every such pod is full, or the agent is at its total.
var (
	errNoReadyPod   = errors.New("no ready agent pod")
	errNoFreePod    = errors.New("no ready agent pod with memory to spare")
	errAgentAtTotal = errors.New("agent is at its total of concurrent runs")
)

// errAgentProvisioning releases a run whose agent is still being created.
var errAgentProvisioning = errors.New("agent is still provisioning")

// memoryReadTimeout bounds one pod memory read. A pod that does not answer in time counts as full.
const memoryReadTimeout = 2 * time.Second

// assignPod places a claimed run on the lowest-ordinal pod with memory to spare
// (docs/adr/2026-11-09-demand-driven-agent-scaling.md). Pod memory is read before
// the agent lock, so a slow pod never holds it.
func (s *RunService) assignPod(ctx context.Context, run Run, claimedAt time.Time, at *AgentType, namespace string) (int, error) {
	ready, err := s.k8s.ReadyOrdinals(ctx, at.Slug, namespace)
	if err != nil {
		return 0, fmt.Errorf("list ready pods: %w", err)
	}
	if len(ready) == 0 {
		return 0, errNoReadyPod
	}
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return 0, err
	}
	readings := s.readPodMemory(ctx, at.Slug, namespace, ready)
	pod := -1
	err = s.repo.WithAgentLock(ctx, at.ID, func(ctx context.Context) error {
		var lockedErr error
		pod, lockedErr = s.assignLocked(ctx, run, claimedAt, at, namespace, ready, readings, allowance.Value())
		return lockedErr
	})
	return pod, err
}

// assignLocked runs under the agent lock: it re-counts the agent's runs, checks the total and
// the current replicas, and assigns the lowest ready pod whose reading has room.
func (s *RunService) assignLocked(ctx context.Context, run Run, claimedAt time.Time, at *AgentType, namespace string,
	ready []int, readings map[int]sessionmanager.Memory, allowance int64) (int, error) {
	counts, err := s.repo.AgentRunCounts(ctx, at.ID)
	if err != nil {
		return 0, err
	}
	if counts.Active+counts.Starting >= at.MaxConcurrentRuns {
		return 0, errAgentAtTotal
	}
	// The scaler may have removed a pod since the list was read.
	replicas, err := s.k8s.Replicas(ctx, at.Slug, namespace)
	if err != nil {
		return 0, fmt.Errorf("read replicas: %w", err)
	}
	starting, err := s.repo.StartingRunsPerPod(ctx, at.ID)
	if err != nil {
		return 0, err
	}
	for _, ord := range ready {
		if m, ok := readings[ord]; ok && ord < replicas && hasRoom(m, starting[ord], allowance, s.cfg.PlacementWatermark) {
			return ord, s.repo.AssignPod(ctx, run.ID, claimedAt, ord)
		}
	}
	return 0, errNoFreePod
}

// readPodMemory reads the memory of each pod at once. A pod whose read fails or times out is left out.
func (s *RunService) readPodMemory(ctx context.Context, slug, namespace string, ordinals []int) map[int]sessionmanager.Memory {
	readings := make([]*sessionmanager.Memory, len(ordinals))
	var wg sync.WaitGroup
	for i, ord := range ordinals {
		wg.Go(func() {
			readCtx, cancel := context.WithTimeout(ctx, memoryReadTimeout)
			defer cancel()
			m, err := s.sm.Memory(readCtx, sessionmanager.PodURL(slug, ord, namespace))
			if err != nil {
				s.logger.Warn(ctx, "pod memory read failed; the pod counts as full",
					logging.Field{Key: "pod_ordinal", Value: ord}, logging.Field{Key: "error", Value: err.Error()})
				return
			}
			readings[i] = &m
		})
	}
	wg.Wait()
	out := make(map[int]sessionmanager.Memory, len(ordinals))
	for i, m := range readings {
		if m != nil {
			out[ordinals[i]] = *m
		}
	}
	return out
}

// hasRoom reports whether a pod stays below the watermark with its starting runs counted at the allowance.
// A pod with no memory limit always has room.
func hasRoom(m sessionmanager.Memory, starting int, allowance int64, watermark float64) bool {
	if m.LimitBytes == 0 {
		return true
	}
	return float64(m.WorkingSetBytes+int64(starting)*allowance) < watermark*float64(m.LimitBytes)
}

// releaseStart returns a claimed run to the queue after a transient failure.
func (s *RunService) releaseStart(ctx context.Context, span trace.Span, runID string, claimedAt time.Time, cause error) {
	observability.RecordFailure(span, cause, errStartReleased)
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
func (s *RunService) failStart(ctx context.Context, span trace.Span, err error, runID string, from RunState, reason string) {
	observability.RecordFailure(span, err, errStartFailed)
	ctx = context.WithoutCancel(ctx)
	// A run terminated meanwhile fails the transition and gets no error event.
	// The run_error follows the transition, so the failed event takes its code from ctx.
	if err := s.Transition(withFailureCode(ctx, events.RunErrorStartFailed), runID, from, RunStateFailed); err != nil {
		s.logger.Warn(ctx, "fail run after start failure failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
		return
	}
	payload, _ := json.Marshal(map[string]string{"code": events.RunErrorStartFailed, "message": reason})
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
