package agents

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.opentelemetry.io/otel/trace"

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

// scalerCluster is the StatefulSet access the scaler needs.
type scalerCluster interface {
	AgentStatefulSet(ctx context.Context, slug, namespace string) (statefulSetView, bool, error)
	ReadyOrdinals(ctx context.Context, slug, namespace string) ([]int, error)
	ScaleStatefulSet(ctx context.Context, slug, namespace string, from, to int) error
	SwitchToParallel(ctx context.Context, slug, namespace string) error
}

// ScalerConfig holds the scaler's settings.
type ScalerConfig struct {
	TenantNamespaceFn func(tenantID string) string
	// MinReplicas is the floor of every agent's pods.
	MinReplicas int32
	// PlacementWatermark, StartingRunAllowances and StartingRunWindow are placement's values.
	PlacementWatermark    float64
	StartingRunAllowances StartingRunAllowances
	StartingRunWindow     time.Duration
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

// Start checks every active agent each scalerInterval until ctx ends.
func (s *Scaler) Start(ctx context.Context) {
	ticker := time.NewTicker(scalerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		ats, err := s.store.ListActiveAgentTypes(ctx)
		if err != nil {
			s.logger.Error(ctx, "list agents to scale failed", logging.Field{Key: "error", Value: err.Error()})
			continue
		}
		for _, at := range ats {
			s.scaleAgent(ctx, at)
		}
	}
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
	case errors.Is(err, errReplicasMoved):
		// Another scaler pass or an operator changed the replicas. The next pass decides again.
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
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return err
	}
	ready, err := s.cluster.ReadyOrdinals(ctx, at.Slug, namespace)
	if err != nil {
		return fmt.Errorf("list ready pods: %w", err)
	}
	readings := readPodMemory(ctx, s.sm, s.logger, at.Slug, namespace, ready)
	ran, err := s.store.WaitWithAgentLock(ctx, at.ID, scalerLockTimeout, func(ctx context.Context) error {
		return s.scaleLocked(ctx, at, namespace, ss, ready, readings, allowance.Value())
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
	ready []int, readings map[int]sessionmanager.Memory, allowance int64) error {
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
		replicas: ss.Replicas, floor: int(max(s.cfg.MinReplicas, 1)), total: at.MaxConcurrentRuns,
		counts: counts, ready: ready, readings: readings, starting: starting,
		topRuns: occupied[ss.Replicas-1], memoryMax: memoryMax(ready, readings, ss.MemoryLimitBytes),
		allowance: allowance, watermark: s.cfg.PlacementWatermark,
	})
	if want == ss.Replicas {
		return nil
	}
	if err := s.cluster.ScaleStatefulSet(ctx, at.Slug, namespace, ss.Replicas, want); err != nil {
		return err
	}
	s.logger.Info(ctx, "scaled agent pods",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "from", Value: ss.Replicas}, logging.Field{Key: "to", Value: want},
		logging.Field{Key: "pending", Value: counts.Pending}, logging.Field{Key: "starting", Value: counts.Starting},
		logging.Field{Key: "active", Value: counts.Active}, logging.Field{Key: "ready", Value: len(ready)})
	return nil
}

// scaleInput is what one scaling decision reads.
type scaleInput struct {
	replicas, floor, total int
	counts                 AgentRunCounts
	ready                  []int
	readings               map[int]sessionmanager.Memory
	starting               map[int]int
	// topRuns counts the runs on the top pod, ordinal replicas-1.
	topRuns   int
	memoryMax int64
	allowance int64
	watermark float64
}

// desiredReplicas raises the replicas to the floor, adds the pods the startable pending runs need in
// one step when no Ready pod has room, or removes the top pod when it is empty and nothing waits.
func desiredReplicas(in scaleInput) int {
	if in.replicas < in.floor {
		return in.floor
	}
	// Runs over the agent's total cannot start, so they need no pod.
	startable := min(in.counts.Pending, in.total-in.counts.Active-in.counts.Starting)
	// Pods at or above the total, after the total was lowered, are never needed for a waiting run.
	if startable > 0 && in.replicas < in.total {
		if _, ok := firstPodWithRoom(in.ready, in.replicas, in.readings, in.starting, in.allowance, in.watermark); ok {
			return in.replicas
		}
		comingUp := in.replicas - len(in.ready)
		add := podsFor(startable, in.allowance, in.watermark, in.memoryMax) - comingUp
		return max(in.replicas, min(in.replicas+add, in.total))
	}
	if in.replicas > in.floor && in.topRuns == 0 {
		return in.replicas - 1
	}
	return in.replicas
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
