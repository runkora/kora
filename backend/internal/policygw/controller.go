package policygw

import (
	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/token"
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

// Boot registers the check and query routes.
//
// Route map:
//
//	POST /policy/check    — binary allow/deny for a principal and resource
//	POST /policy/query    — filter a candidate resource list to allowed entries
func (c *Controller) Boot(mux *platformapi.Mux) {
	mux.Handle("POST /check", c.handleCheck)
	mux.Handle("POST /query", c.handleQuery)
}

// evaluationErrorResponse maps policy evaluation errors to API error responses.
// Shared by both check and query handlers.
func evaluationErrorResponse(_ error) *platformapi.APIError {
	return platformapi.ServiceUnavailableError(platformapi.ErrCodeServiceUnavailable, "policy evaluation unavailable", nil)
}
