package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

const mi = int64(1 << 20)

// testTemplate is the pool template of the scaler tests' pods.
const testTemplate = "t1"

// readyPods returns n Ready pods named testPodName(i), oldest first, on testTemplate.
func readyPods(n int) []agentPod {
	pods := make([]agentPod, n)
	for i := range pods {
		pods[i] = agentPod{Name: testPodName(i), IP: testPodIP(i), Ready: true, Template: testTemplate, Config: agentConfigHash(scaleTestAgent)}
	}
	return pods
}

// notReady returns pods with every pod at the given indexes not Ready.
func notReady(pods []agentPod, indexes ...int) []agentPod {
	for _, i := range indexes {
		pods[i].Ready, pods[i].IP = false, ""
	}
	return pods
}

func TestDesiredPods(t *testing.T) {
	// 1Gi pods at a 0.70 watermark fill at 716.8Mi; 180Mi per starting run.
	full := sessionmanager.Memory{WorkingSetBytes: 720 * mi, LimitBytes: 1024 * mi}
	empty := sessionmanager.Memory{WorkingSetBytes: 10 * mi, LimitBytes: 1024 * mi}
	base := func(mod func(*scaleInput)) scaleInput {
		in := scaleInput{
			pods: readyPods(2), floor: 2, total: 30, maxPods: 10,
			readings:  map[string]sessionmanager.Memory{"pod-0": full, "pod-1": full},
			memoryMax: 1024 * mi, allowance: 180 * mi, watermark: 0.70,
		}
		mod(&in)
		return in
	}
	for _, tc := range []struct {
		name string
		in   scaleInput
		want int
	}{
		// ceil(20 × 180 ÷ 716.8) = 6 pods in one step.
		{"several pods in one step", base(func(in *scaleInput) { in.counts.Pending = 20 }), 8},
		{"minus pods coming up", base(func(in *scaleInput) { in.counts.Pending = 20; in.pods = notReady(readyPods(4), 2, 3) }), 8},
		{"coming up covers the need", base(func(in *scaleInput) { in.counts.Pending = 4; in.pods = notReady(readyPods(4), 2, 3) }), 4},
		// ceil(4 × 1024 ÷ 716.8) = 6 pods would pass the total of 4.
		{"clamped to the agent total", base(func(in *scaleInput) { in.counts.Pending = 40; in.total = 4; in.allowance = 1024 * mi }), 4},
		{"clamped to maxPods", base(func(in *scaleInput) { in.counts.Pending = 30; in.maxPods = 5 }), 5},
		{"at maxPods adds nothing", base(func(in *scaleInput) {
			in.counts.Pending = 30
			in.pods, in.maxPods = readyPods(5), 5
		}), 5},
		{"the total below maxPods clamps first", base(func(in *scaleInput) {
			in.counts.Pending = 40
			in.total = 3
			in.allowance = 1024 * mi
		}), 3},
		{"a lowered maxPods removes an empty pod while runs wait", base(func(in *scaleInput) {
			in.counts.Pending = 30
			in.pods, in.maxPods = readyPods(6), 4
		}), 5},
		// Only 2 of the pending runs fit under the total: ceil(2 × 180 ÷ 716.8) = 1.
		{"runs over the total need no pod", base(func(in *scaleInput) { in.counts.Pending = 20; in.total = 10; in.counts.Active = 8 }), 3},
		{"at the total adds nothing", base(func(in *scaleInput) { in.counts.Pending = 5; in.total = 4; in.counts.Active = 4 }), 2},
		{"a Ready pod with room adds nothing", base(func(in *scaleInput) { in.counts.Pending = 20; in.readings["pod-1"] = empty }), 2},
		{"starting runs fill a pod", base(func(in *scaleInput) {
			in.counts.Pending = 1
			in.readings["pod-1"] = empty
			in.starting = map[string]int{"pod-1": 4}
		}), 3},
		{"a pod with no reading counts as full", base(func(in *scaleInput) { in.counts.Pending = 1; delete(in.readings, "pod-1") }), 3},
		{"pods coming up cover the need", base(func(in *scaleInput) {
			in.counts.Pending = 3
			in.pods, in.readings = notReady(readyPods(2), 0, 1), nil
		}), 2},
		// A terminating pod is not listed. The pod an eviction or a drain took is replaced at once.
		{"an evicted only pod gets a replacement", base(func(in *scaleInput) {
			in.counts.Active, in.floor = 1, 1
			in.pods, in.readings = nil, nil
		}), 1},
		{"an evicted pod is replaced within a total of 1", base(func(in *scaleInput) {
			in.floor, in.total = 1, 1
			in.pods, in.readings = nil, nil
		}), 1},
		{"a terminating pod beside a Ready full pod", base(func(in *scaleInput) {
			in.counts.Pending = 1
			in.pods = readyPods(2)[1:]
		}), 2},
		{"keep the empty replacement while the evicted pod drains", base(func(in *scaleInput) {
			in.floor = 1
			in.pods = readyPods(2)[1:]
		}), 1},
		{"keep the Ready replacement while another pod comes up", base(func(in *scaleInput) {
			in.floor = 1
			in.pods = notReady(readyPods(2), 0)
		}), 2},
		{"keep an empty newest pod above a crashlooping pod", base(func(in *scaleInput) {
			in.pods = notReady(readyPods(3), 1)
		}), 3},
		{"a node drain at a floor of 2 replaces both pods", base(func(in *scaleInput) {
			in.pods, in.readings = nil, nil
		}), 2},
		{"the floor holds when the total is below it", base(func(in *scaleInput) {
			in.total = 1
			in.pods = readyPods(2)[1:]
		}), 2},
		{"remove a newest pod that never got Ready", base(func(in *scaleInput) {
			in.floor = 1
			in.pods = notReady(readyPods(7), 0, 1, 2, 3, 4, 5, 6)
		}), 6},
		{"scale down an empty newest pod", base(func(in *scaleInput) { in.pods = readyPods(5) }), 4},
		{"a busy newest pod leaves the next empty one to go", base(func(in *scaleInput) {
			in.pods, in.occupied = readyPods(5), map[string]int{"pod-4": 1}
		}), 4},
		{"keep the pods when every one is busy", base(func(in *scaleInput) {
			in.pods, in.occupied = readyPods(3), map[string]int{"pod-0": 1, "pod-1": 1, "pod-2": 1}
		}), 3},
		{"never below the floor", base(func(*scaleInput) {}), 2},
		{"no scale down while runs wait for room", base(func(in *scaleInput) {
			in.pods = readyPods(5)
			in.counts.Pending = 1
			in.readings["pod-1"] = empty
		}), 5},
		{"scale down at the total with runs waiting", base(func(in *scaleInput) {
			in.pods, in.total = readyPods(5), 3
			in.counts.Pending, in.counts.Active = 2, 3
		}), 4},
		{"scale down above a lowered total with runs waiting", base(func(in *scaleInput) {
			in.pods, in.total = readyPods(5), 3
			in.counts.Pending, in.counts.Active = 5, 2
		}), 4},
		{"raised floor scales up to it", base(func(in *scaleInput) { in.floor = 4 }), 4},
		{"no scale down during a roll", base(func(in *scaleInput) { in.pods = readyPods(5); in.rolling = true }), 5},
		{"a roll still scales up", base(func(in *scaleInput) { in.counts.Pending = 20; in.rolling = true }), 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := desiredPods(tc.in); got != tc.want {
				t.Errorf("desiredPods = %d, want %d", got, tc.want)
			}
		})
	}
}

