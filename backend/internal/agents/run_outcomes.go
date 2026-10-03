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
// [from, to). The route scope agents:runs:read is a prefix rule, so a caller who
// reaches it may read every agent's runs; what remains is the delegated-run rule
// of ListRuns, applied in the query.
func (s *RunService) RunOutcomes(ctx context.Context, from, to time.Time) (RunOutcomes, error) {
	return s.repo.CountRunOutcomes(ctx, tenantFromContext(ctx), runVisibilityFromContext(ctx), from, to)
}

// handleRunOutcomes handles GET /api/v1/run-outcomes?period=24h|7d|30d|90d and an
// optional end timestamp, so a caller can cut the same window as another service.
func (c *Controller) handleRunOutcomes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period, err := usagewindow.Parse(r.URL.Query().Get("period"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, err.Error(), nil), err)
		return
	}
	end := time.Now()
	if raw := r.URL.Query().Get("end"); raw != "" {
		if end, err = time.Parse(time.RFC3339, raw); err != nil {
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, "end must be a valid RFC3339 timestamp", nil), err)
			return
		}
	}
	window := usagewindow.For(period, end)
	out, err := c.runs.RunOutcomes(ctx, window.Start, window.End)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to count run outcomes", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, out)
}
