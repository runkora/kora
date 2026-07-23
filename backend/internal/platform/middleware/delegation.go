package middleware

import (
	"context"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

const delegationTokenHeader = "X-Orbit-Delegation-Token"

// JTIBlocklist reports whether a token's JTI has been revoked. Implemented by
// internal/platform/storage.JTIBlocklist (the Redis set the identity service
// writes revocations to).
type JTIBlocklist interface {
	IsBlocked(ctx context.Context, jti string) (bool, error)
}

// DelegationMiddleware extracts the optional X-Orbit-Delegation-Token header,
// verifies the delegation JWT (signature, issuer, audience, expiry via the
// same JWKS as platform tokens), and stores the result in context via
// policy.WithDelegation. Downstream handlers retrieve it with
// policy.DelegationFromContext.
//
// Missing header is not an error — most requests are non-delegated. An invalid
// or unverifiable token is rejected with 401. Must run after AuthMiddleware so
// that the delegation token is only accepted alongside a valid platform token.
//
// Services with a Redis client should prefer DelegationMiddlewareWithBlocklist
// so revoked delegations (docs/adr/2026-07-23-delegated-agent-identity.md) are
// rejected before natural token expiry.
func DelegationMiddleware(logger logging.Logger, verifier *token.Verifier) Middleware {
	return DelegationMiddlewareWithBlocklist(logger, verifier, nil)
}

// DelegationMiddlewareWithBlocklist is DelegationMiddleware plus a revocation
// check: after cryptographic verification, the delegation token's JTI is
// checked against the shared blocklist and rejected with 401 when revoked.
// The JTI is stable across re-signed delegation tokens, so one blocklist entry
// kills every token minted from a revoked issuance.
//
// A nil blocklist skips the check (fail-closed enforcement still holds at the
// policy layer via the delegation_required grant marker). On blocklist lookup
// failure the request is allowed with an error log — the same fail-open
// posture the identity service applies to platform-token blocklist checks, so
// a Redis outage cannot take down all delegated traffic.
func DelegationMiddlewareWithBlocklist(logger logging.Logger, verifier *token.Verifier, blocklist JTIBlocklist) Middleware {
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

			if blocklist != nil && d != nil && d.JTI != "" {
				if blocked, blErr := blocklist.IsBlocked(r.Context(), d.JTI); blErr != nil {
					logger.Error(r.Context(), "delegation JTI blocklist check failed — allowing request",
						logging.Field{Key: "jti", Value: d.JTI},
						logging.Field{Key: "error", Value: blErr.Error()},
					)
				} else if blocked {
					logger.Warn(r.Context(), "revoked delegation token rejected",
						logging.Field{Key: "jti", Value: d.JTI},
						logging.Field{Key: "run_id", Value: d.RunID},
					)
					writeUnauthorized(w, "delegation has been revoked")
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(policy.WithDelegation(r.Context(), d)))
		})
	})
}