// memory.max is a Ready pod's report, else the configured limit.
func TestMemoryMax(t *testing.T) {
	readings := map[string]sessionmanager.Memory{"pod-1": {LimitBytes: 2048 * mi}}
	if got := memoryMax(readyPods(2), readings, 1024*mi); got != 2048*mi {
		t.Errorf("with a report = %d, want the reported 2048Mi", got/mi)
	}
	if got := memoryMax(readyPods(1), readings, 1024*mi); got != 1024*mi {
		t.Errorf("with no Ready report = %dMi, want the 1024Mi limit", got/mi)
	}
}

// scalerFake records whether the agent lock is held at each call the scaler makes.
type scalerFake struct {
	t        *testing.T
	mu       sync.Mutex
	locked   bool
	busy     bool // another replica holds the lock
	counts   AgentRunCounts
	occupied map[string]int
	pods     agentPods
	memory   func(ctx context.Context, pod sessionmanager.Pod) (sessionmanager.Memory, error)
	agents   []AgentType
	// inFlight and maxInFlight count the concurrent AgentPods reads. With gate set, each read
	// signals entered and waits for gate.
	inFlight, maxInFlight int
	gate, entered         chan struct{}
	// syncStarted, when set, makes SyncWorkloadCache signal it and block until ctx ends.
	syncStarted chan struct{}
	listCalls   int
	// live is what LiveAgentPods reads. Nil reads the cached pods.
	live      *agentPods
	liveReads int

	// bound and deleted record the pods the scaler bound and deleted. bindErr fails every bind.
	bound, deleted []string
	bindErr        error
	readLocked     []bool
	// lastCompleted is what LastRunCompletedAt reports. shrunk and grown record the resized pods.
	lastCompleted time.Time
	shrunk, grown []string
	// shrinkErr is what ShrinkPod returns.
	shrinkErr error
	// unbound is how many bound checks answer unbound. readyAtDelete records the Ready pods left after each delete.
	unbound, boundChecks int
	readyAtDelete        []int
	// simulate adds a bound pod to the pods as Ready on the current template and config, and removes a deleted
	// pod from them, so passes run one after another see their own changes.
	simulate bool
	retired  int
	// state is what GetAgentType reads under the lock, active when empty. A deleted agent is not found.
	state AgentTypeState
	// warmUntil is what AgentWarmUntil reports.
	warmUntil time.Time
	// sized records the waits each SizePool call set, by namespace. retireErr fails RetireServiceAccount.
	sized     map[string]int32
	retireErr error
}

