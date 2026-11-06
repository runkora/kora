package declarative

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// OperatorComponent is the SYSTEM principal name the operator's platform token
// carries, from its SPIFFE ID /system/operator.
const OperatorComponent = "operator"

// Controller serves the declared-item endpoints for the kinds a service owns:
//
//	POST   /internal/declared/{kind}
//	DELETE /internal/declared/{kind}/{tenant}/{name}
//	GET    /internal/declared/{kind}/{tenant}
//
// Paths sit under the service's base path. Only the operator may call them.
type Controller struct {
	appliers map[string]Applier
	auth     mw.Middleware
	logger   logging.Logger
	// events records an apply that was not projected. Nil records nothing.
	events *storage.Pool
}

// NewController returns a Controller serving one Applier per kind, keyed by
// the Kind constants.
func NewController(verifier *token.Verifier, logger logging.Logger, appliers map[string]Applier) *Controller {
	return &Controller{appliers: appliers, auth: mw.AuthMiddleware(verifier, logger), logger: logger}
}

// WithEvents records declarative.resource.failed.v1 for every apply whose
// result is not Projected.
func (c *Controller) WithEvents(pool *storage.Pool) *Controller {
	c.events = pool
	return c
}

// GroupedBy implements api.Controller.
func (c *Controller) GroupedBy() string { return "" }

// Middleware verifies the platform token and admits only the operator.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(c.auth, mw.RequireSystemCaller(OperatorComponent, c.logger))
}

// Boot implements api.Controller.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /internal/declared/{kind}", c.handleApply)
	mux.Handle("DELETE /internal/declared/{kind}/{tenant}/{name}", c.handleDelete)
	mux.Handle("GET /internal/declared/{kind}/{tenant}", c.handleList)
}

// applier returns the Applier for the request's kind, or writes a 404.
func (c *Controller) applier(w http.ResponseWriter, r *http.Request) (Applier, bool) {
	a, ok := c.appliers[r.PathValue("kind")]
	if !ok {
		api.ErrorResponse(r.Context(), w, c.logger, api.NotFoundError(api.ErrCodeResourceNotFound, "this service owns no such kind", nil), nil)
	}
	return a, ok
}

func (c *Controller) handleApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := c.applier(w, r)
	if !ok {
		return
	}
	var item Item
	if !api.DecodeJSON(ctx, r, w, c.logger, &item) {
		return
	}
	if item.TenantID == "" || item.Namespace == "" || item.Name == "" || item.UID == "" || len(item.Spec) == 0 || string(item.Spec) == "null" {
		api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeMissingField, "tenant_id, namespace, name, uid and spec are required", nil), nil)
		return
	}
	res, err := a.Apply(ctx, item)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "apply declared item", nil), err)
		return
	}
	c.recordFailure(r, item, res)
	api.JSON(ctx, w, c.logger, http.StatusOK, res)
}

// failedEntity is the declared resource a failure event carries. The outcome
// is the bounded reason. The free-text message stays on the resource status.
type failedEntity struct {
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Generation int64  `json:"generation"`
	Outcome    string `json:"outcome"`
}

// recordFailure records an apply that was not projected, once per resource
// generation and outcome. Platform-wide kinds record nothing.
func (c *Controller) recordFailure(r *http.Request, item Item, res Result) {
	if c.events == nil || res.Outcome == OutcomeProjected || item.TenantID == PlatformTenant {
		return
	}
	ctx := r.Context()
	kind := r.PathValue("kind")
	key := fmt.Sprintf("%s/%d/%s", item.UID, item.Generation, res.Outcome)
	err := events.PublishOnce(ctx, c.events, key, events.Event{
		Type: events.DeclaredResourceFailed, Tenant: item.TenantID, Subject: "declared/" + kind + "/" + item.Name,
		Entity: failedEntity{
			Kind: kind, Namespace: item.Namespace, Name: item.Name, UID: item.UID,
			Generation: item.Generation, Outcome: res.Outcome,
		},
	})
	if err != nil {
		c.logger.Error(ctx, "failed to record declared resource failure",
			logging.Field{Key: "kind", Value: kind},
			logging.Field{Key: "name", Value: item.Name},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

func (c *Controller) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := c.applier(w, r)
	if !ok {
		return
	}
	err := a.Delete(ctx, r.PathValue("tenant"), r.PathValue("name"))
	switch {
	case errors.Is(err, ErrInUse):
		api.ErrorResponse(ctx, w, c.logger, api.ConflictError(api.ErrCodeConflict, ErrInUse.Error(), nil), err)
		return
	case err != nil:
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "delete declared item", nil), err)
		return
	}
	api.NoBody(w, http.StatusNoContent)
}

// listResponse is the body of the list endpoint.
type listResponse struct {
	Items []Declared `json:"items"`
}

func (c *Controller) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := c.applier(w, r)
	if !ok {
		return
	}
	items, err := a.List(ctx, r.PathValue("tenant"))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "list declared items", nil), err)
		return
	}
	if items == nil {
		items = []Declared{}
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, listResponse{Items: items})
}
