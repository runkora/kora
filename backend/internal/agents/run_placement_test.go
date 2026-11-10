package agents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
)

// watermarkBytes is the placement watermark of a mock pod.
const watermarkBytes = int64(testWatermark * testPodMemory)

// podMemory serves the given working sets, keyed by ordinal, for the pods of slug.
func podMemory(slug string, workingSets map[int]int64) map[string]sessionmanager.Memory {
	out := make(map[string]sessionmanager.Memory, len(workingSets))
	for ord, ws := range workingSets {
		out[sessionmanager.PodURL(slug, ord, "kora-tenant-t1")] = sessionmanager.Memory{WorkingSetBytes: ws, LimitBytes: testPodMemory}
	}
	return out
}

// fullPods serves the given pods of slug at the placement watermark.
func fullPods(slug string, ordinals ...int) map[string]sessionmanager.Memory {
	ws := make(map[int]int64, len(ordinals))
	for _, ord := range ordinals {
		ws[ord] = watermarkBytes
	}
	return podMemory(slug, ws)
}

func TestHasRoom(t *testing.T) {
	limit := sessionmanager.Memory{LimitBytes: testPodMemory}
	at := func(ws int64) sessionmanager.Memory { m := limit; m.WorkingSetBytes = ws; return m }
	for _, tc := range []struct {
		name     string
		mem      sessionmanager.Memory
		starting int
		want     bool
	}{
		{"below the watermark takes the run", at(watermarkBytes - 1), 0, true},
		{"at the watermark is full", at(watermarkBytes), 0, false},
		{"above the watermark is full", at(watermarkBytes + 1), 0, false},
		{"a starting run counts at the allowance", at(watermarkBytes - testRunAllowance), 1, false},
		{"a starting run below the watermark leaves room", at(watermarkBytes - testRunAllowance - 1), 1, true},
		{"no memory limit has room", sessionmanager.Memory{WorkingSetBytes: testPodMemory}, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRoom(tc.mem, tc.starting, testRunAllowance, testWatermark); got != tc.want {
				t.Errorf("hasRoom = %v, want %v", got, tc.want)
			}
		})
	}
}

// placementEnv is an agent with pods pods, maxRuns total and one pending run.
func placementEnv(t *testing.T, pods, maxRuns int) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient, *AgentType, Run) {
	t.Helper()
	f, repo, k8s, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t1", Slug: "bot", MaxConcurrentRuns: maxRuns})
	k8s.readyPods = pods
	run := seedRun(t, repo, Run{ID: "run-new", TenantID: "t1", AgentTypeID: at.ID, State: RunStatePending})
	return f, repo, k8s, sm, &at, run
}

func TestAssignPod(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(repo *mockRepository, k8s *mockK8s, sm *mockSMClient)
		want  int
		err   error
	}{
		{name: "lowest ordinal with room wins over an emptier pod",
			setup: func(_ *mockRepository, _ *mockK8s, sm *mockSMClient) {
				sm.memory = podMemory("bot", map[int]int64{0: watermarkBytes, 1: 400 << 20, 2: 0})
			},
			want: 1},
		{name: "starting runs between readings count",
			setup: func(repo *mockRepository, _ *mockK8s, sm *mockSMClient) {
				sm.memory = podMemory("bot", map[int]int64{0: watermarkBytes - 2*testRunAllowance})
				for i := range 2 {
					seedRun(t, repo, Run{ID: fmt.Sprintf("starting-%d", i), TenantID: "t1", AgentTypeID: "at-1", State: RunStateStarting, PodOrdinal: 0})
				}
			},
			want: 1},
		{name: "every pod at the watermark",
			setup: func(_ *mockRepository, _ *mockK8s, sm *mockSMClient) { sm.memory = fullPods("bot", 0, 1, 2) },
			err:   errNoFreePod},
		{name: "no placement on an ordinal at or above the current replicas",
			setup: func(_ *mockRepository, k8s *mockK8s, sm *mockSMClient) {
				// The scaler removed pods 1 and 2 after the pod list was read.
				k8s.replicas = 1
				sm.memory = fullPods("bot", 0)
			},
			err: errNoFreePod},
		{name: "the total counts live runs on every pod",
			setup: func(repo *mockRepository, _ *mockK8s, _ *mockSMClient) {
				for i, st := range []RunState{RunStateActive, RunStateIdle, RunStateStarting} {
					seedRun(t, repo, Run{ID: fmt.Sprintf("live-%d", i), TenantID: "t1", AgentTypeID: "at-1", State: st, PodOrdinal: i})
				}
			},
			err: errAgentAtTotal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, k8s, sm, at, run := placementEnv(t, 3, 3)
			tc.setup(repo, k8s, sm)
			pod, err := f.run.assignPod(context.Background(), run, time.Now(), at, "kora-tenant-t1")
			if !errors.Is(err, tc.err) {
				t.Fatalf("assignPod error = %v, want %v", err, tc.err)
			}
			if tc.err == nil && pod != tc.want {
				t.Errorf("pod = %d, want %d", pod, tc.want)
			}
			if got := repo.runs[run.ID]; (tc.err == nil) != (got.State == RunStateStarting) {
				t.Errorf("run state = %s", got.State)
			}
		})
	}
}