func (f *scalerFake) mustHold(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.locked {
		f.t.Errorf("%s ran without the agent lock", call)
	}
}

func (f *scalerFake) ListActiveAgentTypes(context.Context) ([]AgentType, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	return f.agents, nil
}

func (f *scalerFake) GetAgentType(_ context.Context, id string) (*AgentType, error) {
	f.mustHold("GetAgentType")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state == AgentTypeStateDeleted {
		return nil, ErrNotFound
	}
	if f.state != "" {
		at := AgentType{ID: id, State: f.state}
		return &at, nil
	}
	return &AgentType{ID: id, State: AgentTypeStateActive}, nil
}

func (f *scalerFake) SyncWorkloadCache(ctx context.Context) error {
	if f.syncStarted == nil {
		return nil
	}
	close(f.syncStarted)
	<-ctx.Done()
	return ctx.Err()
}

func (f *scalerFake) WaitWithAgentLock(ctx context.Context, _ string, _ time.Duration, fn func(ctx context.Context) error) (bool, error) {
	if f.busy {
		return false, nil
	}
	f.mu.Lock()
	f.locked = true
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.locked = false; f.mu.Unlock() }()
	return true, fn(ctx)
}

// AgentRunCounts is read with no lock only to count a cold wait, for an agent with no pod.
func (f *scalerFake) AgentRunCounts(context.Context, string) (AgentRunCounts, error) {
	f.mu.Lock()
	locked := f.locked
	f.mu.Unlock()
	if !locked && len(f.pods.Live) > 0 {
		f.t.Error("AgentRunCounts ran without the agent lock for an agent with pods")
	}
	return f.counts, nil
}

func (f *scalerFake) StartingRunsPerPod(context.Context, string, time.Time) (map[string]int, error) {
	f.mustHold("StartingRunsPerPod")
	return nil, nil
}

func (f *scalerFake) ActiveRunsPerPod(context.Context, string) (map[string]int, error) {
	f.mustHold("ActiveRunsPerPod")
	return f.occupied, nil
}

func (f *scalerFake) AgentPods(context.Context, AgentType, string) (agentPods, error) {
	f.mu.Lock()
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	f.mu.Unlock()
	if f.gate != nil {
		f.entered <- struct{}{}
		<-f.gate
	}
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return f.pods, nil
}

func (f *scalerFake) LiveAgentPods(context.Context, AgentType, string) (agentPods, error) {
	f.mustHold("LiveAgentPods")
	f.liveReads++
	if f.live == nil {
		return f.pods, nil
	}
	return *f.live, nil
}

func (f *scalerFake) BindPod(context.Context, AgentType, string) (string, error) {
	f.mustHold("BindPod")
	if f.bindErr != nil {
		return "", f.bindErr
	}
	name := fmt.Sprintf("bound-%d", len(f.bound))
	f.bound = append(f.bound, name)
	if f.simulate {
		f.pods.Live = append(f.pods.Live, agentPod{Name: name, IP: "10.0.1.1", Ready: true, Template: testTemplate, Config: agentConfigHash(scaleTestAgent)})
	}
	return name, nil
}

