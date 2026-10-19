package middleware

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token/tokentest"
)

// failingBlocklist stands in for Redis being down.
type failingBlocklist struct{}

func (failingBlocklist) IsBlocked(context.Context, string) (bool, error) {
	return false, errors.New("redis down")
}

func TestVerifyDelegation(t *testing.T) {
	const agent = "spiffe://orbit.test/tenant/acme/agent/coder"
	s := tokentest.NewSigner(t)
	valid := s.Delegation(t, "jti-1", "person-1", agent, "run-1")
	revoked := s.Delegation(t, "jti-revoked", "person-1", agent, "run-1")
	blocklist := tokentest.Blocklist{"jti-revoked": true}

	cases := []struct {
		name      string
		raw       string
		blocklist JTIBlocklist
		wantErr   error
		wantSub   string
	}{
		{"no token", "", blocklist, nil, ""},
		{"valid", valid, blocklist, nil, "person-1"},
		{"malformed", "not-a-jwt", blocklist, ErrDelegationInvalid, ""},
		{"revoked", revoked, blocklist, ErrDelegationRevoked, ""},
		{"blocklist error fails open", revoked, failingBlocklist{}, nil, "person-1"},
		{"no blocklist", revoked, nil, nil, "person-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := VerifyDelegation(context.Background(), logging.NewLoggerWithOutput(io.Discard), s.Verifier, tc.blocklist, tc.raw)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			var sub string
			if d != nil {
				sub = d.Sub
			}
			if sub != tc.wantSub {
				t.Errorf("delegator = %q, want %q", sub, tc.wantSub)
			}
		})
	}
}
