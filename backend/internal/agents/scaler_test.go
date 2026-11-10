package agents

import (
	"context"
	"io"
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
			replicas: 2, floor: 2, total: 30,
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

	scaledTo   int
	switched   bool
	readLocked []bool
}

func (f *scalerFake) mustHold(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.locked {
		f.t.Errorf("%s ran without the agent lock", call)
	}
}

func (f *scalerFake) ListActiveAgentTypes(context.Context) ([]AgentType, error) { return nil, nil }

func (f *scalerFake) TryWithAgentLock(ctx context.Context, _ string, fn func(ctx context.Context) error) (bool, error) {
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
	return f.view, true, nil
}

func (f *scalerFake) ReadyOrdinals(context.Context, string, string) ([]int, error) {
	return f.ready, nil
}

func (f *scalerFake) ScaleStatefulSet(_ context.Context, _, _ string, from, to int) error {
	f.mustHold("ScaleStatefulSet")
	if from != f.view.Replicas {
		f.t.Errorf("scale from %d, want the read replicas %d", from, f.view.Replicas)
	}
	f.scaledTo = to
	return nil
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
	f := &scalerFake{t: t, view: statefulSetView{Replicas: 5, Parallel: true}, ready: []int{0, 1, 2, 3, 4}, memory: fullPod}
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
