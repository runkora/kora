package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

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
	// inFlight and maxInFlight count the concurrent AgentStatefulSet reads. hold delays each read.
	inFlight, maxInFlight int
	hold                  time.Duration

	scaledTo   int
	switched   bool
	released   int
	releaseErr error
	readLocked []bool
}

func (f *scalerFake) mustHold(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.locked {
		f.t.Errorf("%s ran without the agent lock", call)
	}
}

func (f *scalerFake) ListActiveAgentTypes(context.Context) ([]AgentType, error) { return f.agents, nil }

func (f *scalerFake) SyncWorkloadCache(context.Context) error { return nil }

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
	time.Sleep(f.hold)
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

func (f *scalerFake) ReleaseRollHold(_ context.Context, _, _ string, partition int) error {
	f.released = partition
	return f.releaseErr
}

func (f *scalerFake) SwitchToParallel(context.Context, string, string) error {
	f.mustHold("SwitchToParallel")
	f.switched = true
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
	f := &scalerFake{t: t, busy: true, view: statefulSetView{Replicas: 2, Parallel: true}, ready: []int{0, 1}, memory: fullPod, hold: 20 * time.Millisecond}
	for i := range 12 {
		f.agents = append(f.agents, AgentType{ID: fmt.Sprint(i), Slug: fmt.Sprintf("agent-%d", i), MaxConcurrentRuns: 30})
	}
	s := newTestScaler(f)
	s.cfg.Concurrency = 3
	s.pass(context.Background())
	if f.maxInFlight != 3 {
		t.Errorf("at most %d agents checked at once, want 3", f.maxInFlight)
	}
	if len(f.readLocked) != 24 {
		t.Errorf("%d memory reads, want 2 for each of the 12 agents", len(f.readLocked))
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
		{"surge pod Ready", statefulSetView{Replicas: 3, Partition: 2, Parallel: true, SurgeReady: true}, []int{0, 1, 2}, 2, 0},
		// A Ready pod at the partition ordinal on the old revision is not the surge pod.
		{"old pod at the partition", statefulSetView{Replicas: 3, Partition: 2, Parallel: true}, []int{0, 1, 2}, 0, 0},
		{"surge pod not Ready yet", statefulSetView{Replicas: 3, Partition: 2, Parallel: true}, []int{0, 1}, 0, 0},
		// An older scaler removed the surge pod: no pod at the partition ordinal will come.
		{"surge pod scaled away", statefulSetView{Replicas: 2, Partition: 2, Parallel: true}, []int{0, 1}, 2, 0},
		{"roll under way", statefulSetView{Replicas: 3, Rolling: true, Parallel: true}, []int{0, 2}, 0, 0},
		{"roll done", statefulSetView{Replicas: 3, Parallel: true}, []int{0, 1, 2}, 0, 2},
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
