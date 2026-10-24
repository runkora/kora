package declarative

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
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
}

// NewController returns a Controller serving one Applier per kind, keyed by
// the Kind constants.
func NewController(verifier *token.Verifier, logger logging.Logger, appliers map[string]Applier) *Controller {
	return &Controller{appliers: appliers, auth: mw.AuthMiddleware(verifier, logger), logger: logger}
}

// GroupedBy implements api.Controller.
func (c *Controller) GroupedBy() string { return "" }

// Middleware verifies the platform token and admits only the operator.
func (c *Controller) Middleware() mw.MiddlewareHandler {
	return mw.MiddlewareChain(c.auth, mw.NewMiddleware(c.operatorOnly))
}

// Boot implements api.Controller.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /internal/declared/{kind}", c.handleApply)
	mux.Handle("DELETE /internal/declared/{kind}/{tenant}/{name}", c.handleDelete)
	mux.Handle("GET /internal/declared/{kind}/{tenant}", c.handleList)
}

// operatorOnly refuses every principal but the operator. Any in-mesh
// component can hold a SYSTEM token. The type alone is not enough.
func (c *Controller) operatorOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		claims, ok := token.ClaimsFromContext(ctx)
		if !ok || claims.Kora.PrincipalType != token.PrincipalTypeSystem || claims.Kora.DisplayName != OperatorComponent {
			c.logger.Warn(ctx, "rejected declared-item call from a principal other than the operator",
				logging.Field{Key: "path", Value: r.URL.Path})
			api.ErrorResponse(ctx, w, c.logger, api.ForbiddenError(api.ErrCodeForbidden, "only the operator may call declared-item endpoints", nil), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	if item.TenantID == "" || item.Name == "" || item.UID == "" || len(item.Spec) == 0 {
		api.ErrorResponse(ctx, w, c.logger, api.ValidationError(api.ErrCodeMissingField, "tenant_id, name, uid and spec are required", nil), nil)
		return
	}
	res, err := a.Apply(ctx, item)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger, api.InternalError(api.ErrCodeInternalError, "apply declared item", nil), err)
		return
	}
	api.JSON(ctx, w, c.logger, http.StatusOK, res)
}

func (c *Controller) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, ok := c.applier(w, r)
	if !ok {
		return
	}
	if err := a.Delete(ctx, r.PathValue("tenant"), r.PathValue("name")); err != nil {
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
