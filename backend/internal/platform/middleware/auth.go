package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
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
//
// It is AuthMiddlewareWithRevocation without a blocklist: a token revoked ahead
// of its expiry stays usable here until it expires.
func AuthMiddleware(verifier *token.Verifier, logger logging.Logger) Middleware {
	return AuthMiddlewareWithRevocation(verifier, nil, logger)
}

// writeError writes a minimal JSON error. platform/api imports this package,
// so its response helpers are out of reach.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"code":%q,"message":%q}`, code, message)
}

// RevocationChecker reports whether a token's JTI has been revoked ahead of its
// natural expiry. Satisfied by *storage.JTIBlocklist.
//
// It is an interface rather than a concrete type so that platform/middleware
// does not import platform/storage, which would pull a database dependency into
// every service that only needs authentication.
type RevocationChecker interface {
	IsBlocked(ctx context.Context, jti string) (bool, error)
}

// RevokedSince reports whether the token or delegation in ctx was revoked after the request
// was authenticated. An open stream calls it on each heartbeat. A nil checker or a blocklist
// error reports false, failing open as AuthMiddlewareWithRevocation does.
func RevokedSince(ctx context.Context, revoked RevocationChecker) bool {
	if revoked == nil {
		return false
	}
	var jtis []string
	if claims, ok := token.ClaimsFromContext(ctx); ok && claims != nil && claims.ID != "" {
		jtis = append(jtis, claims.ID)
	}
	if d := policy.DelegationFromContext(ctx); d != nil && d.JTI != "" {
		jtis = append(jtis, d.JTI)
	}
	for _, jti := range jtis {
		if blocked, err := revoked.IsBlocked(ctx, jti); err == nil && blocked {
			return true
		}
	}
	return false
}

// AuthMiddlewareWithRevocation is AuthMiddleware plus an immediate revocation
// check against the JTI blocklist, so a revoked token is rejected at once
// rather than remaining usable until it expires.
//
// Services that hold tenant-facing state and can act on a caller's behalf should
// prefer this over AuthMiddleware. Services whose only exposure is a read path
// may use the plain variant.
//
// On a blocklist failure this fails OPEN: the request proceeds and the error is
// logged. A Redis outage must not take authentication down platform-wide, and
// the token has already passed signature and expiry verification. This is the
// same trade-off the identity service makes.
func AuthMiddlewareWithRevocation(verifier *token.Verifier, revoked RevocationChecker, logger logging.Logger) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			raw := r.Header.Get("Authorization")
			if raw == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authorization header required")
				return
			}

			rawToken := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))

			claims, err := verifier.Verify(ctx, rawToken)
			if err != nil {
				logger.Error(ctx, "platform token verification failed",
					logging.Field{Key: "error", Value: err.Error()},
				)
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired platform token")
				return
			}

			if revoked != nil && claims.ID != "" {
				blocked, blErr := revoked.IsBlocked(ctx, claims.ID)
				switch {
				case blErr != nil:
					logger.Error(ctx, "JTI blocklist check failed — allowing request",
						logging.Field{Key: "jti", Value: claims.ID},
						logging.Field{Key: "error", Value: blErr.Error()},
					)
				case blocked:
					logger.Warn(ctx, "rejected revoked token",
						logging.Field{Key: "jti", Value: claims.ID})
					writeError(w, http.StatusUnauthorized, "unauthorized", "token has been revoked")
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(token.WithClaims(ctx, claims)))
		})
	})
}
