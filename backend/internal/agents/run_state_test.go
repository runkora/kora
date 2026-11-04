package agents

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/cmd/kora-agents/migrations"
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

// TestAllRunStates_MatchesDeclaredConstants reads the RunState constants out of
// models.go and requires AllRunStates to list exactly them.
//
// It parses the source rather than comparing against a literal because a
// literal would be a third hand-maintained copy of the same set: a constant
// added to models.go alone would leave AllRunStates, that literal and every
// assertion above agreeing with each other and wrong together. Go cannot
// enumerate a const block at runtime, so reading the declaration is what turns
// the omission into a test failure instead of an over-subscribed pod later.
func TestAllRunStates_MatchesDeclaredConstants(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "models.go", nil, 0)
	if err != nil {
		t.Fatalf("parse models.go: %v", err)
	}

	var declared []RunState
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		ident, ok := spec.Type.(*ast.Ident)
		if !ok || ident.Name != "RunState" {
			return true
		}
		for _, v := range spec.Values {
			lit, ok := v.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote %s: %v", lit.Value, unquoteErr)
			}
			declared = append(declared, RunState(value))
		}
		return true
	})

	if len(declared) == 0 {
		t.Fatal("found no RunState constants in models.go; this parser needs updating")
	}

	listed := make(map[RunState]bool, len(AllRunStates()))
	for _, s := range AllRunStates() {
		listed[s] = true
	}
	for _, s := range declared {
		if !listed[s] {
			t.Errorf("RunState %q is declared in models.go but missing from AllRunStates", s)
		}
		delete(listed, s)
	}
	for s := range listed {
		t.Errorf("AllRunStates lists %q, which is not a declared RunState constant", s)
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

	upSQL := embeddedIndexDefinition(t, "idx_runs_active")
	if !strings.Contains(upSQL, "WHERE state IN ("+wantList+")") {
		t.Errorf("idx_runs_active predicate does not match the query predicate %s: %s", wantList, upSQL)
	}
}

// embeddedIndexDefinition finds the CREATE INDEX statement for name in the
// kora-agents migrations, whichever file carries it.
func embeddedIndexDefinition(t *testing.T, name string) string {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		body, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		upSQL, _, _ := strings.Cut(string(body), "-- +goose Down")
		for stmt := range strings.SplitSeq(upSQL, ";") {
			if strings.Contains(stmt, "CREATE INDEX "+name+" ") {
				return strings.TrimSpace(stmt)
			}
		}
	}
	t.Fatalf("no migration creates index %s", name)
	return ""
}
