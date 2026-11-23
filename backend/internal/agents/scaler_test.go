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

func TestDesiredReplicas(t *testing.T) {
	// 1Gi pods at a 0.70 watermark fill at 716.8Mi; 180Mi per starting run.
	full := sessionmanager.Memory{WorkingSetBytes: 720 * mi, LimitBytes: 1024 * mi}
	empty := sessionmanager.Memory{WorkingSetBytes: 10 * mi, LimitBytes: 1024 * mi}
	base := func(mod func(*scaleInput)) scaleInput {
		in := scaleInput{
			replicas: 2, floor: 2, total: 30, maxReplicas: 10,
			ready:     []int{0, 1},
			readings:  map[int]sessionmanager.Memory{0: full, 1: full},
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
		{"minus pods coming up", base(func(in *scaleInput) { in.counts.Pending = 20; in.replicas = 4 }), 8},
		{"coming up covers the need", base(func(in *scaleInput) { in.counts.Pending = 4; in.replicas = 4 }), 4},
		// ceil(4 × 1024 ÷ 716.8) = 6 pods would pass the total of 4.
		{"clamped to the agent total", base(func(in *scaleInput) { in.counts.Pending = 40; in.total = 4; in.allowance = 1024 * mi }), 4},
		{"clamped to maxReplicas", base(func(in *scaleInput) { in.counts.Pending = 30; in.maxReplicas = 5 }), 5},
		{"at maxReplicas adds nothing", base(func(in *scaleInput) {
			in.counts.Pending = 30
			in.replicas = 5
			in.maxReplicas = 5
			in.ready = []int{0, 1, 2, 3, 4}
		}), 5},
		{"the total below maxReplicas clamps first", base(func(in *scaleInput) {
			in.counts.Pending = 40
			in.total = 3
			in.maxReplicas = 10
			in.allowance = 1024 * mi
		}), 3},
		{"a lowered maxReplicas removes an empty top pod while runs wait", base(func(in *scaleInput) {
			in.counts.Pending = 30
			in.replicas = 6
			in.maxReplicas = 4
			in.ready = []int{0, 1, 2, 3, 4, 5}
		}), 5},
		// Only 2 of the pending runs fit under the total: ceil(2 × 180 ÷ 716.8) = 1.
		{"runs over the total need no pod", base(func(in *scaleInput) { in.counts.Pending = 20; in.total = 10; in.counts.Active = 8 }), 3},
		{"at the total adds nothing", base(func(in *scaleInput) { in.counts.Pending = 5; in.total = 4; in.counts.Active = 4 }), 2},
		{"a Ready pod with room adds nothing", base(func(in *scaleInput) { in.counts.Pending = 20; in.readings[1] = empty }), 2},
		{"starting runs fill a pod", base(func(in *scaleInput) {
			in.counts.Pending = 1
			in.readings[1] = empty
			in.starting = map[int]int{1: 4}
		}), 3},
		{"a pod with no reading counts as full", base(func(in *scaleInput) { in.counts.Pending = 1; delete(in.readings, 1) }), 3},
		{"pods coming up cover the need", base(func(in *scaleInput) {
			in.counts.Pending = 3
			in.ready, in.readings = nil, nil
		}), 2},
		// A draining pod is not coming up: one pending run on a 1-pod agent mid-roll needs a new pod.
		{"a terminating pod is not coming up", base(func(in *scaleInput) {
			in.counts.Pending = 1
			in.replicas, in.floor = 1, 1
			in.ready, in.readings, in.terminating = nil, nil, 1
		}), 2},
		{"a terminating pod beside a Ready full pod", base(func(in *scaleInput) {
			in.counts.Pending = 1
			in.ready, in.terminating = []int{1}, 1
		}), 3},
		// An evicted pod drains for up to drainGracePeriodSecs, and its ordinal takes no new pod until it exits.
		{"an evicted only pod with an idle run gets a replacement", base(func(in *scaleInput) {
			in.replicas, in.floor, in.topRuns = 1, 1, 1
			in.counts.Active = 1
			in.ready, in.readings, in.terminating = nil, nil, 1
		}), 2},
		{"an evicted only pod with no run gets a replacement", base(func(in *scaleInput) {
			in.replicas, in.floor = 1, 1
			in.ready, in.readings, in.terminating = nil, nil, 1
		}), 2},
		{"an evicted pod's replacement stays within the total", base(func(in *scaleInput) {
			in.replicas, in.floor, in.total = 1, 1, 1
			in.ready, in.readings, in.terminating = nil, nil, 1
		}), 1},
		{"keep the empty replacement while the evicted pod drains", base(func(in *scaleInput) {
			in.floor = 1
			in.ready, in.terminating = []int{1}, 1
		}), 2},
		{"a surge roll adds no pod for its draining pod", base(func(in *scaleInput) {
			in.floor, in.rolling = 1, true
			in.ready, in.terminating = []int{1}, 1
		}), 2},
		{"keep the replacement while the evicted pod's successor comes up", base(func(in *scaleInput) {
			in.floor = 1
			in.ready = []int{1}
		}), 2},
		{"keep an empty top pod above a crashlooping pod", base(func(in *scaleInput) {
			in.replicas = 3
			in.ready = []int{0, 2}
		}), 3},
		{"a node drain at a floor of 2 replaces both pods", base(func(in *scaleInput) {
			in.ready, in.terminating = nil, 2
		}), 4},
		{"the floor holds when the total is below it", base(func(in *scaleInput) {
			in.total = 1
			in.ready, in.terminating = []int{1}, 1
		}), 2},
		{"remove a top pod that never got Ready", base(func(in *scaleInput) {
			in.replicas, in.floor = 7, 1
			in.ready = nil
		}), 6},
		{"scale down an empty top pod", base(func(in *scaleInput) { in.replicas = 5 }), 4},
		{"keep a busy top pod", base(func(in *scaleInput) { in.replicas = 5; in.topRuns = 1 }), 5},
		{"never below the floor", base(func(in *scaleInput) {}), 2},
		{"no scale down while runs wait for room", base(func(in *scaleInput) {
			in.replicas = 5
			in.counts.Pending = 1
			in.readings[1] = empty
		}), 5},
		{"scale down at the total with runs waiting", base(func(in *scaleInput) {
			in.replicas, in.total = 5, 3
			in.counts.Pending, in.counts.Active = 2, 3
		}), 4},
		{"scale down above a lowered total with runs waiting", base(func(in *scaleInput) {
			in.replicas, in.total = 5, 3
			in.counts.Pending, in.counts.Active = 5, 2
		}), 4},
		{"raised floor scales up to it", base(func(in *scaleInput) { in.floor = 4 }), 4},
		{"no scale down during a roll", base(func(in *scaleInput) { in.replicas = 5; in.rolling = true }), 5},
		{"a roll still scales up", base(func(in *scaleInput) { in.counts.Pending = 20; in.rolling = true }), 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := desiredReplicas(tc.in); got != tc.want {
				t.Errorf("desiredReplicas = %d, want %d", got, tc.want)
			}
		})
	}
}