func (f *scalerFake) DeletePod(_ context.Context, _, name string) error {
	f.deleted = append(f.deleted, name)
	if f.simulate {
		f.pods.Live = slices.DeleteFunc(f.pods.Live, func(pod agentPod) bool { return pod.Name == name })
	}
	ready := 0
	for _, pod := range f.pods.Live {
		if pod.Ready && !slices.Contains(f.deleted, pod.Name) {
			ready++
		}
	}
	f.readyAtDelete = append(f.readyAtDelete, ready)
	return nil
}

func (f *scalerFake) RetireServiceAccount(context.Context, AgentType, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retired++
	return f.retireErr
}

func (f *scalerFake) SizePool(_ context.Context, namespace string, waits int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sized == nil {
		f.sized = map[string]int32{}
	}
	f.sized[namespace] = waits
	return nil
}

func (f *scalerFake) Bound(context.Context, sessionmanager.Pod) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boundChecks++
	return f.boundChecks > f.unbound, nil
}

func (f *scalerFake) LastRunCompletedAt(context.Context, string) (time.Time, error) {
	f.mustHold("LastRunCompletedAt")
	return f.lastCompleted, nil
}

func (f *scalerFake) AgentWarmUntil(context.Context, string) (time.Time, error) {
	f.mustHold("AgentWarmUntil")
	return f.warmUntil, nil
}

func (f *scalerFake) ShrinkPod(_ context.Context, _, name string) error {
	f.mustHold("ShrinkPod")
	f.shrunk = append(f.shrunk, name)
	return f.shrinkErr
}

func (f *scalerFake) GrowPod(_ context.Context, _, name string) error {
	f.mustHold("GrowPod")
	f.grown = append(f.grown, name)
	return nil
}

func (f *scalerFake) Memory(ctx context.Context, pod sessionmanager.Pod) (sessionmanager.Memory, error) {
	f.mu.Lock()
	f.readLocked = append(f.readLocked, f.locked)
	f.mu.Unlock()
	return f.memory(ctx, pod)
}

func newTestScaler(f *scalerFake) *Scaler {
	q := resource.MustParse("180Mi")
	return NewScaler(f, f, f, ScalerConfig{
		TenantNamespaceFn:     func(string) string { return scaleTestNS },
		MinReplicas:           2,
		MaxReplicas:           10,
		PlacementWatermark:    0.70,
		StartingRunAllowances: NewStartingRunAllowances(q, q, q),
	}, logging.NewLoggerWithOutput(io.Discard))
}

// scaleTestPods is an agent's pods on the current pool template.
func scaleTestPods(live []agentPod) agentPods {
	return agentPods{Live: live, Template: testTemplate, MemoryLimitBytes: 1024 * mi}
}

func fullPod(context.Context, sessionmanager.Pod) (sessionmanager.Memory, error) {
	return sessionmanager.Memory{WorkingSetBytes: 720 * mi, LimitBytes: 1024 * mi}, nil
}

var scaleTestAgent = AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}

func TestScalerBindsUnderTheAgentLock(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 20}, pods: scaleTestPods(readyPods(2)), memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 6 || len(f.deleted) != 0 {
		t.Errorf("bound %v and deleted %v, want 6 binds", f.bound, f.deleted)
	}
	for i, locked := range f.readLocked {
		if locked {
			t.Errorf("memory read %d ran under the agent lock", i)
		}
	}
}

// A pod the scaler deletes leaves no refused shrink behind, and a pass drops the roll stall count of an agent
// that is no longer listed.
func TestScalerForgetsGonePods(t *testing.T) {
	f := &scalerFake{t: t, pods: scaleTestPods(readyPods(5)), memory: fullPod}
	s := newTestScaler(f)
	s.refused.Store(scaleTestNS+"/pod-4", struct{}{})
	s.stalled.Store("gone", 3)
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.refused.Load(scaleTestNS + "/pod-4"); ok || !slices.Equal(f.deleted, []string{"pod-4"}) {
		t.Errorf("deleted %v with the refused entry kept %v, want pod-4 deleted and forgotten", f.deleted, ok)
	}
	s.pass(context.Background())
	if _, ok := s.stalled.Load("gone"); ok {
		t.Error("the stall count of an agent that is gone was kept")
	}
}

