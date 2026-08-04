package agents

import (
	"os"
	"strings"
	"testing"
)

// TestRunStateOccupiesPod_AnswersEveryDeclaredState is the guard that matters:
// a RunState added to the constants without a decision about pod occupancy
// falls into the default branch and fails here, rather than silently counting
// as "does not occupy" and over-subscribing pods.
func TestRunStateOccupiesPod_AnswersEveryDeclaredState(t *testing.T) {
	t.Parallel()

	for _, s := range AllRunStates() {
		if _, known := s.OccupiesPod(); !known {
			t.Errorf("RunState %q is not answered by OccupiesPod", s)
		}
	}
}

func TestRunStateOccupiesPod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state RunState
		want  bool
	}{
		{state: RunStatePending, want: true},
		{state: RunStateStarting, want: true},
		{state: RunStateActive, want: true},
		// idle keeps the session process alive waiting for the next message.
		{state: RunStateIdle, want: true},
		{state: RunStateCompleting, want: true},
		{state: RunStateCompleted, want: false},
		{state: RunStateFailed, want: false},
		// budget_exhausted is terminal: the sandbox is deprovisioned with it.
		{state: RunStateBudgetExhausted, want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			got, known := tt.state.OccupiesPod()
			if !known {
				t.Fatalf("state %q reported unknown", tt.state)
			}
			if got != tt.want {
				t.Errorf("OccupiesPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunStateOccupiesPod_UnknownState(t *testing.T) {
	t.Parallel()

	if occupies, known := RunState("teleporting").OccupiesPod(); occupies || known {
		t.Errorf("OccupiesPod() = (%v, %v), want (false, false)", occupies, known)
	}
}

// TestAllRunStates_MatchesDeclaredConstants pins the declared set so a new
// constant that is not added to AllRunStates is caught here rather than
// slipping past the exhaustiveness test above.
func TestAllRunStates_MatchesDeclaredConstants(t *testing.T) {
	t.Parallel()

	want := []RunState{
		"pending", "starting", "active", "idle",
		"completing", "completed", "failed", "budget_exhausted",
	}

	got := AllRunStates()
	if len(got) != len(want) {
		t.Fatalf("AllRunStates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AllRunStates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestPodOccupancySQLList_MatchesPartialIndex keeps the occupancy query and the
// partial index it relies on in step: idx_runs_active only serves the per-pod
// count while its predicate lists exactly the same states.
func TestPodOccupancySQLList_MatchesPartialIndex(t *testing.T) {
	t.Parallel()

	const wantList = "'pending', 'starting', 'active', 'idle', 'completing'"

	if got := podOccupancySQLList(); got != wantList {
		t.Fatalf("podOccupancySQLList() = %s, want %s", got, wantList)
	}
	if !strings.Contains(activeRunsPerPodQuery, wantList) {
		t.Errorf("activeRunsPerPodQuery does not filter on %s: %s", wantList, activeRunsPerPodQuery)
	}

	const migration = "../../cmd/orbit-agents/migrations/20260804065123_widen_runs_active_index_to_idle.sql"
	body, err := os.ReadFile(migration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	upSQL, _, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatal("migration has no Down section")
	}
	if !strings.Contains(upSQL, "WHERE state IN ("+wantList+")") {
		t.Errorf("idx_runs_active predicate does not match the query predicate %s", wantList)
	}
}
