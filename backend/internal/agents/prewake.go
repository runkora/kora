package agents

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/observability"
)

// A scheduled pre-wake holds the agent warm until preWakeHold after the fire. A fire more than maxPreWakeLead
// ahead is refused, so a caller cannot hold an agent's pods for long without starting runs.
const (
	preWakeHold    = 2 * time.Minute
	maxPreWakeLead = 15 * time.Minute
)

// ErrPreWakeFireAt means a pre-wake's fire time is in the past or more than maxPreWakeLead ahead.
var ErrPreWakeFireAt = errors.New("fire_at must be in the future and at most 15 minutes ahead")

// warmHolder records an agent's warm hold.
type warmHolder interface {
	HoldAgentWarm(ctx context.Context, agentTypeID string, until time.Time) error
}

// SetWarmHolds installs the store of scheduled pre-wake holds.
func (s *RunService) SetWarmHolds(h warmHolder) { s.holds = h }

// PreWake holds the agent warm ahead of a scheduled fire at fireAt (docs/adr/2026-11-22-agent-cold-tier.md, S2).
// The scaler binds the agent's pod on its next pass. The caller must be allowed to start a run of the agent.
func (s *RunService) PreWake(ctx context.Context, agentTypeID string, fireAt time.Time) error {
	ctx, span := agentsTracer.Start(ctx, "agents.PreWake", trace.WithAttributes(observability.AgentIDKey.String(agentTypeID)))
	defer span.End()
	if _, err := s.authorizeStart(ctx, span, StartRunRequest{AgentTypeID: agentTypeID}, tenantFromContext(ctx)); err != nil {
		return err
	}
	if now := time.Now(); !fireAt.After(now) || fireAt.Sub(now) > maxPreWakeLead {
		return ErrPreWakeFireAt
	}
	return s.holds.HoldAgentWarm(ctx, agentTypeID, fireAt.Add(preWakeHold))
}

// preWakeRequest is the body of POST /api/v1/runs/pre-wake.
type preWakeRequest struct {
	AgentTypeID string    `json:"agent_type_id"`
	FireAt      time.Time `json:"fire_at"`
}

// handlePreWake handles POST /api/v1/runs/pre-wake. kora-triggers calls it ahead of each schedule fire.
func (c *Controller) handlePreWake(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req preWakeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.AgentTypeID == "" {
		api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeMissingField, "agent_type_id is required", nil), nil)
		return
	}
	err := c.runs.PreWake(withCallerBearerFrom(ctx, r), req.AgentTypeID, req.FireAt)
	switch {
	case err == nil:
		api.NoBody(w, http.StatusNoContent)
	case errors.Is(err, ErrNotFound):
		api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "agent type not found", nil), nil)
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrDelegatedRunPrincipal):
		api.ErrorResponse(ctx, w, c.logger, api.ForbiddenError(api.ErrCodeForbidden, "not permitted to start runs for this agent", nil), err)
	case errors.Is(err, ErrPreWakeFireAt):
		api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeInvalidField, err.Error(), map[string]any{"field": "fire_at"}), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to pre-wake the agent", nil), err)
	}
}
