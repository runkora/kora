package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/util/wait"

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
	// initialMessageAttempts is how many times the run's first message is sent before the run fails.
	initialMessageAttempts = 3
	// staleRunClaimWindow outlasts an attempt, so only a dead worker's claim goes stale.
	staleRunClaimWindow = 2 * startAttemptTimeout
	// reclaimEvery is how many polls pass between stale-claim sweeps.
	reclaimEvery = 10
	// boundCheckInterval and boundCheckTimeout pace and bound the wait for a pod to serve its agent before
	// the session is created (docs/adr/2026-11-22-agent-cold-tier.md, D5).
	boundCheckInterval = 100 * time.Millisecond
	boundCheckTimeout  = 10 * time.Second
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

	at, namespace, pod, warm, ok := s.placeRun(ctx, span, run, claimedAt)
	if !ok {
		return
	}
	// A warm pod grows to its full requests before it takes the run. A grow past its bound releases the run.
	if s.waker != nil {
		if err := s.waker.WakePod(ctx, namespace, pod, warm); err != nil {
			s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("grow agent pod %s: %w", pod, err))
			return
		}
	}

	// A digest is stored before the session exists, so every run that reaches the agent carries one.
	// An earlier attempt's digest is kept until this attempt's session is created.
	access := s.briefAccess(ctx, *at, run.ID)
	digest := agentConfigDigest(*at, access, runGuardrails(s.cfg.SandboxGuardrails, at.Guardrails), s.cfg.SandboxMount, s.cfg.Brand.ProductName())
	if err := s.repo.InitRunAgentConfig(ctx, run.ID, digest); err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, err)
		return
	}
	target, err := s.boundTarget(ctx, at.Slug, namespace, pod)
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, err)
		return
	}
	err = s.sm.CreateSession(ctx, target, s.sessionRequest(run, *at, access))
	switch {
	case err == nil:
		s.recordAgentConfig(ctx, run.ID, digest)
	case errors.Is(err, sessionmanager.ErrSessionConflict):
		// Sessions are keyed by run ID, so a conflict means an earlier attempt landed. Its digest stays.
	case isRetryableSessionError(err) || ctx.Err() != nil:
		s.closeSession(ctx, target, run.ID)
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("create session on pod %s: %w", pod, err))
		return
	default:
		s.closeSession(ctx, target, run.ID)
		s.failStart(ctx, span, err, run.ID, RunStateStarting, "The agent pod refused the run.")
		return
	}
	// The session exists now, so the attempt timeout must not strand the run in starting.
	if err = s.Transition(context.WithoutCancel(ctx), run.ID, RunStateStarting, RunStateActive); err != nil && !s.runIsActive(ctx, run.ID) {
		closed := s.closeSession(ctx, target, run.ID)
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
	if err := s.tryInitialMessage(ctx, target, run.ID, run.InitialMessage); err != nil {
		// A run with a session and no task would sit active with zero requests.
		s.logger.Error(ctx, "initial message could not be delivered; failing the run",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "pod", Value: pod},
			logging.Field{Key: "error", Value: err.Error()},
		)
		s.closeSession(ctx, target, run.ID)
		s.failStart(ctx, span, err, run.ID, RunStateActive, "The agent could not take the run's first message.")
		return
	}
	s.logger.Info(ctx, "Run started",
		logging.Field{Key: "run_id", Value: run.ID},
		logging.Field{Key: "agent_type_id", Value: run.AgentTypeID},
		logging.Field{Key: "pod", Value: pod},
	)
}

// errNotBound means a pod did not serve its agent within boundCheckTimeout.
var errNotBound = errors.New("pod did not serve its agent within the bound")

// boundTarget is the session-manager target of the agent's pod name once its bound check returns 200. A pod
// that has just been bound answers 503 until Envoy holds the agent certificate and identity-proxy the agent token.
func (s *RunService) boundTarget(ctx context.Context, slug, namespace, name string) (sessionmanager.Pod, error) {
	target, err := s.podTarget(ctx, slug, namespace, name)
	if err != nil {
		return sessionmanager.Pod{}, err
	}
	return target, waitBound(ctx, s.sm, target)
}

