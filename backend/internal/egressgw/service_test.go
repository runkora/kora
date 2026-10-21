package egressgw

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// heldScopes allows exactly the listed "<path>:<action>" scopes.
type heldScopes []string

func (h heldScopes) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	scope := strings.Join(in.PathSegments, ":") + ":" + in.Action
	return policy.Result{Allow: slices.Contains(h, scope)}, nil
}

// Scopes reports each held scope in its full connection:<path>:<action> form.
func (h heldScopes) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	out := make([]string, len(h))
	for i, s := range h {
		out[i] = resourceType + ":" + s
	}
	return out, nil
}

// failingOn errors for one scope and allows every other.
type failingOn string

func (f failingOn) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if strings.Join(in.PathSegments, ":")+":"+in.Action == string(f) {
		return policy.Result{}, errors.New("evaluator down")
	}
	return policy.Result{Allow: true}, nil
}

func (f failingOn) Scopes(context.Context, policy.ScopesInput) ([]string, error) { return nil, nil }

func TestEvaluatePolicy_AlsoRequiredEvaluatorErrorFailsClosed(t *testing.T) {
	cls := credentials.Classification{Action: "pulls:read", AlsoRequired: []string{"contents:read"}, Qualifier: "orbit"}
	b := NewBroker(failingOn("github:contents:orbit:read"), nil, nil, nil, logging.NewLoggerWithOutput(io.Discard), nil)
	got, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
	}
}

func TestEvaluatePolicy_AlsoRequired(t *testing.T) {
	conn := &credentials.Connection{Name: "github"}
	cls := credentials.Classification{
		Action: "issues:read", Alternates: []string{"pulls:read"},
		AlsoRequired: []string{"contents:read"}, Qualifier: "orbit",
	}
	cases := []struct {
		name string
		held heldScopes
		want []string
	}{
		{"all held", heldScopes{"github:issues:orbit:read", "github:contents:orbit:read"}, []string{"issues:read", "contents:read"}},
		{"alternate and required held", heldScopes{"github:pulls:orbit:read", "github:contents:orbit:read"}, []string{"pulls:read", "contents:read"}},
		{"required missing", heldScopes{"github:issues:orbit:read", "github:pulls:orbit:read"}, nil},
		{"only required held", heldScopes{"github:contents:orbit:read"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker(tc.held, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard), nil)
			got, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, conn, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
			if tc.want == nil {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluatePolicy: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("authorized = %v, want %v", got, tc.want)
			}
		})
	}
}

// A missing required action is named in the denial, with the scope that
// would allow it.
func TestEvaluatePolicy_AlsoRequiredDenialNamesAction(t *testing.T) {
	cls := credentials.Classification{Action: "pulls:read", AlsoRequired: []string{"contents:read"}, Qualifier: "orbit"}
	b := NewBroker(heldScopes{"github:pulls:orbit:read"}, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard), nil)
	_, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "POST"})
	var d *Denial
	if !errors.As(err, &d) {
		t.Fatalf("want a Denial, got %v", err)
	}
	if d.Action != "contents:read" || d.Reason != DenialNotGranted || d.RequiredScope == "" {
		t.Errorf("denial = %+v, want contents:read not granted with a required scope", d)
	}
}

// delegationRecorder allows every check and records the delegation each one carried.
type delegationRecorder struct{ seen []*policy.DelegationClaims }

func (d *delegationRecorder) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	d.seen = append(d.seen, in.Delegation)
	return policy.Result{Allow: true}, nil
}

func (d *delegationRecorder) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

// Connection checks and the egress posture check carry the run's verified
// delegation. Policy decides which of them intersect with the delegator.
func TestBrokerChecks_CarryDelegation(t *testing.T) {
	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", RunID: "run-1"}
	cls := credentials.Classification{Action: "issues:read", Alternates: []string{"pulls:read"}, AlsoRequired: []string{"contents:read"}, Qualifier: "orbit"}
	cases := []struct {
		name       string
		delegation *policy.DelegationClaims
	}{
		{"delegated", delegation},
		{"autonomous", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.delegation != nil {
				ctx = policy.WithDelegation(ctx, tc.delegation)
			}
			rec := &delegationRecorder{}
			b := NewBroker(rec, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard), nil)
			if _, err := b.evaluatePolicy(ctx, &token.PlatformClaims{}, &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "GET"}); err != nil {
				t.Fatalf("evaluatePolicy: %v", err)
			}
			b.ownEgressAllows(ctx, &token.PlatformClaims{}, "api.github.com")
			if len(rec.seen) != 4 {
				t.Fatalf("checks = %d, want 4 (primary, alternate, required, posture)", len(rec.seen))
			}
			for i, got := range rec.seen {
				if got != tc.delegation {
					t.Errorf("check %d delegation = %+v, want %+v", i, got, tc.delegation)
				}
			}
		})
	}
}

// A delegation bound to one run is refused for a caller attesting another run,
// and a delegated caller with no attested run is refused before any lookup.
// The evaluator is the real engine with no bundle. The binding denial needs none.
func TestBrokerAuthorize_DelegationRunBinding(t *testing.T) {
	const agent = "spiffe://orbit.test/tenant/acme/agent/coder"
	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", ActSub: agent, RunID: "run-a"}
	claimsFor := func(runID string) *token.PlatformClaims {
		c := &token.PlatformClaims{}
		c.Subject = agent
		c.Kora.TenantID = "acme"
		c.Kora.PrincipalType = token.PrincipalTypeAgent
		if runID != "" {
			c.Kora.Claims = map[string]string{"run_id": runID}
		}
		return c
	}
	logger := logging.NewLoggerWithOutput(io.Discard)
	engine := policy.NewEvaluator("http://127.0.0.1:1/bundles", time.Hour, logger)
	ctx := policy.WithDelegation(context.Background(), delegation)

	t.Run("connection check for another run", func(t *testing.T) {
		b := NewBroker(engine, nil, nil, nil, logger, nil)
		cls := credentials.Classification{Action: "issues:read", Qualifier: "orbit"}
		_, err := b.evaluatePolicy(ctx, claimsFor("run-b"), &credentials.Connection{Name: "github"}, cls, EgressRequest{Host: "api.github.com", Method: "GET"})
		var d *Denial
		if !errors.As(err, &d) || d.decision != policy.ReasonDelegationRunMismatch {
			t.Fatalf("want %s denial, got %v", policy.ReasonDelegationRunMismatch, err)
		}
	})
	t.Run("posture check for another run", func(t *testing.T) {
		b := NewBroker(engine, nil, nil, nil, logger, nil)
		if b.ownEgressAllows(ctx, claimsFor("run-b"), "github.com") {
			t.Fatal("posture check allowed a delegation bound to another run")
		}
	})
	t.Run("no attested run", func(t *testing.T) {
		b := NewBroker(engine, nil, nil, nil, logger, nil)
		_, err := b.Authorize(ctx, claimsFor(""), EgressRequest{Host: "api.github.com", Method: "GET"})
		if !errors.Is(err, ErrDelegationRunUnattested) || !errors.Is(err, ErrForbidden) {
			t.Fatalf("want ErrDelegationRunUnattested, got %v", err)
		}
	})
}
