package inferencegw

import (
	"net/http"

	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// xAPIKeyMiddleware promotes the Anthropic SDK's x-api-key header to
// Authorization: Bearer before AuthMiddleware runs.
//
// Claude Code and other Anthropic-SDK clients send their platform token in the
// x-api-key header. This middleware normalises it to the standard Authorization
// header so AuthMiddleware can verify it without any special-casing.
//
// It is registered at the controller level, before AuthMiddleware in the chain,
// so it runs first on every request. For OpenAI-dialect callers (and any request
// that already carries an Authorization header) it is a trivial no-op.
//
// Execution order:
//
//	xAPIKeyMiddleware → AuthMiddleware → handler
func xAPIKeyMiddleware() mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				if key := r.Header.Get("x-api-key"); key != "" {
					r = r.Clone(r.Context())
					r.Header.Set("Authorization", "Bearer "+key)
				}
			}
			next.ServeHTTP(w, r)
		})
	})
}