// memory.max is a Ready pod's report, else the StatefulSet's container limit.
func TestMemoryMax(t *testing.T) {
	readings := map[int]sessionmanager.Memory{1: {LimitBytes: 2048 * mi}}
	if got := memoryMax([]int{0, 1}, readings, 1024*mi); got != 2048*mi {
		t.Errorf("with a report = %d, want the reported 2048Mi", got/mi)
	}
	if got := memoryMax([]int{0}, readings, 1024*mi); got != 1024*mi {
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
	occupied map[int]int
	view     statefulSetView
	ready    []int
	memory   func(ctx context.Context, podURL string) (sessionmanager.Memory, error)
	agents   []AgentType
	// inFlight and maxInFlight count the concurrent AgentStatefulSet reads. With gate set, each read
	// signals entered and waits for gate.
	inFlight, maxInFlight int
	gate, entered         chan struct{}
	// syncStarted, when set, makes SyncWorkloadCache signal it and block until ctx ends.
	syncStarted chan struct{}
	listCalls   int
	// live is what LivePodOrdinals reads. Nil reads the cached view.
	live      *livePodRead
	liveReads int

	scaledTo   int
	switched   bool
	released   int
	releaseErr error
	readLocked []bool
	// lastCompleted is what LastRunCompletedAt reports. shrunk and grown record the resized ordinals.
	lastCompleted time.Time
	shrunk, grown []int
	// shrinkErr is what ShrinkPod returns.
	shrinkErr error
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

func (f *scalerFake) AgentRunCounts(context.Context, string) (AgentRunCounts, error) {
	f.mustHold("AgentRunCounts")
	return f.counts, nil
}

func (f *scalerFake) StartingRunsPerPod(context.Context, string, time.Time) (map[int]int, error) {
	f.mustHold("StartingRunsPerPod")
	return nil, nil
}

func (f *scalerFake) ActiveRunsPerPod(context.Context, string) (map[int]int, error) {
	f.mustHold("ActiveRunsPerPod")
	return f.occupied, nil
}

func (f *scalerFake) AgentStatefulSet(context.Context, string, string) (statefulSetView, bool, error) {
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
	view := f.view
	view.Ready = f.ready
	return view, true, nil
}

func (f *scalerFake) ScaleStatefulSet(_ context.Context, _, _ string, from, to int, resourceVersion string) error {
	f.mustHold("ScaleStatefulSet")
	if from != f.view.Replicas || resourceVersion != f.view.ResourceVersion {
		f.t.Errorf("scale from %d at version %q, want the read replicas %d and version %q", from, resourceVersion, f.view.Replicas, f.view.ResourceVersion)
	}
	f.scaledTo = to
	return nil
}

func (f *scalerFake) ReleaseRollHold(_ context.Context, _, _ string, partition int, resourceVersion string) error {
	if resourceVersion != f.view.ResourceVersion {
		f.t.Errorf("release at version %q, want the read version %q", resourceVersion, f.view.ResourceVersion)
	}
	f.released = partition
	return f.releaseErr
}

type livePodRead struct {
	ready       []int
	terminating int
}

func (f *scalerFake) LivePodOrdinals(context.Context, string, string) ([]int, int, error) {
	f.mustHold("LivePodOrdinals")
	f.liveReads++
	if f.live == nil {
		return f.ready, f.view.Terminating, nil
	}
	return f.live.ready, f.live.terminating, nil
}

func (f *scalerFake) SwitchToParallel(context.Context, string, string) error {
	f.mustHold("SwitchToParallel")
	f.switched = true
	return nil
}

func (f *scalerFake) LastRunCompletedAt(context.Context, string) (time.Time, error) {
	f.mustHold("LastRunCompletedAt")
	return f.lastCompleted, nil
}

func (f *scalerFake) ShrinkPod(_ context.Context, _, _ string, ordinal int) error {
	f.mustHold("ShrinkPod")
	f.shrunk = append(f.shrunk, ordinal)
	return f.shrinkErr
}

func (f *scalerFake) GrowPod(_ context.Context, _, _ string, ordinal int) error {
	f.mustHold("GrowPod")
	f.grown = append(f.grown, ordinal)
	return nil
}

func (f *scalerFake) Memory(ctx context.Context, podURL string) (sessionmanager.Memory, error) {
	f.mu.Lock()
	f.readLocked = append(f.readLocked, f.locked)
	f.mu.Unlock()
	return f.memory(ctx, podURL)
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

func fullPod(context.Context, string) (sessionmanager.Memory, error) {
	return sessionmanager.Memory{WorkingSetBytes: 720 * mi, LimitBytes: 1024 * mi}, nil
}

func TestScalerScalesUnderTheAgentLock(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 20}, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 8 {
		t.Errorf("scaled to %d, want 8", f.scaledTo)
	}
	for i, locked := range f.readLocked {
		if locked {
			t.Errorf("memory read %d ran under the agent lock", i)
		}
	}
}

// A pod whose memory read blocks past the timeout is read with no lock held and counts as full.
func TestScalerMemoryTimeoutHoldsNoLock(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 1}, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}}
	f.memory = func(ctx context.Context, podURL string) (sessionmanager.Memory, error) {
		if podURL == sessionmanager.PodURL("coder", 0, scaleTestNS) {
			return fullPod(ctx, podURL)
		}
		<-ctx.Done()
		return sessionmanager.Memory{}, ctx.Err()
	}
	start := time.Now()
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < memoryReadTimeout {
		t.Errorf("returned after %s, before the %s read timeout", elapsed, memoryReadTimeout)
	}
	if len(f.readLocked) != 2 || f.readLocked[0] || f.readLocked[1] {
		t.Errorf("lock held during reads = %v, want two reads with no lock", f.readLocked)
	}
	if f.scaledTo != 3 {
		t.Errorf("scaled to %d, want 3: the timed-out pod counts as full", f.scaledTo)
	}
}