// An agent that a delete moved out of active after the pass listed it gets no bind and no delete: the scaler
// re-reads the agent under the lock that the delete takes.
func TestScalerLeavesAnAgentDeletedSinceTheList(t *testing.T) {
	for _, state := range []AgentTypeState{AgentTypeStateDeleting, AgentTypeStateDeleted} {
		f := &scalerFake{t: t, state: state, counts: AgentRunCounts{Pending: 20}, pods: scaleTestPods(readyPods(2)), memory: fullPod}
		if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
			t.Fatal(err)
		}
		if len(f.bound) != 0 || len(f.deleted) != 0 || f.liveReads != 0 {
			t.Errorf("%s: bound %v, deleted %v after %d live reads; want nothing", state, f.bound, f.deleted, f.liveReads)
		}
	}
}

// A pod whose memory read blocks past the timeout is read with no lock held and counts as full.
func TestScalerMemoryTimeoutHoldsNoLock(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 1}, pods: scaleTestPods(readyPods(2))}
	f.memory = func(ctx context.Context, pod sessionmanager.Pod) (sessionmanager.Memory, error) {
		if pod == testPod("coder", 0, scaleTestNS) {
			return fullPod(ctx, pod)
		}
		<-ctx.Done()
		return sessionmanager.Memory{}, ctx.Err()
	}
	start := time.Now()
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < memoryReadTimeout {
		t.Errorf("returned after %s, before the %s read timeout", elapsed, memoryReadTimeout)
	}
	if len(f.readLocked) != 2 || f.readLocked[0] || f.readLocked[1] {
		t.Errorf("lock held during reads = %v, want two reads with no lock", f.readLocked)
	}
	if len(f.bound) != 1 {
		t.Errorf("bound %v, want one pod: the timed-out pod counts as full", f.bound)
	}
}

func TestScalerSkipsWhenAnotherReplicaHoldsTheLock(t *testing.T) {
	f := &scalerFake{t: t, busy: true, counts: AgentRunCounts{Pending: 20}, pods: scaleTestPods(readyPods(2)), memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 0 {
		t.Errorf("bound %v without the lock", f.bound)
	}
}

// The scaler removes the newest empty pod, one per pass.
func TestScalerRemovesTheNewestEmptyPod(t *testing.T) {
	f := &scalerFake{t: t, pods: scaleTestPods(readyPods(5)), memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.deleted, []string{"pod-4"}) || f.liveReads != 1 {
		t.Errorf("deleted %v after %d live reads, want pod-4 after one", f.deleted, f.liveReads)
	}
	f = &scalerFake{t: t, occupied: map[string]int{"pod-4": 1}, counts: AgentRunCounts{Active: 1}, pods: scaleTestPods(readyPods(5)), memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.deleted, []string{"pod-3"}) {
		t.Errorf("deleted %v, want pod-3 under the busy pod-4", f.deleted)
	}
}

// A pool with no Ready pod is not a failure: the next pass binds again.
func TestScaleAgentLogsOnlyARealBindFailure(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		logged bool
	}{
		"empty pool":   {fmt.Errorf("%w in %s", errNoPoolPod, scaleTestNS), false},
		"real failure": {errors.New("api down"), true},
	} {
		t.Run(name, func(t *testing.T) {
			f := &scalerFake{t: t, pods: scaleTestPods(nil), memory: fullPod, bindErr: tc.err}
			s := newTestScaler(f)
			var out bytes.Buffer
			s.logger = logging.NewLoggerWithOutput(&out)
			s.scaleAgent(context.Background(), scaleTestAgent)
			if got := strings.Contains(out.String(), "scale agent failed"); got != tc.logged {
				t.Errorf("logged failure = %v, want %v", got, tc.logged)
			}
		})
	}
}

// A pod that exited without a delete, such as one the kubelet evicted, is deleted and replaced.
func TestScalerDeletesFinishedPods(t *testing.T) {
	pods := scaleTestPods(readyPods(1))
	pods.Finished = []string{"evicted"}
	f := &scalerFake{t: t, pods: pods, memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.deleted, []string{"evicted"}) || len(f.bound) != 1 || f.retired != 1 {
		t.Errorf("deleted %v, bound %v and %d ServiceAccount retires, want the evicted pod deleted, one bind to the floor and one retire", f.deleted, f.bound, f.retired)
	}
}

