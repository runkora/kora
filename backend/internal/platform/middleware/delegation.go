package middleware

import (
	"net/http"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

const delegationTokenHeader = "X-Orbit-Delegation-Token"

// DelegationMiddleware extracts the optional X-Orbit-Delegation-Token header,
// verifies the delegation JWT (signature, issuer, audience, expiry via the
// same JWKS as platform tokens), and stores the result in context via
// policy.WithDelegation. Downstream handlers retrieve it with
// policy.DelegationFromContext.
//
// Missing header is not an error — most requests are non-delegated. An invalid
// or unverifiable token is rejected with 401. Must run after AuthMiddleware so
// that the delegation token is only accepted alongside a valid platform token.
func DelegationMiddleware(logger logging.Logger, verifier *token.Verifier) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(delegationTokenHeader)
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}

			d, err := policy.ParseDelegationToken(r.Context(), raw, verifier)
			if err != nil {
				logger.Error(r.Context(), "invalid delegation token",
					logging.Field{Key: "error", Value: err.Error()},
				)
				writeUnauthorized(w, "invalid delegation token")
				return
			}

			next.ServeHTTP(w, r.WithContext(policy.WithDelegation(r.Context(), d)))
		})
	})
}
