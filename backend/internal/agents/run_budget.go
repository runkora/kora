package agents

import (
	"context"
	"errors"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/inference"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/observability"
	"go.opentelemetry.io/otel/trace"
)

// runBudgetReader asks the inference gateway which ceiling a run's principal has reached.
type runBudgetReader interface {
	RunBudget(ctx context.Context, bearer, tenantID, runID string) (inference.RunBudget, error)
}

// budgetLookupTimeout bounds the lookup: it runs inside the session-manager's state callback.
const budgetLookupTimeout = 5 * time.Second

// errSpanRunBudget is the static span status of a failed budget lookup.
var errSpanRunBudget = errors.New("look up run budget")

// reachedBudget is the ceiling that stopped the run, or nil when the lookup is
// off or fails. A failure is logged: the state change is recorded without it.
// See docs/adr/2026-11-11-run-page-record.md, Decision 6.
func (s *RunService) reachedBudget(ctx context.Context, runID string) *inference.RunBudget {
	if s.budgets == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, budgetLookupTimeout)
	defer cancel()
	budget, err := s.lookUpBudget(ctx, runID)
	if err != nil {
		observability.RecordFailure(trace.SpanFromContext(ctx), err, errSpanRunBudget)
		s.logger.Warn(ctx, "budget stop recorded without its ceiling",
			logging.Field{Key: "run_id", Value: runID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	return &budget
}

func (s *RunService) lookUpBudget(ctx context.Context, runID string) (inference.RunBudget, error) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return inference.RunBudget{}, err
	}
	bearer, err := s.selfToken(ctx)
	if err != nil {
		return inference.RunBudget{}, err
	}
	return s.budgets.RunBudget(ctx, bearer, run.TenantID, runID)
}
