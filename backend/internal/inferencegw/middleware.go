package inferencegw

import (
	"net/http"
	"strings"

	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// xAPIKeyMiddleware promotes the Anthropic SDK's x-api-key header to
// Authorization: Bearer before AuthMiddleware runs.
//
// Claude Code and other Anthropic-SDK clients send their platform token in the
// x-api-key header. This middleware normalises it to the standard Authorization
// header so AuthMiddleware can verify it without any special-casing.
//
// It is registered at the controller level, before orbCredentialMiddleware in
// the chain, so it runs first on every request. For OpenAI-dialect callers
// (and any request that already carries an Authorization header) it is a
// trivial no-op.
//
// Execution order:
//
//	xAPIKeyMiddleware → orbCredentialMiddleware → AuthMiddleware → handler
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

// orbCredentialMiddleware strips the "orb_" prefix from the Authorization
// header bearer value when present.
//
// PATs are returned from the API as "orb_<jwt>" for visual identification and
// secret-scanning compatibility (ADR-0033). This middleware removes the prefix
// so the raw JWT reaches AuthMiddleware unchanged, regardless of whether the
// credential arrived via x-api-key (after xAPIKeyMiddleware promotion) or
// directly in the Authorization header.
//
// This is a separate middleware from xAPIKeyMiddleware so each can be composed
// independently on any route that needs it.
//
// Execution order:
//
//	xAPIKeyMiddleware → orbCredentialMiddleware → AuthMiddleware → handler
func orbCredentialMiddleware() mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer orb_") {
				r = r.Clone(r.Context())
				r.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(auth, "Bearer orb_"))
			}
			next.ServeHTTP(w, r)
		})
	})
}
