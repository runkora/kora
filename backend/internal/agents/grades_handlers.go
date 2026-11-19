package agents

import (
	"errors"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
)

// handleRunGrades handles GET /api/v1/runs/{id}/grades.
func (c *Controller) handleRunGrades(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grades, err := c.runs.RunGrades(ctx, r.PathValue("id"))
	if c.gradeReadFailed(w, r, err, "run not found") {
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"grades": grades})
}

// handleAgentGraders handles GET /api/v1/agent-types/{id}/graders. An agent has few Graders, so
// the list is not paged.
func (c *Controller) handleAgentGraders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	graders, err := c.runs.AgentGraders(ctx, r.PathValue("id"))
	if c.gradeReadFailed(w, r, err, "agent type not found") {
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"graders": graders})
}

// handleAgentGrader handles GET /api/v1/agent-types/{id}/graders/{name}.
func (c *Controller) handleAgentGrader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grader, err := c.runs.AgentGrader(ctx, r.PathValue("id"), r.PathValue("name"))
	if c.gradeReadFailed(w, r, err, "grader not found") {
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, grader)
}

// gradeReadFailed writes the error response of a Grader read and reports whether it wrote one.
func (c *Controller) gradeReadFailed(w http.ResponseWriter, r *http.Request, err error, notFound string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		api.ErrorResponse(r.Context(), w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, notFound, nil), nil)
		return true
	}
	api.ErrorResponse(r.Context(), w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to read grades", nil), err)
	return true
}
