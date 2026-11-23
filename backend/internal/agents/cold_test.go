package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newColdScaler is newTestScaler with a floor of one pod and a cold threshold of an hour.
func newColdScaler(f *scalerFake) *Scaler {
	s := newTestScaler(f)
	s.cfg.MinReplicas = 1
	s.cfg.ColdAfter = time.Hour
	return s
}

func TestScalerCold(t *testing.T) {
	long, recent := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Minute)
	tests := []struct {
		name      string
		coldAfter time.Duration
		counts    AgentRunCounts
		last      time.Time
		deleted   int
	}{
		{name: "idle past the threshold deletes the last pod", coldAfter: time.Hour, last: long, deleted: 1},
		{name: "idle within the threshold keeps the floor", coldAfter: time.Hour, last: recent},
		{name: "a zero threshold keeps the floor", last: long},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &scalerFake{t: t, counts: tt.counts, lastCompleted: tt.last, pods: scaleTestPods(readyPods(1)), memory: fullPod}
			s := newColdScaler(f)
			s.cfg.ColdAfter = tt.coldAfter
			if err := s.scale(context.Background(), scaleTestAgent); err != nil {
				t.Fatal(err)
			}
			if len(f.deleted) != tt.deleted {
				t.Errorf("deleted %v, want %d deletes", f.deleted, tt.deleted)
			}
		})
	}
}

// A cold agent with no pod binds nothing until a run waits for it.
func TestScalerColdAgentWithNoPod(t *testing.T) {
	f := &scalerFake{t: t, lastCompleted: time.Now().Add(-2 * time.Hour), pods: scaleTestPods(nil), memory: fullPod}
	s := newColdScaler(f)
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 0 {
		t.Fatalf("bound %v for a cold agent with no run", f.bound)
	}
	f.counts = AgentRunCounts{Pending: 1}
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 1 {
		t.Errorf("bound %v, want one pod for the pending run", f.bound)
	}
}

func TestScalerReplacesADeferredEmptyPod(t *testing.T) {
	long, recent := time.Now().Add(-time.Minute), time.Now().Add(-time.Second)
	tests := []struct {
		name     string
		since    time.Time
		occupied int
		deleted  bool
	}{
		{name: "deferred past the grow timeout", since: long, deleted: true},
		{name: "deferred within the grow timeout", since: recent},
		{name: "a pod holding a run", since: long, occupied: 1},
		{name: "a grow that is not deferred"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pods := readyPods(2)
			pods[1].DeferredSince = tt.since
			f := &scalerFake{t: t, counts: AgentRunCounts{Active: tt.occupied}, pods: scaleTestPods(pods), memory: fullPod,
				occupied: map[string]int{pods[0].Name: 1, pods[1].Name: tt.occupied}}
			f.counts.Active++
			// A floor of two keeps the empty pod from the scale-down.
			s := newColdScaler(f)
			s.cfg.MinReplicas = 2
			if err := s.scale(context.Background(), scaleTestAgent); err != nil {
				t.Fatal(err)
			}
			got := len(f.deleted) == 1 && f.deleted[0] == pods[1].Name
			if got != tt.deleted || (!tt.deleted && len(f.deleted) != 0) {
				t.Errorf("deleted %v, want the deferred pod deleted: %t", f.deleted, tt.deleted)
			}
		})
	}
}

// A pass sizes the pool of every tenant it checks from the agents with a pending run and no pod.
func TestScalerPassSizesThePool(t *testing.T) {
	cold := AgentType{ID: "b", Slug: "cold", MaxConcurrentRuns: 1}
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 1}, pods: scaleTestPods(nil), memory: fullPod,
		agents: []AgentType{cold}, bindErr: errNoPoolPod}
	s := newColdScaler(f)
	s.pass(context.Background())
	if f.sized[scaleTestNS] != 1 {
		t.Errorf("sized %v, want one cold wait in %s", f.sized, scaleTestNS)
	}
	f.counts = AgentRunCounts{}
	s.pass(context.Background())
	if waits, ok := f.sized[scaleTestNS]; !ok || waits != 0 {
		t.Errorf("sized %v, want the pool sized back with no waits", f.sized)
	}
}

