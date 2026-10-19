package policygw

import (
	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Controller wires the policy gateway's check and query API to HTTP routes.
// Used by owned services for Layer 2 in-application enforcement.
type Controller struct {
	svc      *Service
	verifier *token.Verifier
	logger   logging.Logger
}

// NewController creates a ready-to-boot policy gateway Controller.
func NewController(svc *Service, verifier *token.Verifier, logger logging.Logger) *Controller {
	return &Controller{
		svc:      svc,
		verifier: verifier,
		logger:   logger,
	}
}

func (c *Controller) GroupedBy() string                { return "/policy" }
func (c *Controller) Middleware() mw.MiddlewareHandler { return mw.MiddlewareChain() }

// Boot registers the check, query, scopes, explain, and simulate routes.
//
// Route map:
//
//	POST /policy/check             — binary allow/deny for a principal and resource
//	POST /policy/query             — filter a candidate resource list to allowed entries
//	GET  /policy/scopes            — return all scopes the authenticated principal holds
//	GET  /policy/scopes/explain    — return scopes broken down by source (grants vs roles)
//	POST /policy/check/simulate    — dry-run policy check by principal identity (no JWT required)
//
// The two routes that can name a principal other than the caller require
// platform:grants:read for that case; see authorizeInspectOthers.
func (c *Controller) Boot(mux *platformapi.Mux) {
	mux.Handle("POST /check", c.handleCheck)
	mux.Handle("POST /query", c.handleQuery)

	authChain := mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))

	// The scopes endpoints use JWT auth middleware — the principal is extracted
	// from the Authorization header, not from a request body token field.
	mux.HandleWithMiddleware("GET /scopes", authChain, c.handleScopes)
	mux.HandleWithMiddleware("GET /scopes/explain", authChain, c.handleScopesExplain)
	mux.HandleWithMiddleware("POST /check/simulate", authChain, c.handleCheckSimulate)
}

// evaluationErrorResponse maps policy evaluation errors to API error responses.
// Shared by both check and query handlers.
func evaluationErrorResponse(_ error) *platformapi.APIError {
	return platformapi.ServiceUnavailableError(platformapi.ErrCodeServiceUnavailable, "policy evaluation unavailable", nil)
}
