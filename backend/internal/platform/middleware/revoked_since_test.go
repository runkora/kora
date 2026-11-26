package middleware

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// revokedJTIs reports the listed JTIs as revoked, or fails every check with err.
type revokedJTIs struct {
	jtis []string
	err  error
}

func (r revokedJTIs) IsBlocked(_ context.Context, jti string) (bool, error) {
	if slices.Contains(r.jtis, jti) {
		return true, r.err
	}
	return false, r.err
}

func TestRevokedSince(t *testing.T) {
	claims := &token.PlatformClaims{}
	claims.ID = "tok-1"
	ctx := policy.WithDelegation(token.WithClaims(context.Background(), claims), &policy.DelegationClaims{JTI: "del-1"})
	for _, tt := range []struct {
		name    string
		checker RevocationChecker
		want    bool
	}{
		{"no checker", nil, false},
		{"nothing revoked", revokedJTIs{}, false},
		{"token revoked", revokedJTIs{jtis: []string{"tok-1"}}, true},
		{"delegation revoked", revokedJTIs{jtis: []string{"del-1"}}, true},
		{"blocklist error fails open", revokedJTIs{jtis: []string{"tok-1"}, err: errors.New("redis down")}, false},
	} {
		if got := RevokedSince(ctx, tt.checker); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	if RevokedSince(context.Background(), revokedJTIs{jtis: []string{""}}) {
		t.Error("a context with no claims reported a revocation")
	}
}