// boundChecker reads a pod's bound check.
type boundChecker interface {
	Bound(ctx context.Context, pod sessionmanager.Pod) (bool, error)
}

// waitBound polls the target's bound check every boundCheckInterval until it returns 200, up to boundCheckTimeout.
func waitBound(ctx context.Context, sm boundChecker, target sessionmanager.Pod) error {
	err := wait.PollUntilContextTimeout(ctx, boundCheckInterval, boundCheckTimeout, true, func(ctx context.Context) (bool, error) {
		bound, checkErr := sm.Bound(ctx, target)
		return bound && checkErr == nil, nil
	})
	if wait.Interrupted(err) {
		return fmt.Errorf("%w: %s", errNotBound, target.URL)
	}
	return err
}

// placeRun checks the run's agent, picks a pod and assigns it under the claim, and reports whether its pod
// list showed the pod warm. It reports false when the run was released, failed or taken by another worker.
func (s *RunService) placeRun(ctx context.Context, span trace.Span, run Run, claimedAt time.Time) (at *AgentType, namespace, pod string, warm, ok bool) {
	at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
	if errors.Is(err, ErrNotFound) {
		s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, "The agent no longer exists.")
		return nil, "", "", false, false
	}
	if err != nil {
		s.releaseStart(ctx, span, run.ID, claimedAt, fmt.Errorf("get agent type: %w", err))
		return nil, "", "", false, false
	}
	// The agent can change while the run waits. Re-check what the start depends on.
	switch at.State {
	case AgentTypeStateDeleting:
		s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, "The agent is being deleted.")
		return nil, "", "", false, false
	case AgentTypeStateProvisioning:
		if time.Since(run.CreatedAt) > s.provisionBudget() {
			s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, fmt.Sprintf("The agent did not finish provisioning within %s.", s.provisionBudget()))
			return nil, "", "", false, false
		}
		s.releaseStart(ctx, span, run.ID, claimedAt, errAgentProvisioning)
		return nil, "", "", false, false
	}
	namespace = s.cfg.TenantNamespaceFn(at.TenantID)
	pod, warm, err = s.assignPod(ctx, run, claimedAt, at, namespace)
	switch {
	case err == nil:
		return at, namespace, pod, warm, true
	case errors.Is(err, ErrConflict):
		// The run was terminated, or another worker holds it now.
		s.logger.Info(ctx, "run left the queue before its pod was assigned", logging.Field{Key: "run_id", Value: run.ID})
		return nil, "", "", false, false
	case !errors.Is(err, errNoReadyPod):
		// Full pods and the agent's total keep the run queued with no time limit.
		s.releaseStart(ctx, span, run.ID, claimedAt, err)
		return nil, "", "", false, false
	}
	// Only a run with no Ready pod is charged the start budget. Provisioning time is not part of it.
	budgetStart := run.CreatedAt
	if at.ActivatedAt.After(budgetStart) {
		budgetStart = at.ActivatedAt
	}
	if time.Since(budgetStart) > s.startBudget() {
		waits, waitErr := s.waitsPastBudget(ctx, at, namespace)
		if waitErr != nil {
			s.releaseStart(ctx, span, run.ID, claimedAt, waitErr)
			return nil, "", "", false, false
		}
		if !waits {
			s.failStart(ctx, span, errStartFailed, run.ID, RunStatePending, fmt.Sprintf("No agent pod could take the run within %s.", s.startBudget()))
			return nil, "", "", false, false
		}
	}
	// Missing resources, after a cluster reset for example, leave no Ready pod. Create them only then.
	if provErr := s.missing.ProvisionMissing(ctx, *at, namespace); provErr != nil {
		err = fmt.Errorf("ensure agent infrastructure: %w", provErr)
	}
	s.releaseStart(ctx, span, run.ID, claimedAt, err)
	return nil, "", "", false, false
}

