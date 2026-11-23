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

// scalerStore is the storage the scaler reads. The run counts it decides from are read under placement's
// agent lock.
type scalerStore interface {
	ListActiveAgentTypes(ctx context.Context) ([]AgentType, error)
	GetAgentType(ctx context.Context, id string) (*AgentType, error)
	WaitWithAgentLock(ctx context.Context, agentTypeID string, timeout time.Duration, fn func(ctx context.Context) error) (bool, error)
	AgentRunCounts(ctx context.Context, agentTypeID string) (AgentRunCounts, error)
	StartingRunsPerPod(ctx context.Context, agentTypeID string, assignedSince time.Time) (map[string]int, error)
	ActiveRunsPerPod(ctx context.Context, agentTypeID string) (map[string]int, error)
	// LastRunCompletedAt is the completion time of the agent's latest completed run, zero when it has none.
	LastRunCompletedAt(ctx context.Context, agentTypeID string) (time.Time, error)
	// AgentWarmUntil is the end of the agent's scheduled pre-wake hold, zero when it has none.
	AgentWarmUntil(ctx context.Context, agentTypeID string) (time.Time, error)
}

// scalerCluster is the pod access the scaler needs. AgentPods reads a cache that SyncWorkloadCache starts.
// LiveAgentPods reads the API server.
type scalerCluster interface {
	SyncWorkloadCache(ctx context.Context) error
	AgentPods(ctx context.Context, at AgentType, namespace string) (agentPods, error)
	LiveAgentPods(ctx context.Context, at AgentType, namespace string) (agentPods, error)
	// BindPod binds a pool pod to the agent and returns its name. DeletePod drains and deletes a pod.
	BindPod(ctx context.Context, at AgentType, namespace string) (string, error)
	DeletePod(ctx context.Context, namespace, name string) error
	// RetireServiceAccount deletes the agent's ServiceAccount once no StatefulSet pod of the agent is left.
	RetireServiceAccount(ctx context.Context, at AgentType, namespace string) error
	// ShrinkPod and GrowPod set a pod's session-manager requests to the warm or the full size.
	ShrinkPod(ctx context.Context, namespace, name string) error
	GrowPod(ctx context.Context, namespace, name string) error
	// SizePool sets the tenant pool's replicas from the namespace's cold wakes.
	SizePool(ctx context.Context, namespace string, waits int32) error
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
	// ColdAfter is how long an agent with no pending, starting, active or idle run keeps its pods. Zero keeps
	// every agent at its floor.
	ColdAfter time.Duration
}

// Scaler binds and deletes each agent's pods from its runs and pod memory, and rolls the pods a pool
// template change left behind (docs/adr/2026-11-09-demand-driven-agent-scaling.md,
// docs/adr/2026-11-22-agent-cold-tier.md).
type Scaler struct {
	store   scalerStore
	cluster scalerCluster
	sm      scalerSessionManager
	cfg     ScalerConfig
	logger  logging.Logger
	// refused holds the pods whose shrink the API server refused for good.
	refused sync.Map
	// stalled counts, per agent ID, the passes in a row in which a roll changed no pod.
	stalled sync.Map
	// waits counts, per tenant namespace, the agents in this pass with a pending run and no bound pod.
	// unsized holds the namespaces in which an agent's check failed this pass, whose pool is left as it is.
	waitsMu sync.Mutex
	waits   map[string]int32
	unsized map[string]bool
}

// rollStallPasses is how many passes in a row a roll may change no pod before the scaler logs a warning.
const rollStallPasses = 5

// scalerSessionManager reads a pod's memory and its bound check.
type scalerSessionManager interface {
	podMemoryReader
	boundChecker
}

