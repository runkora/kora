package agents

import (
	"context"
	"errors"
	"math"
	"slices"
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
	StartingRunsPerPod(ctx context.Context, agentTypeID string, assignedSince time.Time) (map[int]int, error)
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[int]int, error)
}

// scalerCluster is the StatefulSet access the scaler needs. AgentStatefulSet reads a cache that
// SyncWorkloadCache starts. The writes read the live StatefulSet.
type scalerCluster interface {
	SyncWorkloadCache(ctx context.Context) error
	AgentStatefulSet(ctx context.Context, slug, namespace string) (statefulSetView, bool, error)
	ScaleStatefulSet(ctx context.Context, slug, namespace string, from, to int, resourceVersion string) error
	ReleaseRollHold(ctx context.Context, slug, namespace string, partition int, resourceVersion string) error
	SwitchToParallel(ctx context.Context, slug, namespace string) error
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
}

// Scaler sets each agent's StatefulSet replicas from its runs and pod memory
// (docs/adr/2026-11-09-demand-driven-agent-scaling.md).
type Scaler struct {
	store   scalerStore
	cluster scalerCluster
	sm      podMemoryReader
	cfg     ScalerConfig
	logger  logging.Logger
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
	case errors.Is(err, errReplicasMoved), errors.Is(err, errPartitionMoved):
		// Another scaler pass, an apply, an operator or the controller's status write changed the StatefulSet.
		// The next pass decides again.
	default:
		observability.RecordFailure(span, err, errScaleAgent)
		s.logger.Warn(ctx, "scale agent failed",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

func (s *Scaler) scale(ctx context.Context, at AgentType) error {
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	ss, found, err := s.cluster.AgentStatefulSet(ctx, at.Slug, namespace)
	if err != nil || !found {
		return err
	}
	// A Ready surge pod releases the held roll, as does a hold with no surge pod left to wait for.
	// A hold left by a restart is released here too.
	if ss.Partition > 0 && (ss.Partition >= ss.Replicas || ss.SurgeReady) {
		s.logger.Info(ctx, "releasing agent roll hold",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "partition", Value: ss.Partition})
		return s.cluster.ReleaseRollHold(ctx, at.Slug, namespace, ss.Partition, ss.ResourceVersion)
	}
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return err
	}
	readings := readPodMemory(ctx, s.sm, s.logger, at.Slug, namespace, ss.Ready)
	ran, err := s.store.WaitWithAgentLock(ctx, at.ID, scalerLockTimeout, func(ctx context.Context) error {
		return s.scaleLocked(ctx, at, namespace, ss, readings, allowance.Value())
	})
	if err == nil && !ran {
		s.logger.Debug(ctx, "scaler skipped agent for this pass",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "reason", Value: "agent lock wait timed out"})
	}
	return err
}

// scaleLocked runs under the agent lock: it re-counts the runs, then switches an idle agent to
// Parallel pod management or moves its replicas toward demand.
func (s *Scaler) scaleLocked(ctx context.Context, at AgentType, namespace string, ss statefulSetView,
	readings map[int]sessionmanager.Memory, allowance int64) error {
	counts, err := s.store.AgentRunCounts(ctx, at.ID)
	if err != nil {
		return err
	}
	// The policy is immutable and the switch recreates the StatefulSet, so it waits for an agent with no run on a pod.
	if !ss.Parallel && counts.Active+counts.Starting == 0 {
		s.logger.Info(ctx, "switching agent StatefulSet to Parallel pod management",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "replicas", Value: ss.Replicas})
		return s.cluster.SwitchToParallel(ctx, at.Slug, namespace)
	}
	starting, err := s.store.StartingRunsPerPod(ctx, at.ID, time.Now().Add(-s.cfg.StartingRunWindow))
	if err != nil {
		return err
	}
	occupied, err := s.store.ActiveRunsPerPod(ctx, at.ID)
	if err != nil {
		return err
	}
	want := desiredReplicas(scaleInput{
		replicas: ss.Replicas, floor: int(max(s.cfg.MinReplicas, 1)), total: at.MaxConcurrentRuns, maxReplicas: int(s.cfg.MaxReplicas),
		counts: counts, ready: ss.Ready, terminating: ss.Terminating, rolling: ss.Partition > 0 || ss.Rolling,
		readings: readings, starting: starting,
		topRuns: occupied[ss.Replicas-1], memoryMax: memoryMax(ss.Ready, readings, ss.MemoryLimitBytes),
		allowance: allowance, watermark: s.cfg.PlacementWatermark,
	})
	if want == ss.Replicas {
		return nil
	}
	if err := s.cluster.ScaleStatefulSet(ctx, at.Slug, namespace, ss.Replicas, want, ss.ResourceVersion); err != nil {
		return err
	}
	s.logger.Info(ctx, "scaled agent pods",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "from", Value: ss.Replicas}, logging.Field{Key: "to", Value: want},
		logging.Field{Key: "pending", Value: counts.Pending}, logging.Field{Key: "starting", Value: counts.Starting},
		logging.Field{Key: "active", Value: counts.Active}, logging.Field{Key: "ready", Value: len(ss.Ready)})
	return nil
}

