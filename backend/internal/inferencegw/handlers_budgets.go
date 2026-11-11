package inferencegw

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/api"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// ─── GET /v1/budgets/principals ──────────────────────────────────────────────

// handleListBudgetPrincipals handles GET /v1/budgets/principals.
//
// Returns per-principal budget status for the tenant — budget categories,
// effective limit, current period spend, and utilization — joined from the
// compiled policy bundle, the Redis attribution counters, and the usage log.
// Requires the platform:budgets:read scope, enforced by the route registration
// in inferenceRoutes(). TenantID is always sourced from the verified JWT.
func (c *Controller) handleListBudgetPrincipals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "inferencegw.Controller.handleListBudgetPrincipals")
	defer span.End()

	p, err := principalFromClaims(ctx)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(ErrTokenParseFailed), err)
		return
	}

	resp, err := c.svc.ListBudgetPrincipals(ctx, p.TenantID)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, inferenceErrorResponse(err), err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// ─── GET /v1/budgets/runs/{runID} ────────────────────────────────────────────

// handleGetRunBudget handles GET /v1/budgets/runs/{runID}?tenant_id=.
//
// Returns the spending ceiling that stopped the run. kora-agents calls it when
// a run moves to budget_exhausted (docs/adr/2026-11-11-run-page-record.md,
// Decision 6). The tenant comes from the query, so only kora-agents may call
// it. The usage lookup is scoped to that tenant: a run outside it is a 404, as
// is a run with no reached ceiling.
func (c *Controller) handleGetRunBudget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := tracer.Start(ctx, "inferencegw.Controller.handleGetRunBudget")
	defer span.End()

	if !mw.IsSystemCaller(ctx, runBudgetCaller) {
		api.ErrorResponse(ctx, w, c.logger, api.ForbiddenError(api.ErrCodeForbidden, "only "+runBudgetCaller+" may read a run's budget stop", nil), nil)
		return
	}
	runID, tenantID := r.PathValue("runID"), r.URL.Query().Get("tenant_id")
	if parsed, err := uuid.Parse(runID); err != nil || parsed.String() != runID || tenantID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError("invalid_param", "run id must be a UUID and tenant_id is required", nil), nil)
		return
	}

	budget, err := c.svc.ReachedRunBudget(ctx, tenantID, runID)
	switch {
	case errors.Is(err, ErrNoReachedBudget):
		api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(codeBudgetNotReached, "no spending ceiling of this run has been reached", nil), nil)
	case errors.Is(err, ErrUsageRecordNotFound):
		api.ErrorResponse(ctx, w, c.logger, api.NotFoundError("run_usage_not_found", "the run has no inference usage in this tenant", nil), nil)
	case err != nil:
		observability.RecordFailure(span, err, errSpanRunBudget)
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to read the run's budget", nil), err)
	default:
		api.JSON(ctx, w, c.logger, http.StatusOK, budget)
	}
}

// codeBudgetNotReached is the 404 code of a run whose ceilings are all below their limits.
const codeBudgetNotReached = "budget_not_reached"

// runBudgetCaller is the only SYSTEM component that reads a run's budget stop.
const runBudgetCaller = "kora-agents"

// errSpanRunBudget is the static span status of a failed run budget lookup.
var errSpanRunBudget = errors.New("read run budget")
