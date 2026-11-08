package api

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// RejectEncodedPaths wraps srv.Handler so that any request whose encoded path
// differs from its decoded form is refused with a 400 before any routing.
//
// Istio does not decode escapes such as %64, but Go's ServeMux does, so an
// encoded path like /v1/internal/%64eclared/... would miss mesh path rules
// and still reach the controller. Rejecting every escaped path closes that gap.
//
// It wraps the handler outside the outer mux api.Server builds, so unwrapped
// routes such as GET /health are covered too. It returns srv.
func RejectEncodedPaths(logger logging.Logger, srv *http.Server) *http.Server {
	next := srv.Handler
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if escaped := r.URL.EscapedPath(); escaped != r.URL.Path {
			logger.Warn(r.Context(), "Rejected percent-encoded request path",
				logging.Field{Key: "path", Value: escaped},
			)
			ErrorResponse(r.Context(), w, logger,
				BadRequestError("encoded_path", "percent-encoded request paths are not accepted", nil), nil)
			return
		}
		next.ServeHTTP(w, r)
	})
	return srv
}