// scaleInput is what one scaling decision reads.
type scaleInput struct {
	replicas, floor, total, maxReplicas int
	counts                              AgentRunCounts
	ready                               []int
	// terminating counts the draining pods below the replicas. They are not coming up.
	terminating int
	// rolling holds scale-down while a roll is held or under way.
	rolling  bool
	readings map[int]sessionmanager.Memory
	starting map[int]int
	// topRuns counts the runs on the top pod, ordinal replicas-1.
	topRuns   int
	memoryMax int64
	allowance int64
	watermark float64
}

// desiredReplicas raises the replicas until the floor of pods are not terminating, adds the pods the
// startable pending runs need in one step when no Ready pod has room, or removes the top pod when it is
// empty, nothing waits, no roll is on and keepsReadyFloor holds. It never adds pods past
// the agent's total or maxReplicas, other than to reach the floor.
func desiredReplicas(in scaleInput) int {
	ceiling := min(in.total, in.maxReplicas)
	if floor := floorReplicas(in, ceiling); floor > in.replicas {
		return floor
	}
	// Runs over the agent's total cannot start, so they need no pod.
	startable := min(in.counts.Pending, in.total-in.counts.Active-in.counts.Starting)
	// Pods above the ceiling, after it was lowered, are never needed for a waiting run. A pod at the
	// ceiling is kept while runs wait, or it would be removed and added again.
	if startable > 0 && in.replicas <= ceiling {
		if in.replicas == ceiling {
			return in.replicas
		}
		if _, ok := firstPodWithRoom(in.ready, in.replicas, in.readings, in.starting, in.allowance, in.watermark); ok {
			return in.replicas
		}
		comingUp := in.replicas - len(in.ready) - in.terminating
		add := podsFor(startable, in.allowance, in.watermark, in.memoryMax) - comingUp
		return max(in.replicas, min(in.replicas+add, ceiling))
	}
	if keepsReadyFloor(in) && in.topRuns == 0 && !in.rolling {
		return in.replicas - 1
	}
	return in.replicas
}

// floorReplicas is the replicas that keep the floor of pods not terminating. A draining pod holds its
// ordinal until it exits, so its replacement goes above the replicas, up to the ceiling.
func floorReplicas(in scaleInput, ceiling int) int {
	short := in.floor - (in.replicas - in.terminating)
	return max(in.floor, min(in.replicas+short, ceiling))
}

// keepsReadyFloor reports whether removing the top pod keeps the floor of pods not terminating, and
// keeps the floor of Ready pods below it when the top pod is Ready. A top pod that never got Ready
// costs no Ready pod and is removed.
func keepsReadyFloor(in scaleInput) bool {
	top := in.replicas - 1
	if top-in.terminating < in.floor {
		return false
	}
	readyBelow := 0
	for _, ord := range in.ready {
		if ord < top {
			readyBelow++
		}
	}
	return readyBelow >= in.floor || !slices.Contains(in.ready, top)
}

// podsFor is ceil(runs × allowance ÷ (watermark × memory.max)): the pods that runs fill to the watermark.
func podsFor(runs int, allowance int64, watermark float64, memoryMax int64) int {
	perPod := watermark * float64(memoryMax)
	if perPod <= 0 {
		return 1
	}
	return int(math.Ceil(float64(runs) * float64(allowance) / perPod))
}

// memoryMax is a Ready pod's reported memory.max, else the StatefulSet's container memory limit.
func memoryMax(ready []int, readings map[int]sessionmanager.Memory, limit int64) int64 {
	for _, ord := range ready {
		if m, ok := readings[ord]; ok && m.LimitBytes > 0 {
			return m.LimitBytes
		}
	}
	return limit
}
