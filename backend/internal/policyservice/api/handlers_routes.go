package api

import (
	"net/http"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/policyservice/routes"
)

// registerRoutesRequest is the body for POST /internal/routes/register.
type registerRoutesRequest struct {
	Service  string              `json:"service"`
	TenantID string              `json:"tenant_id"`
	Routes   []routes.RouteScope `json:"routes"`
}

// InternalRegisterHandler returns an http.HandlerFunc for the internal route
// registration endpoint. It is mounted on the internal-only port in main.go
// and is not reachable from outside the cluster.
func InternalRegisterHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		var req registerRoutesRequest
		if !platformapi.DecodeJSON(ctx, r, w, svc.logger, &req) {
			return
		}

		if req.Service == "" || len(req.Routes) == 0 {
			platformapi.ErrorResponse(ctx, w, svc.logger,
				platformapi.ValidationError(platformapi.ErrCodeMissingField,
					"service and routes are required", nil), nil)
			return
		}

		if err := svc.RegisterRoutes(ctx, req.Service, req.TenantID, req.Routes); err != nil {
			platformapi.ErrorResponse(ctx, w, svc.logger, routesErrorResponse(err), err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func routesErrorResponse(err error) *platformapi.APIError {
	var scopeErr *ScopeVocabularyError
	if isScopeVocabularyError(err, &scopeErr) {
		return platformapi.ValidationError(platformapi.ErrCodeInvalidField,
			"scope '"+scopeErr.Scope+"' is not in the scope vocabulary",
			map[string]any{"scope": scopeErr.Scope})
	}
	return platformapi.InternalError(platformapi.ErrCodeDatabaseError, "route registration failed", nil)
}
