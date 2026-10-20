package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// DelegationTokenHeader carries a run's delegation token alongside the platform token.
const DelegationTokenHeader = "X-Orbit-Delegation-Token"

// JTIBlocklist reports whether a token's JTI has been revoked. Implemented by
// internal/platform/storage.JTIBlocklist (the Redis set the identity service
// writes revocations to).
type JTIBlocklist interface {
	IsBlocked(ctx context.Context, jti string) (bool, error)
}

// Refusals returned by VerifyDelegation.
var (
	ErrDelegationInvalid = errors.New("invalid delegation token")
	ErrDelegationRevoked = errors.New("delegation has been revoked")
)

// VerifyDelegation verifies a delegation token and refuses one whose JTI is on the blocklist.
// An empty token yields nil claims. A blocklist lookup error is logged and the token accepted.
func VerifyDelegation(ctx context.Context, logger logging.Logger, verifier *token.Verifier, blocklist JTIBlocklist, raw string) (*policy.DelegationClaims, error) {
	d, err := policy.ParseDelegationToken(ctx, raw, verifier)
	if err != nil {
		logger.Error(ctx, "invalid delegation token", logging.Field{Key: "error", Value: err.Error()})
		return nil, fmt.Errorf("%w: %w", ErrDelegationInvalid, err)
	}
	if blocklist == nil || d == nil || d.JTI == "" {
		return d, nil
	}
	blocked, err := blocklist.IsBlocked(ctx, d.JTI)
	if err != nil {
		logger.Error(ctx, "delegation JTI blocklist check failed — allowing request",
			logging.Field{Key: "jti", Value: d.JTI},
			logging.Field{Key: "error", Value: err.Error()})
		return d, nil
	}
	if blocked {
		logger.Warn(ctx, "revoked delegation token rejected",
			logging.Field{Key: "jti", Value: d.JTI},
			logging.Field{Key: "run_id", Value: d.RunID})
		return nil, ErrDelegationRevoked
	}
	return d, nil
}

// DelegationRefusalMessage is the client-facing text for a VerifyDelegation error.
func DelegationRefusalMessage(err error) string {
	if errors.Is(err, ErrDelegationRevoked) {
		return ErrDelegationRevoked.Error()
	}
	return ErrDelegationInvalid.Error()
}

// DelegationMiddlewareWithBlocklist extracts the optional X-Orbit-Delegation-Token
// header, verifies the delegation JWT (signature, issuer, audience, expiry) with
// the given verifier, and stores the result in context via policy.WithDelegation.
// Downstream handlers retrieve it with policy.DelegationFromContext.
//
// Missing header is not an error — most requests are non-delegated. An invalid
// or unverifiable token is rejected with 401. Must run after AuthMiddleware so
// that the delegation token is only accepted alongside a valid platform token.
//
// After verification the token's JTI is checked against the shared blocklist
// and rejected with 401 when revoked
// (docs/adr/2026-07-23-delegated-agent-identity.md). The JTI is stable across
// re-signed delegation tokens, so one blocklist entry kills every token minted
// from a revoked issuance.
//
// A nil blocklist skips the check (fail-closed enforcement still holds at the
// policy layer via the delegation_required grant marker). On blocklist lookup
// failure the request is allowed with an error log — the same fail-open
// posture the identity service applies to platform-token blocklist checks, so
// a Redis outage cannot take down all delegated traffic.
func DelegationMiddlewareWithBlocklist(logger logging.Logger, verifier *token.Verifier, blocklist JTIBlocklist) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get(DelegationTokenHeader)
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}

			d, err := VerifyDelegation(r.Context(), logger, verifier, blocklist, raw)
			if err != nil {
				writeUnauthorized(w, DelegationRefusalMessage(err))
				return
			}

			next.ServeHTTP(w, r.WithContext(policy.WithDelegation(r.Context(), d)))
		})
	})
}