// A pass checks every agent, up to the concurrency bound at once and never more.
func TestScalerPassIsBounded(t *testing.T) {
	f := &scalerFake{t: t, busy: true, pods: scaleTestPods(readyPods(2)), memory: fullPod,
		gate: make(chan struct{}), entered: make(chan struct{}, 12)}
	for i := range 12 {
		f.agents = append(f.agents, AgentType{ID: fmt.Sprint(i), Slug: fmt.Sprintf("agent-%d", i), MaxConcurrentRuns: 30})
	}
	s := newTestScaler(f)
	s.cfg.Concurrency = 3
	done := make(chan struct{})
	go func() {
		s.pass(context.Background())
		close(done)
	}()
	// Three checks start. Each one released lets exactly one more start.
	for range 3 {
		<-f.entered
	}
	for range 9 {
		f.gate <- struct{}{}
		<-f.entered
	}
	close(f.gate)
	<-done
	if f.maxInFlight != 3 {
		t.Errorf("at most %d agents checked at once, want 3", f.maxInFlight)
	}
	if len(f.readLocked) != 24 {
		t.Errorf("%d memory reads, want 2 for each of the 12 agents", len(f.readLocked))
	}
}

// Start runs no pass before the workload cache syncs, and returns when ctx ends.
func TestScalerStartWaitsForTheCache(t *testing.T) {
	f := &scalerFake{t: t, syncStarted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		newTestScaler(f).Start(ctx)
		close(done)
	}()
	<-f.syncStarted
	cancel()
	<-done
	if f.listCalls != 0 {
		t.Errorf("Start listed agents %d times before the cache synced, want 0", f.listCalls)
	}
}

// A cache that has not seen pod-0 start terminating shows two Ready pods. The live read before the
// scale-down shows pod-1 is the only one left at the floor, and the scaler keeps it.
func TestScalerScaleDownReadsPodsLive(t *testing.T) {
	live := scaleTestPods(readyPods(2)[1:])
	f := &scalerFake{t: t, pods: scaleTestPods(readyPods(2)), memory: fullPod, live: &live}
	s := newTestScaler(f)
	s.cfg.MinReplicas = 1
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if f.liveReads != 1 || len(f.deleted) != 0 {
		t.Errorf("live reads %d, deleted %v; want one read and no scale-down below the floor", f.liveReads, f.deleted)
	}
	f.live = nil
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.deleted, []string{"pod-1"}) {
		t.Errorf("deleted %v with no pod terminating, want pod-1", f.deleted)
	}
}

// A pool template or agent config change rolls the agent's pods: current pods bind while they are fewer than
// the old ones, and an old pod goes, an empty one first, once as many current pods serve the agent.
func TestScalerRoll(t *testing.T) {
	old := func(pods []agentPod) []agentPod {
		for i := range pods {
			pods[i].Template = "t0"
		}
		return pods
	}
	fresh := func(n int) []agentPod {
		pods := readyPods(n)
		for i := range pods {
			pods[i].Name = fmt.Sprintf("new-%d", i)
		}
		return pods
	}
	for _, tc := range []struct {
		name        string
		pods        agentPods
		occupied    map[string]int
		unbound     int
		max         int32
		wantBound   int
		wantDeleted []string
	}{
		{name: "binds a pod on the current template", pods: scaleTestPods(old(readyPods(2))), wantBound: 1},
		{name: "binds until the current pods match the old ones", pods: scaleTestPods(append(old(readyPods(2)), fresh(1)...)), wantBound: 1},
		{name: "waits for the current pods to turn Ready", pods: scaleTestPods(append(old(readyPods(2)), notReady(fresh(2), 0, 1)...))},
		{name: "waits for the current pods to serve the agent", pods: scaleTestPods(append(old(readyPods(1)), fresh(1)...)), unbound: 1 << 30},
		{name: "removes an empty old pod first", pods: scaleTestPods(append(old(readyPods(2)), fresh(2)...)),
			occupied: map[string]int{"pod-0": 1}, wantDeleted: []string{"pod-1"}},
		{name: "removes a busy old pod with the drain", pods: scaleTestPods(append(old(readyPods(1)), fresh(1)...)),
			occupied: map[string]int{"pod-0": 1}, wantDeleted: []string{"pod-0"}},
		{name: "at maxPods removes an old pod once one current pod serves", pods: scaleTestPods(append(old(readyPods(2)), fresh(1)...)),
			max: 3, wantDeleted: []string{"pod-0"}},
		{name: "at maxPods with no current pod binds one past the ceiling", pods: scaleTestPods(old(readyPods(3))), max: 3, wantBound: 1},
		{name: "a framework or model change rolls a pod bound with another config", pods: scaleTestPods(func() []agentPod {
			pods := readyPods(1)
			pods[0].Config = "other"
			return pods
		}()), wantBound: 1},
		{name: "no template roll with no pool Deployment cached", pods: agentPods{Live: old(readyPods(2)), MemoryLimitBytes: 1024 * mi}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, pods: tc.pods, occupied: tc.occupied, unbound: tc.unbound, counts: AgentRunCounts{Active: len(tc.occupied)}, memory: fullPod}
			s := newTestScaler(f)
			if tc.max > 0 {
				s.cfg.MaxReplicas = tc.max
			}
			if err := s.scale(context.Background(), scaleTestAgent); err != nil {
				t.Fatal(err)
			}
			if len(f.bound) != tc.wantBound || !slices.Equal(f.deleted, tc.wantDeleted) {
				t.Errorf("bound %v and deleted %v, want %d binds and %v deleted", f.bound, f.deleted, tc.wantBound, tc.wantDeleted)
			}
		})
	}
}