// Pods whose memory read times out count as full, and the reads run at once.
func TestAssignPod_MemoryReadTimeout(t *testing.T) {
	f, repo, _, sm, at, run := placementEnv(t, 3, 3)
	fast := sessionmanager.PodURL("bot", 2, "kora-tenant-t1")
	var lockedDuringRead atomic.Bool
	sm.memoryFn = func(ctx context.Context, podURL string) (sessionmanager.Memory, error) {
		if repo.agentLockHeld.Load() {
			lockedDuringRead.Store(true)
		}
		if podURL != fast {
			<-ctx.Done()
			return sessionmanager.Memory{}, ctx.Err()
		}
		return sessionmanager.Memory{LimitBytes: testPodMemory}, nil
	}

	start := time.Now()
	pod, err := f.run.assignPod(context.Background(), run, time.Now(), at, "kora-tenant-t1")
	if err != nil || pod != 2 {
		t.Fatalf("assignPod = %d, %v; want pod 2", pod, err)
	}
	// Two pods time out. Read one after the other, they would take twice the timeout.
	if elapsed := time.Since(start); elapsed > memoryReadTimeout+time.Second {
		t.Errorf("placement took %s, want about the %s read timeout", elapsed, memoryReadTimeout)
	}
	if lockedDuringRead.Load() {
		t.Error("the agent lock was held during a memory read")
	}
}

// Workers placing runs at once never take the agent past its total, whatever pod each lands on.
func TestAssignPod_TotalHoldsAcrossPods(t *testing.T) {
	f, repo, _, sm, at, _ := placementEnv(t, 2, 3)
	// Pod 0 has room for one starting run, so the total spans both pods.
	sm.memory = podMemory("bot", map[int]int64{0: watermarkBytes - 1})
	runs := make([]Run, 5)
	for i := range runs {
		runs[i] = seedRun(t, repo, Run{ID: fmt.Sprintf("burst-%d", i), TenantID: "t1", AgentTypeID: at.ID, State: RunStatePending})
	}

	var wg sync.WaitGroup
	errs := make([]error, len(runs))
	for i, run := range runs {
		wg.Go(func() {
			_, errs[i] = f.run.assignPod(context.Background(), run, time.Now(), at, "kora-tenant-t1")
		})
	}
	wg.Wait()

	placed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			placed++
		case !errors.Is(err, errAgentAtTotal):
			t.Errorf("assignPod: %v", err)
		}
	}
	if placed != 3 {
		t.Errorf("placed %d runs, want the total of 3", placed)
	}
	if got := repo.podCounts(at.ID, func(s RunState) bool { return s == RunStateStarting }); got[0] != 1 || got[1] != 2 {
		t.Errorf("starting runs per pod = %v, want map[0:1 1:2]", got)
	}
}
