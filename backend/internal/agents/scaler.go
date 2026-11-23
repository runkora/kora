package agents

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// scalerInterval is how often the scaler checks each agent, independent of the claim tick.
const scalerInterval = 5 * time.Second

// scalerLockTimeout bounds the scaler's wait for the agent lock. An agent it times out on is skipped for the pass.
const scalerLockTimeout = 2 * time.Second

// errScaleAgent is the span status of a scaler pass that failed for one agent.
var errScaleAgent = errors.New("scale agent")

// scalerStore is the storage the scaler reads. The run counts are read under placement's agent lock.
type scalerStore interface {
	ListActiveAgentTypes(ctx context.Context) ([]AgentType, error)
	WaitWithAgentLock(ctx context.Context, agentTypeID string, timeout time.Duration, fn func(ctx context.Context) error) (bool, error)
	AgentRunCounts(ctx context.Context, agentTypeID string) (AgentRunCounts, error)
	StartingRunsPerPod(ctx context.Context, agentTypeID string, assignedSince time.Time) (map[string]int, error)
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[string]int, error)
	// LastRunCompletedAt is the completion time of the agent's latest completed run, zero when it has none.
	LastRunCompletedAt(ctx context.Context, agentTypeID string) (time.Time, error)
}

// scalerCluster is the pod access the scaler needs. AgentPods reads a cache that SyncWorkloadCache starts.
// LiveAgentPods reads the API server.
type scalerCluster interface {
	SyncWorkloadCache(ctx context.Context) error
	AgentPods(ctx context.Context, slug, namespace string) (agentPods, error)
	LiveAgentPods(ctx context.Context, slug, namespace string) (agentPods, error)
	// BindPod binds a pool pod to the agent and returns its name. DeletePod drains and deletes a pod.
	BindPod(ctx context.Context, at AgentType, namespace string) (string, error)
	DeletePod(ctx context.Context, namespace, name string) error
	// ShrinkPod and GrowPod set a pod's session-manager requests to the warm or the full size.
	ShrinkPod(ctx context.Context, namespace, name string) error
	GrowPod(ctx context.Context, namespace, name string) error
}

// ScalerConfig holds the scaler's settings.
type ScalerConfig struct {
	TenantNamespaceFn func(tenantID string) string
	// MinReplicas is the floor of every agent's pods, and MaxReplicas their ceiling.
	MinReplicas int32
	MaxReplicas int32
	// PlacementWatermark, StartingRunAllowances and StartingRunWindow are placement's values.
	PlacementWatermark    float64
	StartingRunAllowances StartingRunAllowances
	StartingRunWindow     time.Duration
	// Concurrency is how many agents one pass checks at once. Each check may hold a Postgres connection.
	Concurrency int
	// WarmAfter is how long an agent stays idle before its pods shrink to the warm size. Zero keeps them full.
	WarmAfter time.Duration
}

// Scaler binds and deletes each agent's pods from its runs and pod memory, and rolls the pods a pool
// template change left behind (docs/adr/2026-11-09-demand-driven-agent-scaling.md,
// docs/adr/2026-11-22-agent-cold-tier.md).
type Scaler struct {
	store   scalerStore
	cluster scalerCluster
	sm      podMemoryReader
	cfg     ScalerConfig
	logger  logging.Logger
	// refused holds the pods whose shrink the API server refused for good.
	refused sync.Map
}

// NewScaler builds a scaler. Start runs it.
func NewScaler(store scalerStore, cluster scalerCluster, sm podMemoryReader, cfg ScalerConfig, logger logging.Logger) *Scaler {
	return &Scaler{store: store, cluster: cluster, sm: sm, cfg: cfg, logger: logger}
}

