package middleware

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// IsSystemCaller reports whether the verified caller is the SYSTEM component
// named by its SPIFFE ID /system/<component>. The type alone is not enough:
// every in-mesh component holds a SYSTEM token, and scope enforcement does not
// restrict SYSTEM.
func IsSystemCaller(ctx context.Context, component string) bool {
	claims, ok := token.ClaimsFromContext(ctx)
	return ok && claims != nil && claims.Kora.PrincipalType == token.PrincipalTypeSystem && claims.Kora.DisplayName == component
}

// RequireSystemCaller refuses with 403 every caller but the named SYSTEM components.
// It runs after AuthMiddleware, which puts the claims on the context.
func RequireSystemCaller(logger logging.Logger, components ...string) Middleware {
	expected := strings.Join(components, " or ")
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !slices.ContainsFunc(components, func(c string) bool { return IsSystemCaller(ctx, c) }) {
				principalType, displayName := "", ""
				if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil {
					principalType, displayName = string(claims.Kora.PrincipalType), claims.Kora.DisplayName
				}
				logger.Warn(ctx, "rejected a call from an unexpected principal",
					logging.Field{Key: "path", Value: r.URL.Path},
					logging.Field{Key: "expected", Value: expected},
					logging.Field{Key: "principal_type", Value: principalType},
					logging.Field{Key: "display_name", Value: displayName},
				)
				writeError(w, http.StatusForbidden, "forbidden", "only "+expected+" may call this endpoint")
				return
			}
			next.ServeHTTP(w, r)
		})
	})
}
