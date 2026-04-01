package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// AuthMiddleware returns a Middleware that enforces platform JWT authentication
// on every request. On success the verified *token.PlatformClaims are stored
// in the request context via token.WithClaims — downstream handlers retrieve
// them with token.ClaimsFromContext.
//
// This middleware accepts only the standard Authorization: Bearer <token> header.
// For routes that must also accept x-api-key (e.g. the Anthropic /messages path),
// chain XAPIKeyMiddleware before this one — it promotes x-api-key to
// Authorization: Bearer before this middleware runs.
//
// This follows the same pattern as LoggingMiddleware — call it in the
// controller's Middleware() method:
//
//	func (c *Controller) Middleware() mw.MiddlewareHandler {
//	    return mw.MiddlewareChain(mw.AuthMiddleware(c.verifier, c.logger))
//	}
func AuthMiddleware(verifier *token.Verifier, logger logging.Logger) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			raw := r.Header.Get("Authorization")
			if raw == "" {
				writeUnauthorized(w, "Authorization header required")
				return
			}

			rawToken := strings.TrimPrefix(raw, "Bearer ")
			rawToken = strings.TrimSpace(rawToken)

			claims, err := verifier.Verify(ctx, rawToken)
			if err != nil {
				logger.Error(ctx, "platform token verification failed",
					logging.Field{Key: "error", Value: err.Error()},
				)
				writeUnauthorized(w, "invalid or expired platform token")
				return
			}

			next.ServeHTTP(w, r.WithContext(token.WithClaims(ctx, claims)))
		})
	})
}

// writeUnauthorized writes a minimal JSON 401 response without importing
// platform/api (which imports platform/middleware — avoiding an import cycle).
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `{"code":"unauthorized","message":%q}`, message)
}
