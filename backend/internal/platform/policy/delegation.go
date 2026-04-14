package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/platform/token"
)

// delegationTokenPayload is the wire format of the delegation token payload.
// The token is issued by the identity service with the same signing key,
// issuer, and audience as platform tokens.
type delegationTokenPayload struct {
	Sub   string `json:"sub"`
	Scope string `json:"scope"`
	Act   struct {
		Sub string `json:"sub"`
	} `json:"act"`
}

// ParseDelegationToken verifies and parses a delegation token.
// The delegation token is issued by the Orbit identity service alongside the
// platform token. Its structure: sub = delegator (human), act.sub = agent,
// scope = space-separated scopes granted for this session (RFC 6749/8693).
//
// The token is cryptographically verified using the same JWKS, issuer, and
// audience as platform tokens. Signature, expiry, issuer, and audience are
// all checked before claims are extracted.
//
// Returns nil, nil if rawToken is empty.
func ParseDelegationToken(ctx context.Context, rawToken string, verifier *token.Verifier) (*DelegationClaims, error) {
	if rawToken == "" {
		return nil, nil
	}

	var payload delegationTokenPayload
	if err := verifier.VerifyAndExtract(ctx, rawToken, &payload); err != nil {
		return nil, fmt.Errorf("verify delegation token: %w", err)
	}

	var scopes []string
	for s := range strings.FieldsSeq(payload.Scope) {
		if s != "" {
			scopes = append(scopes, s)
		}
	}

	return &DelegationClaims{
		Sub:    payload.Sub,
		ActSub: payload.Act.Sub,
		Scopes: scopes,
	}, nil
}