// NewScaler builds a scaler. Start runs it.
func NewScaler(store scalerStore, cluster scalerCluster, sm scalerSessionManager, cfg ScalerConfig, logger logging.Logger) *Scaler {
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
	// The roll stall counts of agents that are gone are dropped.
	listed := make(map[string]bool, len(ats))
	for _, at := range ats {
		listed[at.ID] = true
	}
	s.stalled.Range(func(id, _ any) bool {
		if !listed[id.(string)] {
			s.stalled.Delete(id)
		}
		return true
	})
	waits := make(map[string]int32)
	for _, at := range ats {
		waits[s.cfg.TenantNamespaceFn(at.TenantID)] = 0
	}
	s.waitsMu.Lock()
	s.waits, s.unsized = waits, map[string]bool{}
	s.waitsMu.Unlock()
	var g errgroup.Group
	g.SetLimit(max(s.cfg.Concurrency, 1))
	for _, at := range ats {
		g.Go(func() error {
			s.scaleAgent(ctx, at)
			return nil
		})
	}
	_ = g.Wait() // scaleAgent logs its own failures and returns none.
	s.waitsMu.Lock()
	defer s.waitsMu.Unlock()
	for namespace, n := range s.waits {
		// A failed check may have missed a cold wait, so the pool would shrink under a run about to bind.
		if s.unsized[namespace] {
			continue
		}
		if err := s.cluster.SizePool(ctx, namespace, n); err != nil {
			s.logger.Warn(ctx, "size tenant pool failed",
				logging.Field{Key: "namespace", Value: namespace}, logging.Field{Key: "error", Value: err.Error()})
		}
	}
}

// noteColdWait counts an agent with a pending run and no bound pod toward its tenant pool's size.
func (s *Scaler) noteColdWait(namespace string) {
	s.waitsMu.Lock()
	defer s.waitsMu.Unlock()
	if s.waits != nil {
		s.waits[namespace]++
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
	case errors.Is(err, errNoPoolPod):
		// The pool refills within seconds, and the next pass binds again.
		s.logger.Debug(ctx, "no pool pod to bind for the agent",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "error", Value: err.Error()})
	default:
		s.waitsMu.Lock()
		if s.unsized != nil {
			s.unsized[s.cfg.TenantNamespaceFn(at.TenantID)] = true
		}
		s.waitsMu.Unlock()
		observability.RecordFailure(span, err, errScaleAgent)
		s.logger.Warn(ctx, "scale agent failed",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "error", Value: err.Error()})
	}
}

func (s *Scaler) scale(ctx context.Context, at AgentType) error {
	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	pods, err := s.cluster.AgentPods(ctx, at, namespace)
	if err != nil {
		return err
	}
	// Every replica counts the cold wakes the same way, with no agent lock. An agent whose pod read failed is
	// not counted.
	if len(pods.Live) == 0 {
		counts, countErr := s.store.AgentRunCounts(ctx, at.ID)
		if countErr != nil {
			return countErr
		}
		held, heldErr := s.heldWarm(ctx, at)
		if heldErr != nil {
			return heldErr
		}
		// A held agent binds before its fire, so it waits on the pool as a pending run does.
		if counts.Pending > 0 || held {
			s.noteColdWait(namespace)
		}
	}
	// A pod that exited, such as one the kubelet evicted, holds its workspace claim until it is deleted.
	for _, name := range pods.Finished {
		if deleteErr := s.deletePod(ctx, namespace, name); deleteErr != nil {
			return deleteErr
		}
	}
	if err = s.cluster.RetireServiceAccount(ctx, at, namespace); err != nil {
		return err
	}
	allowance, err := s.cfg.StartingRunAllowances.For(at.Framework)
	if err != nil {
		return err
	}
	config := agentConfigHash(at)
	// A pod with a pending resize has no reading and counts as full, so a stuck grow gets a new pod.
	// The memory reads and the bound checks of a roll run before the agent lock.
	readings := readPodMemory(ctx, s.sm, s.logger, at.Slug, namespace, readable(pods.Live))
	var bound map[string]bool
	if pods.rolling(config) {
		bound = readPods(ctx, readable(pods.current(config)), func(ctx context.Context, pod agentPod) (bool, error) {
			return s.sm.Bound(ctx, sessionmanager.PodAt(pod.IP, at.Slug, namespace))
		})
	}
	ran, err := s.store.WaitWithAgentLock(ctx, at.ID, scalerLockTimeout, func(ctx context.Context) error {
		return s.scaleLocked(ctx, at, namespace, pods, config, readings, bound, allowance.Value())
	})
	if err == nil && !ran {
		s.logger.Debug(ctx, "scaler skipped agent for this pass",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "reason", Value: "agent lock wait timed out"})
	}
	return err
}

