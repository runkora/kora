package agents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func outcomesController(repo *mockRepository) *Controller {
	svc := &RunService{repo: repo}
	return &Controller{runs: svc, logger: logging.NewLoggerWithOutput(io.Discard)}
}

func getOutcomes(c *Controller, ctx context.Context, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/run-outcomes"+query, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	c.handleRunOutcomes(w, req)
	return w
}

func TestRunOutcomes_ReturnsTheCountForTheWindow(t *testing.T) {
	repo := newMockRepository()
	repo.outcomes = RunOutcomes{Completed: 4, Failed: 1}
	c := outcomesController(repo)

	before := time.Now()
	w := getOutcomes(c, withRunCallerCtx(context.Background(), "t1", "alice", ""), "?period=30d")
	var got RunOutcomes
	if err := json.NewDecoder(w.Body).Decode(&got); w.Code != http.StatusOK || err != nil || got != repo.outcomes {
		t.Fatalf("status %d body %+v err %v", w.Code, got, err)
	}
	if repo.outcomesTo.Before(before) || repo.outcomesTo.Sub(repo.outcomesFrom) != 30*24*time.Hour {
		t.Errorf("window [%s, %s)", repo.outcomesFrom, repo.outcomesTo)
	}
}

// The count runs under the run list rule: an admin sees every run, anyone else
// the delegated runs they started.
func TestRunOutcomes_UsesTheRunVisibilityRule(t *testing.T) {
	repo := newMockRepository()
	c := outcomesController(repo)

	getOutcomes(c, withRunCallerCtx(context.Background(), "t1", "alice", ""), "")
	if repo.outcomesVis != (RunVisibility{Subject: "alice"}) {
		t.Errorf("non-admin visibility = %+v", repo.outcomesVis)
	}
	getOutcomes(c, withRunCallerCtx(context.Background(), "t1", "root", rolePlatformAdmin), "")
	if !repo.outcomesVis.SeeAll {
		t.Errorf("admin visibility = %+v, want SeeAll", repo.outcomesVis)
	}
}