// A roll that changes no pod for rollStallPasses passes in a row logs a warning.
func TestScalerWarnsOnAStalledRoll(t *testing.T) {
	pods := append(readyPods(1), agentPod{Name: "new", Template: testTemplate, Config: agentConfigHash(scaleTestAgent)})
	pods[0].Template = "t0"
	f := &scalerFake{t: t, pods: scaleTestPods(pods), memory: fullPod}
	s := newTestScaler(f)
	var logs bytes.Buffer
	s.logger = logging.NewLoggerWithOutput(&logs)
	for pass := 1; pass <= rollStallPasses; pass++ {
		if err := s.scale(context.Background(), scaleTestAgent); err != nil {
			t.Fatal(err)
		}
		if warned := strings.Contains(logs.String(), "agent roll made no progress"); warned != (pass == rollStallPasses) {
			t.Fatalf("pass %d: warned = %v", pass, warned)
		}
	}
}

// A model change on an agent with two pods binds two replacements before the first old pod goes. Pass after
// pass the agent keeps at least one Ready pod, and the current pods are never fewer than the old pods deleted.
func TestScalerModelChangeKeepsCapacity(t *testing.T) {
	pods := readyPods(2)
	for i := range pods {
		pods[i].Config = "old-model"
	}
	f := &scalerFake{t: t, pods: scaleTestPods(pods), memory: fullPod, simulate: true}
	s := newTestScaler(f)
	for pass := 0; pass < 8 && len(f.deleted) < 2; pass++ {
		if err := s.scale(context.Background(), scaleTestAgent); err != nil {
			t.Fatal(err)
		}
		if ready := len(readyOf(f.pods.Live)); ready < 1 {
			t.Fatalf("pass %d left %d Ready pods", pass, ready)
		}
		if current := len(f.pods.current(agentConfigHash(scaleTestAgent))); current < len(f.deleted) {
			t.Fatalf("pass %d: %d current pods for %d old pods deleted", pass, current, len(f.deleted))
		}
	}
	if !slices.Equal(f.deleted, []string{"pod-0", "pod-1"}) || len(f.bound) != 2 {
		t.Errorf("bound %v and deleted %v, want two binds and both old pods deleted", f.bound, f.deleted)
	}
}

// warmScaler is newTestScaler with a 5 minute WarmAfter.
func warmScaler(f *scalerFake) *Scaler {
	s := newTestScaler(f)
	s.cfg.WarmAfter = 5 * time.Minute
	return s
}

func emptyPod(context.Context, sessionmanager.Pod) (sessionmanager.Memory, error) {
	return sessionmanager.Memory{WorkingSetBytes: 10 * mi, LimitBytes: 1024 * mi}, nil
}

// residencyPods marks the pods at warm warm and the pods at resizing with a pending resize.
func residencyPods(pods []agentPod, warm, resizing []int) agentPods {
	for _, i := range warm {
		pods[i].Warm = true
	}
	for _, i := range resizing {
		pods[i].Resizing = true
	}
	return scaleTestPods(pods)
}