// statefulSetRunReason is the run_error message of a run that ends at the cutover to pool pods.
const statefulSetRunReason = "The agent moved to pool pods."

// EndStatefulSetRuns fails every idle run that an earlier release placed on an agent StatefulSet pod: it has a
// pod ordinal and no pod name. Those pods drain away at the cutover to pool pods, and idle runs on them end
// once (docs/adr/2026-11-22-agent-cold-tier.md). A starting, active or completing run finishes within the
// drain. kora-agents runs it at startup and on each reaper tick, which ends the runs an earlier replica
// places during the rolling update.
func (s *RunService) EndStatefulSetRuns(ctx context.Context) {
	runs, err := s.repo.ListStatefulSetRuns(ctx)
	if err != nil {
		s.logger.Error(ctx, "list runs on agent StatefulSet pods failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, run := range runs {
		if err := s.Transition(withFailureCode(ctx, events.RunErrorRunnerError), run.ID, run.State, RunStateFailed); err != nil {
			s.logger.Warn(ctx, "end run on an agent StatefulSet pod failed",
				logging.Field{Key: "run_id", Value: run.ID}, logging.Field{Key: "error", Value: err.Error()})
			continue
		}
		payload, _ := json.Marshal(map[string]string{"code": events.RunErrorRunnerError, "message": statefulSetRunReason})
		if err := s.PublishEvent(ctx, run.ID, PushedEvent{Type: "run_error", Payload: payload}); err != nil {
			s.logger.Warn(ctx, "record the end of a run on an agent StatefulSet pod failed",
				logging.Field{Key: "run_id", Value: run.ID}, logging.Field{Key: "error", Value: err.Error()})
		}
	}
}

// initialMessageBackoff is the wait before the first retry of the run's first
// message. It doubles per retry. Tests shorten it.
var initialMessageBackoff = 250 * time.Millisecond

// livePods tells the start budget whether the agent has a pod that is not terminating or finished.
type livePods interface {
	LivePod(ctx context.Context, slug, namespace string) (bool, error)
}

// waitsPastBudget reports whether a run with no Ready pod stays queued past the start budget:
// a pod is coming up for it, or the agent is at its total (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
// A pod that turned Ready since placement looked also counts.
func (s *RunService) waitsPastBudget(ctx context.Context, at *AgentType, namespace string) (bool, error) {
	up, err := s.pods.LivePod(ctx, at.Slug, namespace)
	if err != nil {
		return false, fmt.Errorf("look up agent pods: %w", err)
	}
	if up {
		return true, nil
	}
	counts, err := s.placement.AgentRunCounts(ctx, at.ID)
	if err != nil {
		return false, err
	}
	return counts.atTotal(at.MaxConcurrentRuns), nil
}

// sessionRequest is the session-manager request for a run of at, with the brief for access.
func (s *RunService) sessionRequest(run Run, at AgentType, access *EffectiveAccess) sessionmanager.CreateSessionRequest {
	req := sessionmanager.CreateSessionRequest{
		RunID:       run.ID,
		AgentTypeID: run.AgentTypeID,
		AgentSlug:   at.Slug,
		TenantID:    run.TenantID,
		Framework:   string(at.Framework),
		// One governed MCP endpoint per server the agent has tool grants on.
		MCPServers: at.MCPServers(),
		// The agent's own prompt plus the runtime-environment brief.
		SystemPrompt: s.systemPromptWithBrief(at, access, run.ID),
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

// podPlacementStore is the run counts, agent lock and pod assignment that placing a run takes.
type podPlacementStore interface {
	AgentRunCounts(ctx context.Context, agentTypeID string) (AgentRunCounts, error)
	StartingRunsPerPod(ctx context.Context, agentTypeID string, assignedSince time.Time) (map[string]int, error)
	// WithAgentLock runs fn holding the agent's lock. Calls made with fn's ctx join its transaction.
	WithAgentLock(ctx context.Context, agentTypeID string, fn func(ctx context.Context) error) error
	AssignPod(ctx context.Context, runID string, claimedAt time.Time, podName string) error
}

// memoryReadTimeout bounds one pod memory read. A pod that does not answer in time counts as full.
const memoryReadTimeout = 2 * time.Second

// assignPod places a claimed run on the oldest pod with memory to spare
// (docs/adr/2026-11-22-agent-cold-tier.md, O3). Pod memory is read before
// the agent lock. A slow pod must never hold it, and the lock is taken only when a pod
// has room by that reading.
func (s *RunService) assignPod(ctx context.Context, run Run, claimedAt time.Time, at *AgentType, namespace string) (pod string, warm bool, err error) {
	pods, err := s.k8s.AgentPods(ctx, at.Slug, namespace)
	if err != nil {
		return "", false, fmt.Errorf("list agent pods: %w", err)
	}
	// During a roll the current pods take runs first, so the old pods drain.
	config := agentConfigHash(*at)
	ready := slices.Concat(readyOf(pods.current(config)), readyOf(pods.old(config)))
	if len(ready) == 0 {
		return "", false, errNoReadyPod
	}
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return "", false, err
	}
	// A pod with a pending resize has no reading and counts as full. A grow stuck on a full node waits there.
	readings := readPodMemory(ctx, s.sm, s.logger, at.Slug, namespace, readable(ready))
	// A run for an agent whose pods are all full is released here. Many such runs at once would
	// otherwise queue on the agent lock. The lock re-checks.
	starting, err := s.placement.StartingRunsPerPod(ctx, at.ID, time.Now().Add(-s.cfg.StartingRunWindow))
	if err != nil {
		return "", false, err
	}
	if _, ok := firstPodWithRoom(ready, readings, starting, allowance.Value(), s.cfg.PlacementWatermark); !ok {
		return "", false, errNoFreePod
	}
	var placed agentPod
	err = s.placement.WithAgentLock(ctx, at.ID, func(ctx context.Context) error {
		var lockedErr error
		placed, lockedErr = s.assignLocked(ctx, run, claimedAt, at, namespace, ready, readings, allowance.Value())
		return lockedErr
	})
	return placed.Name, placed.Warm, err
}

// assignLocked runs under the agent lock: it re-counts the agent's runs, checks the total, and assigns the
// oldest Ready pod whose reading has room once the API server shows it is not terminating.
func (s *RunService) assignLocked(ctx context.Context, run Run, claimedAt time.Time, at *AgentType, namespace string,
	ready []agentPod, readings map[string]sessionmanager.Memory, allowance int64) (agentPod, error) {
	counts, err := s.placement.AgentRunCounts(ctx, at.ID)
	if err != nil {
		return agentPod{}, err
	}
	if counts.atTotal(at.MaxConcurrentRuns) {
		return agentPod{}, errAgentAtTotal
	}
	starting, err := s.placement.StartingRunsPerPod(ctx, at.ID, time.Now().Add(-s.cfg.StartingRunWindow))
	if err != nil {
		return agentPod{}, err
	}
	pod, ok := firstPodWithRoom(ready, readings, starting, allowance, s.cfg.PlacementWatermark)
	if !ok {
		return agentPod{}, errNoFreePod
	}
	// The scaler may have removed the pod since the list was read.
	live, err := s.k8s.PodLive(ctx, namespace, pod.Name)
	if err != nil {
		return agentPod{}, err
	}
	if !live {
		return agentPod{}, errNoFreePod
	}
	return pod, s.placement.AssignPod(ctx, run.ID, claimedAt, pod.Name)
}

// podWaker grows a warm agent pod before a run is sent to it (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3).
// warm reports whether placement's pod list showed the pod warm.
type podWaker interface {
	WakePod(ctx context.Context, namespace, name string, warm bool) error
}

// podTarget is the session-manager target of the agent's bound pod name.
func (s *RunService) podTarget(ctx context.Context, slug, namespace, name string) (sessionmanager.Pod, error) {
	ip, err := s.k8s.PodIP(ctx, namespace, name)
	if err != nil {
		return sessionmanager.Pod{}, err
	}
	return sessionmanager.PodAt(ip, slug, namespace), nil
}

// podMemoryReader reads one pod's memory report.
type podMemoryReader interface {
	Memory(ctx context.Context, pod sessionmanager.Pod) (sessionmanager.Memory, error)
}

// readPodMemory reads the memory of each pod at its IP at once, keyed by pod name. A pod whose read fails or
// times out is left out.
func readPodMemory(ctx context.Context, sm podMemoryReader, logger logging.Logger, slug, namespace string, pods []agentPod) map[string]sessionmanager.Memory {
	readings := make([]*sessionmanager.Memory, len(pods))
	var wg sync.WaitGroup
	for i, pod := range pods {
		wg.Go(func() {
			readCtx, cancel := context.WithTimeout(ctx, memoryReadTimeout)
			defer cancel()
			m, err := sm.Memory(readCtx, sessionmanager.PodAt(pod.IP, slug, namespace))
			if err != nil {
				logger.Warn(ctx, "pod memory read failed; the pod counts as full",
					logging.Field{Key: "pod", Value: pod.Name}, logging.Field{Key: "error", Value: err.Error()})
				return
			}
			readings[i] = &m
		})
	}
	wg.Wait()
	out := make(map[string]sessionmanager.Memory, len(pods))
	for i, m := range readings {
		if m != nil {
			out[pods[i].Name] = *m
		}
	}
	return out
}

// firstPodWithRoom returns the first Ready pod in pods, oldest first, whose reading has room. A pod with no
// reading is full. Placement and the scaler share it, so the scaler adds pods exactly when placement finds none.
func firstPodWithRoom(pods []agentPod, readings map[string]sessionmanager.Memory, starting map[string]int, allowance int64, watermark float64) (agentPod, bool) {
	for _, pod := range pods {
		if m, ok := readings[pod.Name]; ok && pod.Ready && hasRoom(m, starting[pod.Name], allowance, watermark) {
			return pod, true
		}
	}
	return agentPod{}, false
}

// hasRoom reports whether a pod stays below the watermark with its starting runs and the run being
// placed counted at the allowance. A pod with no memory limit always has room.
func hasRoom(m sessionmanager.Memory, starting int, allowance int64, watermark float64) bool {
	if m.LimitBytes == 0 {
		return true
	}
	return float64(m.WorkingSetBytes+int64(starting+1)*allowance) < watermark*float64(m.LimitBytes)
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
func (s *RunService) closeSession(ctx context.Context, target sessionmanager.Pod, runID string) bool {
	ctx = context.WithoutCancel(ctx)
	if err := s.sm.CloseSession(ctx, target, runID); err != nil && !errors.Is(err, sessionmanager.ErrSessionNotFound) {
		s.logger.Warn(ctx, "close session after a failed start failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
		return false
	}
	return true
}

// recordAgentConfig stores the digest of the session this attempt created. The session exists, so a failed write does not stop the start.
func (s *RunService) recordAgentConfig(ctx context.Context, runID, digest string) {
	if err := s.repo.SetRunAgentConfig(context.WithoutCancel(ctx), runID, digest); err != nil {
		s.logger.Warn(ctx, "set run agent config failed",
			logging.Field{Key: "run_id", Value: runID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

// runIsActive reports whether a run is active. A transition can commit and still return an error.
func (s *RunService) runIsActive(ctx context.Context, runID string) bool {
	run, err := s.repo.GetRun(context.WithoutCancel(ctx), runID)
	return err == nil && run.State == RunStateActive
}