// scaleLocked runs under the agent lock: it re-reads the agent and re-counts the runs, then takes one roll
// step or moves the agent's pods toward demand. A pass that would bind or delete a pod decides again from a
// live read of the pods, as the cache can lag another replica's bind or a pod that started terminating.
// DeleteAgentType moves the agent to deleting and deletes its pods under the same lock, so an agent that is
// no longer active is left alone.
func (s *Scaler) scaleLocked(ctx context.Context, at AgentType, namespace string, pods agentPods, config string,
	readings map[string]sessionmanager.Memory, bound map[string]bool, allowance int64) error {
	if active, err := s.stillActive(ctx, at.ID); !active || err != nil {
		return err
	}
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
	replaced, err := s.replaceDeferred(ctx, at, namespace, pods.Live, occupied)
	if replaced || err != nil {
		return err
	}
	floor, err := s.floor(ctx, at, counts)
	if err != nil {
		return err
	}
	in := scaleInput{
		pods: pods.Live, floor: floor, total: at.MaxConcurrentRuns, maxPods: int(s.cfg.MaxReplicas),
		counts: counts, rolling: pods.rolling(config), readings: readings, starting: starting, occupied: occupied,
		memoryMax: memoryMax(pods.Live, readings, pods.MemoryLimitBytes), allowance: allowance, watermark: s.cfg.PlacementWatermark,
	}
	if !in.rolling && desiredPods(in) == len(in.pods) {
		s.stalled.Delete(at.ID)
		return s.settleResidency(ctx, at, namespace, pods.Live, counts, occupied)
	}
	live, err := s.cluster.LiveAgentPods(ctx, at, namespace)
	if err != nil {
		return err
	}
	in.pods, in.rolling = live.Live, live.rolling(config)
	if !in.rolling {
		s.stalled.Delete(at.ID)
	} else {
		rolled, err := s.roll(ctx, at, namespace, live, config, bound, occupied)
		s.noteRoll(ctx, at, rolled)
		if rolled || err != nil {
			return err
		}
	}
	return s.moveToDemand(ctx, at, namespace, in)
}

// moveToDemand binds the pods desiredPods adds or removes the one it drops. A cold agent's pending run is left
// to its start, which binds a pool pod and sends the "waking agent" event (docs/adr/2026-11-22-agent-cold-tier.md, D5).
func (s *Scaler) moveToDemand(ctx context.Context, at AgentType, namespace string, in scaleInput) error {
	want := desiredPods(in)
	switch {
	case want > len(in.pods) && len(in.pods) == 0 && in.counts.Pending > 0:
		return nil
	case want > len(in.pods):
		return s.bindPods(ctx, at, namespace, want-len(in.pods), in)
	}
	return s.removeEmptyPod(ctx, at, namespace, in)
}

// stillActive re-reads the agent and reports whether it is still active. A deleted agent is not.
func (s *Scaler) stillActive(ctx context.Context, id string) (bool, error) {
	current, err := s.store.GetAgentType(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current.State == AgentTypeStateActive, nil
}

// deletePod drains and deletes a pod and forgets its refused shrink.
func (s *Scaler) deletePod(ctx context.Context, namespace, name string) error {
	if err := s.cluster.DeletePod(ctx, namespace, name); err != nil {
		return err
	}
	s.refused.Delete(namespace + "/" + name)
	return nil
}

// bindPods binds n pool pods to the agent.
func (s *Scaler) bindPods(ctx context.Context, at AgentType, namespace string, n int, in scaleInput) error {
	for range n {
		name, err := s.cluster.BindPod(ctx, at, namespace)
		if name == "" {
			return err
		}
		s.logger.Info(ctx, "bound agent pod",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: name},
			logging.Field{Key: "pending", Value: in.counts.Pending}, logging.Field{Key: "starting", Value: in.counts.Starting},
			logging.Field{Key: "active", Value: in.counts.Active}, logging.Field{Key: "pods", Value: len(in.pods)})
		if err != nil {
			return err
		}
	}
	return nil
}