// An idle agent's hot Ready pods, and a pod whose grow is still pending, shrink once its last run completed
// WarmAfter ago, and not before, not while a run waits, not when WarmAfter is zero, and not again once warm.
func TestScalerShrinksIdleAgent(t *testing.T) {
	old, recent := time.Now().Add(-10*time.Minute), time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name     string
		mutate   func(f *scalerFake, s *Scaler)
		activate time.Time
		want     []string
	}{
		{"idle past the threshold", func(*scalerFake, *Scaler) {}, old, []string{"pod-0", "pod-1"}},
		{"last run inside the threshold", func(f *scalerFake, _ *Scaler) { f.lastCompleted = recent }, old, nil},
		{"never ran and activated inside the threshold", func(f *scalerFake, _ *Scaler) { f.lastCompleted = time.Time{} }, recent, nil},
		{"never ran and activated long ago", func(f *scalerFake, _ *Scaler) { f.lastCompleted = time.Time{} }, old, []string{"pod-0", "pod-1"}},
		{"a run waits", func(f *scalerFake, _ *Scaler) { f.counts.Pending = 1 }, old, nil},
		{"warm tier off", func(_ *scalerFake, s *Scaler) { s.cfg.WarmAfter = 0 }, old, nil},
		{"already warm", func(f *scalerFake, _ *Scaler) { f.pods = residencyPods(readyPods(2), []int{0, 1}, []int{1}) }, old, nil},
		{"a grow left pending shrinks back", func(f *scalerFake, _ *Scaler) { f.pods = residencyPods(readyPods(2), []int{0}, []int{1}) }, old, []string{"pod-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, pods: scaleTestPods(readyPods(2)), memory: emptyPod, lastCompleted: old}
			s := warmScaler(f)
			tc.mutate(f, s)
			if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20, ActivatedAt: tc.activate}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(f.shrunk, tc.want) || len(f.grown) != 0 {
				t.Errorf("shrunk %v and grew %v, want shrunk %v", f.shrunk, f.grown, tc.want)
			}
		})
	}
}

// A warm pod that holds a run grows without a wait, and an empty warm pod of a busy agent stays warm.
func TestScalerGrowsOccupiedWarmPod(t *testing.T) {
	f := &scalerFake{
		t: t, counts: AgentRunCounts{Active: 2}, occupied: map[string]int{"pod-1": 1, "pod-2": 1},
		pods: residencyPods(readyPods(3), []int{0, 1}, []int{2}), memory: emptyPod,
	}
	// A floor of 3 keeps the empty pod-0 from being removed.
	s := warmScaler(f)
	s.cfg.MinReplicas = 3
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	// pod-2's grow is pending. GrowPod frees the wake reserve when the kubelet holds it as Deferred.
	if !slices.Equal(f.grown, []string{"pod-1", "pod-2"}) || len(f.shrunk) != 0 {
		t.Errorf("grew %v and shrunk %v, want grew [pod-1 pod-2]", f.grown, f.shrunk)
	}
}

// With the warm tier off, every warm pod grows back, held or not.
func TestScalerGrowsWarmPodsWithTheTierOff(t *testing.T) {
	f := &scalerFake{t: t, pods: residencyPods(readyPods(2), []int{0}, []int{1}), memory: emptyPod}
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.grown, []string{"pod-0", "pod-1"}) || len(f.shrunk) != 0 {
		t.Errorf("grew %v and shrunk %v, want grew [pod-0 pod-1]", f.grown, f.shrunk)
	}
}

// A shrink the API server refuses for good is logged once, and the pod is not tried again.
func TestScalerSkipsPodAfterRefusedShrink(t *testing.T) {
	var logs bytes.Buffer
	f := &scalerFake{t: t, pods: scaleTestPods(readyPods(1)), memory: emptyPod,
		lastCompleted: time.Now().Add(-time.Hour),
		shrinkErr:     apierrors.NewGenericServerResponse(404, "patch", schema.GroupResource{Resource: "pods"}, "", "", 0, false)}
	s := warmScaler(f)
	s.cfg.MinReplicas = 1
	s.logger = logging.NewLoggerWithOutput(&logs)
	for range 3 {
		if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.shrunk) != 1 || strings.Count(logs.String(), "cannot resize in place") != 1 {
		t.Errorf("shrinks %v and %d warnings, want one of each", f.shrunk, strings.Count(logs.String(), "cannot resize in place"))
	}
}

// A pod with a pending resize counts as full, so a waiting run gets a new pod.
func TestScalerCountsResizingPodFull(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 1}, pods: residencyPods(readyPods(1), nil, []int{0}), memory: emptyPod}
	s := warmScaler(f)
	s.cfg.MinReplicas = 1
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 1 {
		t.Errorf("bound %v, want one pod", f.bound)
	}
	if len(f.readLocked) != 0 {
		t.Errorf("memory reads = %d, want none of the resizing pod", len(f.readLocked))
	}
}