// The pool grows by the cold waits, whatever its Ready pods, and is left alone at that size. Its Ready pods
// already count in its replicas.
func TestSizePool(t *testing.T) {
	tests := []struct {
		name  string
		ready int
		waits int32
		want  int32
	}{
		{name: "no waits keeps the size", waits: 0, want: 2},
		{name: "waits with no Ready pool pod", waits: 3, want: 5},
		{name: "waits with Ready pool pods", ready: 2, waits: 2, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPoolFixture()
			replicas := int32(2)
			f.dep.Spec.Replicas = &replicas
			objs := []runtime.Object{f.dep, f.rs}
			for i := range tt.ready {
				objs = append(objs, f.pod(fmt.Sprintf("pool-%d", i), testTemplate, i))
			}
			p, client, _ := bindProvisioner(t, objs...)
			p.cfg.PoolSize = 2
			updates := 0
			client.PrependReactor("update", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
				updates++
				return false, nil, nil
			})
			if err := p.SizePool(t.Context(), scaleTestNS, tt.waits); err != nil {
				t.Fatal(err)
			}
			dep, err := client.AppsV1().Deployments(scaleTestNS).Get(t.Context(), poolName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if *dep.Spec.Replicas != tt.want {
				t.Errorf("replicas = %d, want %d", *dep.Spec.Replicas, tt.want)
			}
			if wantUpdates := map[bool]int{true: 0, false: 1}[tt.want == 2]; updates != wantUpdates {
				t.Errorf("updates = %d, want %d", updates, wantUpdates)
			}
		})
	}
}

func TestDeferredSince(t *testing.T) {
	at := metav1.NewTime(time.Unix(100, 0))
	pod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
		Type: corev1.PodResizePending, Status: corev1.ConditionTrue, Reason: corev1.PodReasonDeferred, LastTransitionTime: at,
	}}}}
	if got := deferredSince(pod); !got.Equal(at.Time) {
		t.Errorf("deferredSince = %v, want %v", got, at.Time)
	}
	pod.Status.Conditions[0].Reason = corev1.PodReasonInfeasible
	if got := deferredSince(pod); !got.IsZero() {
		t.Errorf("deferredSince of an infeasible resize = %v, want zero", got)
	}
}

// fakeColdWaker binds pool pods by name and reports live pods.
type fakeColdWaker struct {
	live    []agentPod
	bindErr error
	bound   []string
}

func (w *fakeColdWaker) LiveAgentPods(context.Context, AgentType, string) (agentPods, error) {
	return agentPods{Live: w.live}, nil
}

func (w *fakeColdWaker) BindPod(context.Context, AgentType, string) (string, error) {
	if w.bindErr != nil {
		return "", w.bindErr
	}
	name := testPodName(len(w.bound))
	w.bound = append(w.bound, name)
	return name, nil
}

// A run of an agent with no pod binds a pool pod, sends the waking event and starts on the pod once its grow
// and bound check are done.
func TestStartClaimed_ColdWake(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cold         *fakeColdWaker
		wantState    RunState
		wantBinds    int
		wantReleased int
		wantProvMiss int
	}{
		{name: "binds a pool pod and starts the run", cold: &fakeColdWaker{},
			wantState: RunStateActive, wantBinds: 1},
		{name: "a pod another start bound keeps the run waiting", cold: &fakeColdWaker{live: readyPods(1)},
			wantState: RunStatePending, wantReleased: 1},
		{name: "an empty pool keeps the run waiting", cold: &fakeColdWaker{bindErr: errNoPoolPod},
			wantState: RunStatePending, wantReleased: 1},
		{name: "a tenant with no pool deployment provisions it", cold: &fakeColdWaker{bindErr: errNoPoolDeployment},
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "a refused bind fails the run", cold: &fakeColdWaker{bindErr: fmt.Errorf("%w: forbidden", errBindRefused)},
			wantState: RunStateFailed},
		{name: "a failed bind releases the run", cold: &fakeColdWaker{bindErr: errors.New("connection reset")},
			wantState: RunStatePending, wantReleased: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, k8s, sm := schedulerService(t)
			k8s.noReadyPods = true
			waker := &fakeWaker{}
			f.run.SetPodWaker(waker)
			f.run.SetColdWaker(tc.cold)
			run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1", InitialMessage: "fix the build"})
			if err != nil {
				t.Fatal(err)
			}
			claimed, claimedAt := claimRun(repo, run.ID)
			f.run.startClaimed(context.Background(), claimed, claimedAt)

			if state := repo.runs[run.ID].State; state != tc.wantState {
				t.Errorf("state = %s, want %s", state, tc.wantState)
			}
			got := []int{len(tc.cold.bound), repo.released, len(k8s.provisionedMissing)}
			if want := []int{tc.wantBinds, tc.wantReleased, tc.wantProvMiss}; !slices.Equal(got, want) {
				t.Errorf("binds,released,provisions = %v, want %v", got, want)
			}
			var waking int
			repo.mu.Lock()
			for _, e := range repo.events[run.ID] {
				if e.EventType == eventAgentWaking {
					waking++
				}
			}
			repo.mu.Unlock()
			if waking != tc.wantBinds {
				t.Errorf("waking events = %d, want %d", waking, tc.wantBinds)
			}
			if tc.wantBinds == 1 {
				if len(waker.warm) != 1 || !waker.warm[0] || len(sm.sessions) != 1 {
					t.Errorf("grows %v and sessions %d, want one waited grow before one session", waker.warm, len(sm.sessions))
				}
			}
		})
	}
}