// Start waits for the workload cache, then checks every active agent each scalerInterval until ctx ends.
func (s *Scaler) Start(ctx context.Context) {
	if err := s.cluster.SyncWorkloadCache(ctx); err != nil {
		s.logger.Error(ctx, "scaler stopped before its workload cache synced", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	ticker := time.NewTicker(scalerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.pass(ctx)
	}
}

// pass checks every active agent, up to cfg.Concurrency at once, and returns when all are checked.
// Start runs one pass at a time.
func (s *Scaler) pass(ctx context.Context) {
	ats, err := s.store.ListActiveAgentTypes(ctx)
	if err != nil {
		s.logger.Error(ctx, "list agents to scale failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	var g errgroup.Group
	g.SetLimit(max(s.cfg.Concurrency, 1))
	for _, at := range ats {
		g.Go(func() error {
			s.scaleAgent(ctx, at)
			return nil
		})
	}
	_ = g.Wait() // scaleAgent logs its own failures and returns none.
}

// scaleAgent reads pod memory with no lock held, then decides and scales under the agent lock.
// A replica that waits past scalerLockTimeout for the lock skips the agent for this pass.
func (s *Scaler) scaleAgent(ctx context.Context, at AgentType) {
	ctx, span := agentsTracer.Start(observability.WithTenant(ctx, at.TenantID), "agents.Scaler.scaleAgent",
		trace.WithAttributes(observability.AgentIDKey.String(at.ID)))
	defer span.End()

	err := s.scale(ctx, at)
	switch {
	case err == nil:
	case errors.Is(err, errNoPoolPod):
		// The pool refills within seconds, and the next pass binds again.
		s.logger.Debug(ctx, "no pool pod to bind for the agent",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "error", Value: err.Error()})
	default:
		observability.RecordFailure(span, err, errScaleAgent)
		s.logger.Warn(ctx, "scale agent failed",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

func (s *Scaler) scale(ctx context.Context, at AgentType) error {
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	pods, err := s.cluster.AgentPods(ctx, at.Slug, namespace)
	if err != nil {
		return err
	}
	// A pod that exited, such as one the kubelet evicted, holds its workspace claim until it is deleted.
	for _, name := range pods.Finished {
		if deleteErr := s.cluster.DeletePod(ctx, namespace, name); deleteErr != nil {
			return deleteErr
		}
	}
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return err
	}
	// A pod with a pending resize has no reading and counts as full, so a stuck grow gets a new pod.
	readings := readPodMemory(ctx, s.sm, s.logger, at.Slug, namespace, readable(pods.Live))
	ran, err := s.store.WaitWithAgentLock(ctx, at.ID, scalerLockTimeout, func(ctx context.Context) error {
		return s.scaleLocked(ctx, at, namespace, pods, readings, allowance.Value())
	})
	if err == nil && !ran {
		s.logger.Debug(ctx, "scaler skipped agent for this pass",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "reason", Value: "agent lock wait timed out"})
	}
	return err
}

// scaleLocked runs under the agent lock: it re-counts the runs, then takes one roll step or moves the
// agent's pods toward demand.
func (s *Scaler) scaleLocked(ctx context.Context, at AgentType, namespace string, pods agentPods,
	readings map[string]sessionmanager.Memory, allowance int64) error {
	counts, err := s.store.AgentRunCounts(ctx, at.ID)
	if err != nil {
		return err
	}
	starting, err := s.store.StartingRunsPerPod(ctx, at.ID, time.Now().Add(-s.cfg.StartingRunWindow))
	if err != nil {
		return err
	}
	occupied, err := s.store.ActiveRunsPerPod(ctx, at.ID)
	if err != nil {
		return err
	}
	if rolled, err := s.roll(ctx, at, namespace, pods, occupied); rolled || err != nil {
		return err
	}
	in := scaleInput{
		pods: pods.Live, floor: int(max(s.cfg.MinReplicas, 1)), total: at.MaxConcurrentRuns, maxPods: int(s.cfg.MaxReplicas),
		counts: counts, rolling: pods.rolling(), readings: readings, starting: starting, occupied: occupied,
		memoryMax: memoryMax(pods.Live, readings, pods.MemoryLimitBytes), allowance: allowance, watermark: s.cfg.PlacementWatermark,
	}
	want := desiredPods(in)
	switch {
	case want > len(in.pods):
		for range want - len(in.pods) {
			name, err := s.cluster.BindPod(ctx, at, namespace)
			if name == "" {
				return err
			}
			s.logger.Info(ctx, "bound agent pod",
				logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: name},
				logging.Field{Key: "pending", Value: counts.Pending}, logging.Field{Key: "starting", Value: counts.Starting},
				logging.Field{Key: "active", Value: counts.Active}, logging.Field{Key: "pods", Value: len(in.pods)})
			if err != nil {
				return err
			}
		}
		return nil
	case want < len(in.pods):
		// The cache can miss a pod that started terminating. A scale-down decides the floor and the
		// empty pod again from a live read of the pods.
		live, err := s.cluster.LiveAgentPods(ctx, at.Slug, namespace)
		if err != nil {
			return err
		}
		in.pods = live.Live
		victim, ok := scaleDownCandidate(in)
		if !ok {
			return nil
		}
		if err := s.cluster.DeletePod(ctx, namespace, victim.Name); err != nil {
			return err
		}
		s.logger.Info(ctx, "removed empty agent pod",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: victim.Name},
			logging.Field{Key: "pods", Value: len(in.pods)})
		return nil
	}
	return s.settleResidency(ctx, at, namespace, pods.Live, counts, occupied)
}

// roll takes one step of moving the agent's pods to the pool's current template: it binds one pod on the
// current template, and once one is Ready, deletes one old pod with the drain, an empty one first
// (docs/adr/2026-11-22-agent-cold-tier.md, O3). It reports whether it changed a pod.
func (s *Scaler) roll(ctx context.Context, at AgentType, namespace string, pods agentPods, occupied map[string]int) (bool, error) {
	if !pods.rolling() {
		return false, nil
	}
	var current, old []agentPod
	for _, pod := range pods.Live {
		if pod.Template == pods.Template {
			current = append(current, pod)
		} else {
			old = append(old, pod)
		}
	}
	if len(current) == 0 {
		name, err := s.cluster.BindPod(ctx, at, namespace)
		if name != "" {
			s.logger.Info(ctx, "bound agent pod on the current pool template",
				logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: name})
		}
		return true, err
	}
	if !slices.ContainsFunc(current, func(pod agentPod) bool { return pod.Ready }) {
		return false, nil
	}
	victim := old[0]
	if i := slices.IndexFunc(old, func(pod agentPod) bool { return occupied[pod.Name] == 0 }); i >= 0 {
		victim = old[i]
	}
	if err := s.cluster.DeletePod(ctx, namespace, victim.Name); err != nil {
		return true, err
	}
	s.logger.Info(ctx, "removed agent pod on an old pool template",
		logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: victim.Name},
		logging.Field{Key: "runs", Value: occupied[victim.Name]})
	return true, nil
}