// removeEmptyPod deletes the pod scaleDownCandidate picks, if any.
func (s *Scaler) removeEmptyPod(ctx context.Context, at AgentType, namespace string, in scaleInput) error {
	victim, ok := scaleDownCandidate(in)
	if !ok || desiredPods(in) >= len(in.pods) {
		return nil
	}
	if err := s.deletePod(ctx, namespace, victim.Name); err != nil {
		return err
	}
	s.logger.Info(ctx, "removed empty agent pod",
		logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: victim.Name},
		logging.Field{Key: "pods", Value: len(in.pods)})
	return nil
}

// noteRoll counts the passes in a row in which the agent's roll changed no pod, and logs a warning every
// rollStallPasses such passes.
func (s *Scaler) noteRoll(ctx context.Context, at AgentType, rolled bool) {
	if rolled {
		s.stalled.Delete(at.ID)
		return
	}
	n := 1
	if v, ok := s.stalled.Load(at.ID); ok {
		n = v.(int) + 1
	}
	s.stalled.Store(at.ID, n)
	if n%rollStallPasses == 0 {
		s.logger.Warn(ctx, "agent roll made no progress; its current pods are not Ready or do not pass the bound check",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "passes", Value: n})
	}
}

// roll takes one step of moving the agent's pods to the pool's current template and the agent's current
// framework and model (docs/adr/2026-11-22-agent-cold-tier.md, O3). It binds a current pod while the
// current pods are fewer than the old ones. It deletes one old pod with the drain, an empty one first,
// once the current pods that are Ready and serve the agent are at least the old ones. At maxPods it deletes
// an old pod once one current pod serves the agent. It reports whether it changed a pod.
func (s *Scaler) roll(ctx context.Context, at AgentType, namespace string, pods agentPods, config string,
	bound map[string]bool, occupied map[string]int) (bool, error) {
	current, old := pods.current(config), pods.old(config)
	serving := 0
	for _, pod := range current {
		if pod.Ready && bound[pod.Name] {
			serving++
		}
	}
	atCeiling := len(pods.Live) >= int(s.cfg.MaxReplicas)
	if serving >= len(old) || (atCeiling && serving > 0) {
		victim := old[0]
		if i := slices.IndexFunc(old, func(pod agentPod) bool { return occupied[pod.Name] == 0 }); i >= 0 {
			victim = old[i]
		}
		if err := s.deletePod(ctx, namespace, victim.Name); err != nil {
			return true, err
		}
		s.logger.Info(ctx, "removed agent pod on an old pool template or agent config",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: victim.Name},
			logging.Field{Key: "runs", Value: occupied[victim.Name]})
		return true, nil
	}
	if len(current) >= len(old) || (atCeiling && len(current) > 0) {
		return false, nil
	}
	name, err := s.cluster.BindPod(ctx, at, namespace)
	if name != "" {
		s.logger.Info(ctx, "bound agent pod on the current pool template and agent config",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: name})
	}
	return true, err
}

// settleResidency grows a warm pod that holds a run, and shrinks the other Ready pods once the agent has
// been idle for WarmAfter (docs/adr/2026-11-09-demand-driven-agent-scaling.md, Amendment 3). A run placed
// with no grow, by a stale cache, gets its pod grown here, and a held grow frees the wake reserve. With the
// warm tier off, every warm pod grows.
func (s *Scaler) settleResidency(ctx context.Context, at AgentType, namespace string, pods []agentPod,
	counts AgentRunCounts, occupied map[string]int) error {
	if grown, err := s.growForHold(ctx, at, namespace, pods); grown || err != nil {
		return err
	}
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
	idleSince, err := s.idleSince(ctx, at)
	if err != nil {
		return err
	}
	if time.Since(idleSince) < s.cfg.WarmAfter {
		return nil
	}
	if held, err := s.heldWarm(ctx, at); held || err != nil {
		return err
	}
	return s.shrink(ctx, at, namespace, hot)
}