// The floor is the warm floor while any run occupies or waits for a pod, and 0 once the agent is cold.
func TestScalerFloor(t *testing.T) {
	long := time.Now().Add(-2 * time.Hour)
	for _, tc := range []struct {
		name   string
		counts AgentRunCounts
		want   int
	}{
		{name: "cold", want: 0},
		{name: "a pending run", counts: AgentRunCounts{Pending: 1}, want: 1},
		{name: "a starting run", counts: AgentRunCounts{Starting: 1}, want: 1},
		{name: "an active, idle or completing run", counts: AgentRunCounts{Active: 1}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &scalerFake{t: t, lastCompleted: long, locked: true}
			got, err := newColdScaler(f).floor(context.Background(), scaleTestAgent, tc.counts)
			if err != nil || got != tc.want {
				t.Errorf("floor = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// With run starts waking cold agents, the scaler binds no pod for a cold agent's pending run.
func TestScalerLeavesColdWakesToRunStarts(t *testing.T) {
	f := &scalerFake{t: t, counts: AgentRunCounts{Pending: 1}, lastCompleted: time.Now().Add(-2 * time.Hour), pods: scaleTestPods(nil), memory: fullPod}
	s := newColdScaler(f)
	s.cfg.RunsWakeColdAgents = true
	if err := s.scale(context.Background(), scaleTestAgent); err != nil {
		t.Fatal(err)
	}
	if len(f.bound) != 0 {
		t.Errorf("bound %v, want the run's start to wake the agent", f.bound)
	}
}

// A cold wake whose pod is not in the cache yet waits for its grow there instead of skipping it.
func TestWakePodWaitsForAnUncachedPod(t *testing.T) {
	p := cachedProvisioner(t, fake.NewClientset())
	p.growTimeout = 100 * time.Millisecond
	if err := p.WakePod(t.Context(), scaleTestNS, "pool-new", true); !errors.Is(err, errGrowTimeout) {
		t.Errorf("WakePod = %v, want errGrowTimeout after waiting for the pod", err)
	}
	if err := p.WakePod(t.Context(), scaleTestNS, "pool-new", false); err != nil {
		t.Errorf("WakePod of an uncached pod placement did not see warm = %v, want nil", err)
	}
}

// deletedAfterRead reports the agent deleting from its second read on, as when the agent is deleted while
// its run's start waits for the agent lock.
type deletedAfterRead struct {
	AgentTypeRepository
	reads int
}

func (r *deletedAfterRead) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	at, err := r.AgentTypeRepository.GetAgentType(ctx, id)
	if r.reads++; err == nil && r.reads > 1 {
		deleting := *at
		deleting.State = AgentTypeStateDeleting
		return &deleting, nil
	}
	return at, err
}

func TestStartClaimed_ColdWakeOfADeletedAgent(t *testing.T) {
	f, repo, k8s, _ := schedulerService(t)
	k8s.noReadyPods = true
	cold := &fakeColdWaker{}
	f.run.SetColdWaker(cold)
	run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err != nil {
		t.Fatal(err)
	}
	f.run.atRepo = &deletedAfterRead{AgentTypeRepository: f.run.atRepo}
	claimed, claimedAt := claimRun(repo, run.ID)
	f.run.startClaimed(context.Background(), claimed, claimedAt)
	if state := repo.runs[run.ID].State; state != RunStateFailed || len(cold.bound) != 0 {
		t.Errorf("state %s with binds %v, want a failed run and no bind", state, cold.bound)
	}
}