func TestScalerSkipsWhenAnotherReplicaHoldsTheLock(t *testing.T) {
	f := &scalerFake{t: t, busy: true, counts: AgentRunCounts{Pending: 20}, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 0 {
		t.Errorf("scaled to %d without the lock", f.scaledTo)
	}
}

func TestScalerScalesDownOnePodAtATime(t *testing.T) {
	f := &scalerFake{t: t, view: statefulSetView{ResourceVersion: "41", Replicas: 5, Parallel: true}, ready: []int{0, 1, 2, 3, 4}, memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 4 {
		t.Errorf("scaled to %d, want 4", f.scaledTo)
	}
	f = &scalerFake{t: t, occupied: map[int]int{4: 1}, counts: AgentRunCounts{Active: 1}, view: statefulSetView{Replicas: 5, Parallel: true}, ready: []int{0, 1, 2, 3, 4}, memory: fullPod}
	if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 0 {
		t.Errorf("scaled a busy top pod away, to %d", f.scaledTo)
	}
}

// A pass checks every agent, up to the concurrency bound at once and never more.
func TestScalerPassIsBounded(t *testing.T) {
	f := &scalerFake{t: t, busy: true, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: fullPod,
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

// A cache that has not seen pod 0 start terminating shows two Ready pods. The live read before the
// scale-down shows the top pod is the only one left above the floor, and the scaler keeps it.
func TestScalerScaleDownReadsPodsLive(t *testing.T) {
	f := &scalerFake{t: t, view: statefulSetView{ResourceVersion: "41", Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: fullPod,
		live: &livePodRead{ready: []int{1}, terminating: 1}}
	s := newTestScaler(f)
	s.cfg.MinReplicas = 1
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.liveReads != 1 || f.scaledTo != 0 {
		t.Errorf("live reads %d, scaled to %d; want one read and no scale-down below the floor", f.liveReads, f.scaledTo)
	}
	f.live = nil
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 1 {
		t.Errorf("scaled to %d with no pod terminating, want 1", f.scaledTo)
	}
}

func TestScalerSwitchesOnlyAnIdleAgentToParallel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts AgentRunCounts
		want   bool
	}{
		{"idle", AgentRunCounts{Pending: 3}, true},
		{"active run", AgentRunCounts{Active: 1}, false},
		{"starting run", AgentRunCounts{Starting: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, counts: tc.counts, view: statefulSetView{Replicas: 3}, ready: []int{0, 1, 2}, memory: fullPod}
			if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
				t.Fatal(err)
			}
			if f.switched != tc.want {
				t.Errorf("switched = %v, want %v", f.switched, tc.want)
			}
		})
	}
}

// A roll held for a surge pod is released once that pod is Ready, and the agent keeps its pods until then.
func TestScalerRollHold(t *testing.T) {
	for _, tc := range []struct {
		name         string
		view         statefulSetView
		ready        []int
		wantRelease  int
		wantScaledTo int
	}{
		{"surge pod Ready", statefulSetView{ResourceVersion: "41", Replicas: 3, Partition: 2, Parallel: true, SurgeReady: true}, []int{0, 1, 2}, 2, 0},
		// A Ready pod at the partition ordinal on the old revision is not the surge pod.
		{"old pod at the partition", statefulSetView{Replicas: 3, Partition: 2, Parallel: true}, []int{0, 1, 2}, 0, 0},
		{"surge pod not Ready yet", statefulSetView{Replicas: 3, Partition: 2, Parallel: true}, []int{0, 1}, 0, 0},
		// An older scaler removed the surge pod: no pod at the partition ordinal will come.
		{"surge pod scaled away", statefulSetView{ResourceVersion: "41", Replicas: 2, Partition: 2, Parallel: true}, []int{0, 1}, 2, 0},
		{"roll under way", statefulSetView{Replicas: 3, Rolling: true, Parallel: true}, []int{0, 2}, 0, 0},
		{"roll done", statefulSetView{ResourceVersion: "41", Replicas: 3, Parallel: true}, []int{0, 1, 2}, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, view: tc.view, ready: tc.ready, memory: fullPod}
			if err := newTestScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30}); err != nil {
				t.Fatal(err)
			}
			if f.released != tc.wantRelease || f.scaledTo != tc.wantScaledTo {
				t.Errorf("released partition %d, scaled to %d; want %d and %d", f.released, f.scaledTo, tc.wantRelease, tc.wantScaledTo)
			}
		})
	}
}

