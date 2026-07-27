package triggers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// maxWebhookBodyBytes caps the inbound webhook body read to defend against
// oversized payloads. GitHub deliveries are well under this.
const maxWebhookBodyBytes = 5 << 20 // 5 MiB

// Controller wires the Triggers service to HTTP routes. It is a self-contained
// api.Controller mounted by the identity binary alongside the identity controller.
type Controller struct {
	svc      *Service
	authMW   mw.Middleware
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewController builds the Triggers controller. authMW is the platform JWT auth
// middleware (supplied by the identity service); eval/routes/basePath drive
// ScopeEnforcementMiddleware on the management routes, exactly as for identity's
// own authenticated routes.
func NewController(svc *Service, authMW mw.Middleware, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *Controller {
	return &Controller{svc: svc, authMW: authMW, eval: eval, routes: routes, basePath: basePath, logger: logger}
}

// GroupedBy returns "" — routes are registered with paths relative to the
// server base path, like the orbit-agents controller.
func (c *Controller) GroupedBy() string { return "" }

// Middleware returns nil; auth is applied per-route so the public webhook
// endpoint can bypass it (mirrors the identity controller).
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers the Triggers routes.
//
// Public (unauthenticated, HMAC-verified by the source adapter):
//
//	POST /api/v1/webhooks/{sourceID}          Inbound webhook delivery.
//
// Management (platform-admin; triggers:read / triggers:write):
//
//	POST   /api/v1/trigger-sources            Register a webhook source.
//	GET    /api/v1/trigger-sources            List webhook sources.
//	GET    /api/v1/trigger-sources/{id}       Get a webhook source.
//	PATCH  /api/v1/trigger-sources/{id}       Enable/disable or rotate secret.
//	DELETE /api/v1/trigger-sources/{id}       Delete a webhook source.
//	POST   /api/v1/triggers                   Create a trigger (any kind).
//	GET    /api/v1/triggers                   List triggers (filter ?agent_type_id= ?kind=).
//	GET    /api/v1/triggers/{id}              Get a trigger.
//	PATCH  /api/v1/triggers/{id}              Edit a trigger.
//	DELETE /api/v1/triggers/{id}              Delete a trigger.
//	GET    /api/v1/trigger-deliveries         List recent deliveries (filter ?source_id= ?trigger_id= ?kind=).
//
// Fire (requires the per-trigger trigger:{id}:fire scope, checked in the
// handler because RouteScope cannot express a path-dependent scope):
//
//	POST   /api/v1/triggers/{id}/fire         Fire a manual trigger.
func (c *Controller) Boot(mux *api.Mux) {
	// Public inbound webhook — authenticated by the per-source adapter (HMAC
	// signature for GitHub, bearer token for Alertmanager), not a platform
	// token, so it is registered without the auth chain (like /identity/device).
	mux.Handle("POST /webhooks/{sourceID}", c.handleIngest)

	auth := mw.MiddlewareChain(
		c.authMW,
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)

	mux.HandleWithMiddleware("POST /trigger-sources", auth, c.handleCreateSource)
	mux.HandleWithMiddleware("GET /trigger-sources", auth, c.handleListSources)
	mux.HandleWithMiddleware("GET /trigger-sources/{id}", auth, c.handleGetSource)
	mux.HandleWithMiddleware("PATCH /trigger-sources/{id}", auth, c.handleUpdateSource)
	mux.HandleWithMiddleware("DELETE /trigger-sources/{id}", auth, c.handleDeleteSource)

	mux.HandleWithMiddleware("POST /triggers", auth, c.handleCreateTrigger)
	mux.HandleWithMiddleware("GET /triggers", auth, c.handleListTriggers)
	mux.HandleWithMiddleware("GET /triggers/{id}", auth, c.handleGetTrigger)
	mux.HandleWithMiddleware("PATCH /triggers/{id}", auth, c.handleUpdateTrigger)
	mux.HandleWithMiddleware("DELETE /triggers/{id}", auth, c.handleDeleteTrigger)

	// The fire endpoint shares the management auth chain for authentication,
	// then checks trigger:{id}:fire in the handler. Route-level enforcement
	// cannot do it: RouteScope matches static prefixes, so this path is
	// indistinguishable from POST /api/v1/triggers (creation).
	mux.HandleWithMiddleware("POST /triggers/{id}/fire", auth, c.handleFireTrigger)

	// Schedule preview for the creation wizard. Read-only and computed from the
	// request alone — it touches no stored trigger, so it needs no more than
	// triggers:read (applied by the /triggers prefix in the route table).
	mux.HandleWithMiddleware("POST /triggers/schedule-preview", auth, c.handleSchedulePreview)

	mux.HandleWithMiddleware("GET /trigger-deliveries", auth, c.handleListDeliveries)
}

// ─── Inbound webhook ──────────────────────────────────────────────────────────

func (c *Controller) handleIngest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sourceID := r.PathValue("sourceID")

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, "could not read request body", nil), err)
		return
	}

	outcome, err := c.svc.Ingest(ctx, sourceID, r.Header, body)
	if err != nil {
		switch {
		case errors.Is(err, ErrSignatureInvalid):
			api.ErrorResponse(ctx, w, c.logger, api.UnauthorizedError(api.ErrCodeUnauthorized, "signature verification failed", nil), nil)
		case errors.Is(err, ErrMissingDeliveryID):
			api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, "delivery is missing a provider delivery id", nil), nil)
		case errors.Is(err, ErrSourceNotFound), errors.Is(err, ErrSourceDisabled):
			api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "webhook source not found", nil), nil)
		default:
			api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "failed to process delivery", nil), err)
		}
		return
	}

	if outcome == IngestDuplicate {
		api.JSON(ctx, w, c.logger, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// ─── Sources ──────────────────────────────────────────────────────────────────

func (c *Controller) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateSourceRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	resp, err := c.svc.CreateSource(ctx, tenantFromCtx(ctx), req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, resp)
}

func (c *Controller) handleListSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sources, err := c.svc.ListSources(ctx, tenantFromCtx(ctx))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"sources": sources})
}