// settleResidency grows a warm pod that holds a run, and shrinks the other Ready pods once the agent has
// been idle for WarmAfter (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3). A run placed
// with no grow, by a stale cache, gets its pod grown here, and a held grow frees the wake reserve. With the
// warm tier off, every warm pod grows.
func (s *Scaler) settleResidency(ctx context.Context, at AgentType, namespace string, pods []agentPod,
	counts AgentRunCounts, occupied map[string]int) error {
	if counts.Starting+counts.Active > 0 || s.cfg.WarmAfter <= 0 {
		return s.growHeld(ctx, namespace, pods, occupied)
	}
	// A grow still pending on a full node shrinks back too, so the pod stops counting as full.
	hot := slices.DeleteFunc(slices.Clone(pods), func(pod agentPod) bool {
		_, refused := s.refused.Load(namespace + "/" + pod.Name)
		return !pod.Ready || pod.Warm || refused
	})
	if counts.Pending > 0 || len(hot) == 0 {
		return nil
	}
	last, err := s.store.LastRunCompletedAt(ctx, at.ID)
	if err != nil {
		return err
	}
	idleSince := at.ActivatedAt
	if last.After(idleSince) {
		idleSince = last
	}
	if time.Since(idleSince) < s.cfg.WarmAfter {
		return nil
	}
	return s.shrink(ctx, at, namespace, hot)
}

// growHeld grows each warm pod that holds a run, or every warm pod with the warm tier off.
func (s *Scaler) growHeld(ctx context.Context, namespace string, pods []agentPod, occupied map[string]int) error {
	for _, pod := range pods {
		if !pod.Warm && !pod.Resizing || occupied[pod.Name] == 0 && s.cfg.WarmAfter > 0 {
			continue
		}
		if err := s.cluster.GrowPod(ctx, namespace, pod.Name); err != nil {
			return err
		}
	}
	return nil
}