// A partition that moved under the release is left to the next pass, and only a real failure is logged.
func TestScaleAgentSwallowsMovedPartition(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		logged bool
	}{
		"moved partition": {errPartitionMoved, false},
		"real failure":    {errors.New("api down"), true},
	} {
		t.Run(name, func(t *testing.T) {
			f := &scalerFake{t: t, view: statefulSetView{Replicas: 2, Partition: 1, Parallel: true, SurgeReady: true}, ready: []int{0, 1}, memory: fullPod, releaseErr: tc.err}
			s := newTestScaler(f)
			var out bytes.Buffer
			s.logger = logging.NewLoggerWithOutput(&out)
			s.scaleAgent(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 30})
			if got := strings.Contains(out.String(), "scale agent failed"); got != tc.logged {
				t.Errorf("logged failure = %v, want %v", got, tc.logged)
			}
		})
	}
}

// warmScaler is newTestScaler with a 5 minute WarmAfter.
func warmScaler(f *scalerFake) *Scaler {
	s := newTestScaler(f)
	s.cfg.WarmAfter = 5 * time.Minute
	return s
}

func emptyPod(context.Context, string) (sessionmanager.Memory, error) {
	return sessionmanager.Memory{WorkingSetBytes: 10 * mi, LimitBytes: 1024 * mi}, nil
}