func (c *Controller) handleGetSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := c.svc.GetSource(ctx, tenantFromCtx(ctx), r.PathValue("id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, src)
}

func (c *Controller) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req UpdateSourceRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	resp, err := c.svc.UpdateSource(ctx, tenantFromCtx(ctx), r.PathValue("id"), req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

func (c *Controller) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.svc.DeleteSource(ctx, tenantFromCtx(ctx), r.PathValue("id")); err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// ─── Triggers ─────────────────────────────────────────────────────────────────

func (c *Controller) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateTriggerRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	t, err := c.svc.CreateTrigger(ctx, tenantFromCtx(ctx), req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusCreated, t)
}

func (c *Controller) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	triggers, err := c.svc.ListTriggers(ctx, tenantFromCtx(ctx), r.URL.Query().Get("agent_type_id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"triggers": triggers})
}

func (c *Controller) handleGetTrigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := c.svc.GetTrigger(ctx, tenantFromCtx(ctx), r.PathValue("id"))
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, t)
}

func (c *Controller) handleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req UpdateTriggerRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	t, err := c.svc.UpdateTrigger(ctx, tenantFromCtx(ctx), r.PathValue("id"), req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, t)
}

func (c *Controller) handleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := c.svc.DeleteTrigger(ctx, tenantFromCtx(ctx), r.PathValue("id")); err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleFireTrigger handles POST /api/v1/triggers/{id}/fire.
//
// Firing is authorized per trigger, here rather than on the route.
// ScopeEnforcementMiddleware matches static path prefixes, so it cannot express
// a scope containing the trigger id; the route-level check it does apply
// (triggers:write, shared with trigger creation) would mean anyone who can
// manage triggers can fire any trigger.
//
// The caller's permission does not narrow what the run can do. The run executes
// under the trigger's own principal, so a caller with narrow permissions firing
// a broadly-scoped trigger gets the trigger's scope. That is deliberate — a
// manual trigger is a published capability with a fixed blast radius — and it is
// exactly why firing needs its own check (ADR Decision 6).
func (c *Controller) handleFireTrigger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	triggerID := r.PathValue("id")

	if err := c.authorizeFire(ctx, triggerID); err != nil {
		c.logger.Warn(ctx, "fire refused: caller lacks the per-trigger fire scope",
			logging.Field{Key: "trigger_id", Value: triggerID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "not authorized to fire this trigger", nil), err)
		return
	}

	var req FireTriggerRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	callerID := principalFromCtx(ctx)
	resp, err := c.svc.FireTrigger(ctx, tenantFromCtx(ctx), triggerID, callerID, req)
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusAccepted, resp)
}

// authorizeFire checks the caller holds trigger:<id>:fire, failing closed.
//
// The scope puts the identifier before the action, following the platform's
// qualified-scope convention: hierarchy is by prefix, so a grant on "trigger"
// covers every trigger and "trigger:<id>" covers every action on one. The
// inverted form (triggers:fire:<id>) would make "triggers:fire" fail to cover a
// specific trigger, turning the hierarchy upside down. No wildcard literal is
// introduced.
func (c *Controller) authorizeFire(ctx context.Context, triggerID string) error {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ErrFireForbidden
	}
	if c.eval == nil {
		// Misconfiguration, not a permission decision. Refuse rather than
		// treat an absent evaluator as an allow.
		return fmt.Errorf("%w: no policy evaluator configured", ErrFireForbidden)
	}

	result, err := c.eval.Check(ctx, policy.CheckInput{
		Claims:       claims,
		Delegation:   policy.DelegationFromContext(ctx),
		ResourceType: "trigger",
		PathSegments: []string{triggerID},
		Action:       "fire",
	})
	if err != nil {
		// Fail closed: an evaluator error must not become an allow.
		return fmt.Errorf("evaluate fire scope: %w", err)
	}
	if !result.Allow {
		return fmt.Errorf("%w: trigger:%s:fire", ErrFireForbidden, triggerID)
	}
	return nil
}

