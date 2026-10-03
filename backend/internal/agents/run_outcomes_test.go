package agents

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// The count runs under the run list rule: an admin sees every run, anyone else
// the delegated runs they started.
func TestRunOutcomes_UsesTheRunVisibilityRule(t *testing.T) {
	repo := newMockRepository()
	c := &Controller{runs: &RunService{repo: repo}, logger: logging.NewLoggerWithOutput(io.Discard)}
	for _, tc := range []struct {
		roles string
		want  RunVisibility
	}{{"", RunVisibility{Subject: "alice"}}, {rolePlatformAdmin, RunVisibility{SeeAll: true}}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/run-outcomes", nil).
			WithContext(withRunCallerCtx(context.Background(), "t1", "alice", tc.roles))
		if c.handleRunOutcomes(httptest.NewRecorder(), req); repo.outcomesVis != tc.want {
			t.Errorf("roles %q: visibility %+v, want %+v", tc.roles, repo.outcomesVis, tc.want)
		}
	}
}