// shrink sets each of the agent's hot pods to the warm requests. A pod whose shrink the API server refuses
// for good is logged once and left at full size.
func (s *Scaler) shrink(ctx context.Context, at AgentType, namespace string, hot []agentPod) error {
	for _, pod := range hot {
		err := s.cluster.ShrinkPod(ctx, namespace, pod.Name)
		switch {
		case err == nil:
			s.logger.Info(ctx, "shrank idle agent pod to warm",
				logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: pod.Name})
		case resizeRefused(err):
			// The cluster or the pod cannot resize in place. The pod stays at full size and is not tried again.
			s.refused.Store(namespace+"/"+pod.Name, struct{}{})
			s.logger.Warn(ctx, "agent pod cannot resize in place; it stays at full size",
				logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: pod.Name},
				logging.Field{Key: "error", Value: err.Error()})
		default:
			return err
		}
	}
	return nil
}

// scaleInput is what one scaling decision reads.
type scaleInput struct {
	// pods holds the agent's live pods, oldest first. A terminating pod does not count.
	pods                  []agentPod
	floor, total, maxPods int
	counts                AgentRunCounts
	// rolling holds scale-down while a roll is under way.
	rolling            bool
	readings           map[string]sessionmanager.Memory
	starting, occupied map[string]int
	memoryMax          int64
	allowance          int64
	watermark          float64
}

// desiredPods raises the pods to the floor, adds the pods the startable pending runs need in one step when
// no Ready pod has room, or removes one pod when scaleDownCandidate finds one. It never adds pods past the
// agent's total or maxPods, other than to reach the floor.
func desiredPods(in scaleInput) int {
	live := len(in.pods)
	if live < in.floor {
		return in.floor
	}
	ceiling := min(in.total, in.maxPods)
	// Runs over the agent's total cannot start, so they need no pod.
	startable := min(in.counts.Pending, in.total-in.counts.Active-in.counts.Starting)
	// Pods above the ceiling, after it was lowered, are never needed for a waiting run. A pod at the
	// ceiling is kept while runs wait, or it would be removed and added again.
	if startable > 0 && live <= ceiling {
		if live == ceiling {
			return live
		}
		if _, ok := firstPodWithRoom(in.pods, in.readings, in.starting, in.allowance, in.watermark); ok {
			return live
		}
		comingUp := live - countReady(in.pods)
		add := podsFor(startable, in.allowance, in.watermark, in.memoryMax) - comingUp
		return max(live, min(live+add, ceiling))
	}
	if _, ok := scaleDownCandidate(in); ok {
		return live - 1
	}
	return live
}

// countReady counts the Ready pods.
func countReady(pods []agentPod) int {
	n := 0
	for _, pod := range pods {
		if pod.Ready {
			n++
		}
	}
	return n
}

// scaleDownCandidate returns the newest empty pod when no roll is under way and removing it keeps the floor
// of pods and, when it is Ready, the floor of Ready pods. An empty pod that never got Ready costs no Ready
// pod and is removed.
func scaleDownCandidate(in scaleInput) (agentPod, bool) {
	if in.rolling || len(in.pods)-1 < in.floor {
		return agentPod{}, false
	}
	for _, pod := range slices.Backward(in.pods) {
		if in.occupied[pod.Name] > 0 {
			continue
		}
		if !pod.Ready || countReady(in.pods)-1 >= in.floor {
			return pod, true
		}
		return agentPod{}, false
	}
	return agentPod{}, false
}

// podsFor is ceil(runs × allowance ÷ (watermark × memory.max)): the pods that runs fill to the watermark.
func podsFor(runs int, allowance int64, watermark float64, memoryMax int64) int {
	perPod := watermark * float64(memoryMax)
	if perPod <= 0 {
		return 1
	}
	return int(math.Ceil(float64(runs) * float64(allowance) / perPod))
}

// memoryMax is a Ready pod's reported memory.max, else the configured session-manager memory limit.
func memoryMax(pods []agentPod, readings map[string]sessionmanager.Memory, limit int64) int64 {
	for _, pod := range pods {
		if m, ok := readings[pod.Name]; ok && m.LimitBytes > 0 {
			return m.LimitBytes
		}
	}
	return limit
}
