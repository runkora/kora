package agents

import (
	"context"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// RunOutcomes counts the runs that finished in a window.
type RunOutcomes struct {
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
}

// RunOutcomes counts the caller's visible runs that completed or failed in
// [from, to), under the delegated-run rule of ListRuns. The route scope
// agents:runs:read is a prefix rule, so no per-agent check is needed.
func (s *RunService) RunOutcomes(ctx context.Context, from, to time.Time) (RunOutcomes, error) {
	return s.repo.CountRunOutcomes(ctx, tenantFromContext(ctx), runVisibilityFromContext(ctx), from, to)
}

// handleRunOutcomes handles GET /api/v1/run-outcomes?period=24h|7d|30d|90d.
func (c *Controller) handleRunOutcomes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, err.Error(), nil), err)
		return
	}
	window := usagewindow.For(period, time.Now())
	out, err := c.runs.RunOutcomes(ctx, window.Start, window.End)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to count run outcomes", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
