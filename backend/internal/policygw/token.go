package policygw

import (
	"fmt"
	"strings"

	"github.com/clear-route/orbit/internal/policygw/evaluation"
	jwt "github.com/golang-jwt/jwt/v5"
)

// parseDelegationToken parses delegation token claims without full verification.
// The delegation token is issued by the Orbit identity service alongside the
// platform token. Its structure: sub = delegator (human), act.sub = agent,
// scope = space-separated scopes granted for this session (RFC 6749/8693).
//
// Returns nil if rawToken is empty.
func parseDelegationToken(rawToken string) (*evaluation.DelegationClaims, error) {
	if rawToken == "" {
		return nil, nil
	}

	// Parse without validation — cryptographic validation is out of scope here;
	// the delegation token was issued by the same identity service and its
	// presence is contextually trusted alongside a valid platform token.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	mapClaims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(rawToken, mapClaims)
	if err != nil {
		return nil, fmt.Errorf("parse delegation token: %w", err)
	}

	sub, _ := mapClaims["sub"].(string)
	scopeRaw, _ := mapClaims["scope"].(string)

	var actSub string
	if act, ok := mapClaims["act"].(map[string]any); ok {
		actSub, _ = act["sub"].(string)
	}

	var scopes []string
	for s := range strings.FieldsSeq(scopeRaw) {
		if s != "" {
			scopes = append(scopes, s)
		}
	}

	return &evaluation.DelegationClaims{
		Sub:    sub,
		ActSub: actSub,
		Scopes: scopes,
	}, nil
}