// ─── Deliveries ───────────────────────────────────────────────────────────────

func (c *Controller) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	deliveries, err := c.svc.ListDeliveries(ctx, tenantFromCtx(ctx), DeliveryFilter{
		SourceID:  r.URL.Query().Get("source_id"),
		TriggerID: r.URL.Query().Get("trigger_id"),
		Kind:      r.URL.Query().Get("kind"),
		Limit:     limit,
	})
	if err != nil {
		c.writeServiceError(w, r, err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, map[string]any{"deliveries": deliveries})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func (c *Controller) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, ErrSourceNotFound), errors.Is(err, ErrTriggerNotFound):
		api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, err.Error(), nil), nil)
	case errors.Is(err, ErrTriggerAtCapacity):
		// 429 rather than 500: the trigger is healthy, it is just busy, and the
		// caller should retry rather than treat this as a fault.
		api.ErrorResponse(ctx, w, c.logger, &api.APIError{
			Code:       "too_many_requests",
			Message:    err.Error(),
			StatusCode: http.StatusTooManyRequests,
		}, nil)
	case errors.Is(err, ErrTriggerDisabled):
		api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeResourceExists, err.Error(), nil), nil)
	case errors.Is(err, ErrWrongKind):
		api.ErrorResponse(ctx, w, c.logger, api.BadRequestError(api.ErrCodeInvalidField, err.Error(), nil), nil)
	case errors.As(err, new(*InputValidationError)):
		// Per-field detail so the console can mark the offending inputs rather
		// than showing one opaque message.
		var ive *InputValidationError
		_ = errors.As(err, &ive)
		details := make(map[string]any, len(ive.Problems))
		for k, v := range ive.Problems {
			details[k] = v
		}
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeValidationFailed, "one or more inputs are invalid", details), nil)
	case errors.Is(err, ErrValidation), errors.Is(err, ErrUnknownProvider):
		api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeValidationFailed, err.Error(), nil), nil)
	default:
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "request failed", nil), err)
	}
}

func tenantFromCtx(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ""
	}
	return claims.Orbit.TenantID
}

func principalFromCtx(ctx context.Context) string {
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok || claims == nil {
		return ""
	}
	return claims.Subject
}

// SchedulePreviewRequest asks what a cron expression would actually do.
type SchedulePreviewRequest struct {
	ScheduleConfig *ScheduleConfig `json:"schedule_config"`
}

// SchedulePreviewResponse returns the upcoming fire times in the schedule's own
// timezone, plus a human-readable rendering of each.
type SchedulePreviewResponse struct {
	NextFires []SchedulePreviewFire `json:"next_fires"`
	Timezone  string                `json:"timezone"`
}

// SchedulePreviewFire is one upcoming fire.
type SchedulePreviewFire struct {
	At    time.Time `json:"at"`
	Label string    `json:"label"`
}

// schedulePreviewCount is how many upcoming fires to show. Three is enough to
// reveal the shape of a schedule (daily, weekdays, monthly) without becoming a
// wall of timestamps.
const schedulePreviewCount = 3

// handleSchedulePreview answers "what will this actually do?" before a schedule
// is saved.
//
// A cron expression is easy to get subtly wrong, and the failure mode is a
// trigger that looks healthy and quietly never runs. Showing the next few fires
// in the schedule's own timezone turns that into something an admin can catch
// while creating it.
func (c *Controller) handleSchedulePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req SchedulePreviewRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	if req.ScheduleConfig == nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeValidationFailed, "schedule_config is required", nil), nil)
		return
	}

	fires, err := NextFires(req.ScheduleConfig, time.Now(), schedulePreviewCount)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeValidationFailed, err.Error(), nil), nil)
		return
	}

	out := make([]SchedulePreviewFire, 0, len(fires))
	for _, f := range fires {
		out = append(out, SchedulePreviewFire{At: f, Label: f.Format("Mon 2 Jan 2006, 15:04")})
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, SchedulePreviewResponse{
		NextFires: out,
		Timezone:  req.ScheduleConfig.Timezone,
	})
}