// growForHold grows every warm pod of an agent a scheduled pre-wake holds, so the fire finds them at full
// size. It reports whether a hold was found. The hold is read only for an agent with a warm pod.
func (s *Scaler) growForHold(ctx context.Context, at AgentType, namespace string, pods []agentPod) (bool, error) {
	if !slices.ContainsFunc(pods, func(pod agentPod) bool { return pod.Warm && !pod.Resizing }) {
		return false, nil
	}
	held, err := s.heldWarm(ctx, at)
	if !held || err != nil {
		return false, err
	}
	for _, pod := range pods {
		if !pod.Warm || pod.Resizing {
			continue
		}
		if err = s.cluster.GrowPod(ctx, namespace, pod.Name); err != nil {
			return true, err
		}
	}
	return true, nil
}

// heldWarm reports whether a scheduled pre-wake holds the agent warm now (docs/adr/2026-11-22-agent-cold-tier.md, S2).
func (s *Scaler) heldWarm(ctx context.Context, at AgentType) (bool, error) {
	until, err := s.store.AgentWarmUntil(ctx, at.ID)
	if err != nil {
		return false, err
	}
	return time.Now().Before(until), nil
}

// idleSince is when the agent's latest run ended, or when it was activated if that is later.
func (s *Scaler) idleSince(ctx context.Context, at AgentType) (time.Time, error) {
	last, err := s.store.LastRunCompletedAt(ctx, at.ID)
	if err != nil {
		return time.Time{}, err
	}
	if last.After(at.ActivatedAt) {
		return last, nil
	}
	return at.ActivatedAt, nil
}

// floor is the agent's floor of pods: MinReplicas, at least 1, while the agent is warm, and 0 once it has had
// no pending, starting, active or idle run for ColdAfter (docs/adr/2026-11-22-agent-cold-tier.md, D6).
// A cold agent's next run binds a pool pod. A scheduled pre-wake keeps the warm floor while it holds.
func (s *Scaler) floor(ctx context.Context, at AgentType, counts AgentRunCounts) (int, error) {
	warm := int(max(s.cfg.MinReplicas, 1))
	if s.cfg.ColdAfter <= 0 || counts.Pending+counts.Starting+counts.Active > 0 {
		return warm, nil
	}
	idleSince, err := s.idleSince(ctx, at)
	if err != nil {
		return 0, err
	}
	if time.Since(idleSince) < s.cfg.ColdAfter {
		return warm, nil
	}
	if held, err := s.heldWarm(ctx, at); held || err != nil {
		return warm, err
	}
	return 0, nil
}

// replaceDeferred deletes each empty pod whose grow the kubelet has held as Deferred for longer than a run
// waits for a grow. The run that waited on the grow was released, and a later bind takes the next pool pod
// (docs/adr/2026-11-22-agent-cold-tier.md, W1). It reports whether it deleted a pod.
func (s *Scaler) replaceDeferred(ctx context.Context, at AgentType, namespace string, pods []agentPod, occupied map[string]int) (bool, error) {
	replaced := false
	for _, pod := range pods {
		if pod.DeferredSince.IsZero() || time.Since(pod.DeferredSince) < defaultGrowTimeout || occupied[pod.Name] > 0 {
			continue
		}
		if err := s.deletePod(ctx, namespace, pod.Name); err != nil {
			return replaced, err
		}
		replaced = true
		s.logger.Info(ctx, "deleted an empty agent pod whose grow stayed deferred",
			logging.Field{Key: "agent_type_id", Value: at.ID}, logging.Field{Key: "pod", Value: pod.Name},
			logging.Field{Key: "deferred_since", Value: pod.DeferredSince})
	}
	return replaced, nil
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