// An idle agent's hot Ready pods, and a pod whose grow is still pending, shrink once its last run completed
// WarmAfter ago, and not before, not while a run waits, not when WarmAfter is zero, and not again once warm.
func TestScalerShrinksIdleAgent(t *testing.T) {
	old, recent := time.Now().Add(-10*time.Minute), time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name     string
		mutate   func(f *scalerFake, s *Scaler)
		activate time.Time
		want     []int
	}{
		{"idle past the threshold", func(*scalerFake, *Scaler) {}, old, []int{0, 1}},
		{"last run inside the threshold", func(f *scalerFake, _ *Scaler) { f.lastCompleted = recent }, old, nil},
		{"never ran and activated inside the threshold", func(f *scalerFake, _ *Scaler) { f.lastCompleted = time.Time{} }, recent, nil},
		{"never ran and activated long ago", func(f *scalerFake, _ *Scaler) { f.lastCompleted = time.Time{} }, old, []int{0, 1}},
		{"a run waits", func(f *scalerFake, _ *Scaler) { f.counts.Pending = 1 }, old, nil},
		{"warm tier off", func(_ *scalerFake, s *Scaler) { s.cfg.WarmAfter = 0 }, old, nil},
		{"already warm", func(f *scalerFake, _ *Scaler) { f.view.Warm, f.view.Resizing = []int{0, 1}, []int{1} }, old, nil},
		{"a grow left pending shrinks back", func(f *scalerFake, _ *Scaler) { f.view.Warm, f.view.Resizing = []int{0}, []int{1} }, old, []int{1}},
		{"a pod being scaled away", func(f *scalerFake, _ *Scaler) { f.view.Replicas, f.ready = 3, []int{0, 1, 2} }, old, []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: emptyPod, lastCompleted: old}
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
		t: t, counts: AgentRunCounts{Active: 2}, occupied: map[int]int{1: 1, 2: 1},
		view: statefulSetView{Replicas: 3, Parallel: true, Warm: []int{0, 1}, Resizing: []int{2}}, ready: []int{0, 1, 2}, memory: emptyPod,
	}
	if err := warmScaler(f).scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	// Pod 2's grow is pending. GrowPod frees the wake reserve when the kubelet holds it as Deferred.
	if !slices.Equal(f.grown, []int{1, 2}) || len(f.shrunk) != 0 {
		t.Errorf("grew %v and shrunk %v, want grew [1 2]", f.grown, f.shrunk)
	}
}

// With the warm tier off, every warm pod grows back, held or not.
func TestScalerGrowsWarmPodsWithTheTierOff(t *testing.T) {
	f := &scalerFake{t: t, view: statefulSetView{Replicas: 2, Parallel: true, Warm: []int{0}, Resizing: []int{1}}, ready: []int{0, 1}, memory: emptyPod}
	s := newTestScaler(f)
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.grown, []int{0, 1}) || len(f.shrunk) != 0 {
		t.Errorf("grew %v and shrunk %v, want grew [0 1]", f.grown, f.shrunk)
	}
}

// A shrink the API server refuses for good is logged once, and the pod is not tried again.
func TestScalerSkipsPodAfterRefusedShrink(t *testing.T) {
	var logs bytes.Buffer
	f := &scalerFake{t: t, view: statefulSetView{Replicas: 1, Parallel: true}, ready: []int{0}, memory: emptyPod,
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
	f := &scalerFake{
		t: t, counts: AgentRunCounts{Pending: 1},
		view: statefulSetView{Replicas: 1, Parallel: true, Resizing: []int{0}}, ready: []int{0}, memory: emptyPod,
	}
	s := warmScaler(f)
	s.cfg.MinReplicas = 1
	if err := s.scale(context.Background(), AgentType{ID: "a", Slug: "coder", MaxConcurrentRuns: 20}); err != nil {
		t.Fatal(err)
	}
	if f.scaledTo != 2 {
		t.Errorf("scaled to %d, want 2", f.scaledTo)
	}
	if len(f.readLocked) != 0 {
		t.Errorf("memory reads = %d, want none of the resizing pod", len(f.readLocked))
	}
}
