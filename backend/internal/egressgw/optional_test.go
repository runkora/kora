package egressgw

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// delegatorBound allows a scope the agent holds, and under a delegation only
// when the delegator holds it too, as the policy bundle intersects them.
type delegatorBound struct{ agent, delegator heldScopes }

func (d delegatorBound) Check(ctx context.Context, in policy.CheckInput) (policy.Result, error) {
	scope := strings.Join(in.PathSegments, ":") + ":" + in.Action
	allow := slices.Contains(d.agent, scope) && (in.Delegation == nil || slices.Contains(d.delegator, scope))
	return policy.Result{Allow: allow}, nil
}

func (d delegatorBound) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

// coveringScopes allows a check that any listed "<path>:<action>" scope covers,
// so an unqualified scope covers its qualified forms as the policy bundle does.
type coveringScopes []string

func (c coveringScopes) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	scope := strings.Join(in.PathSegments, ":") + ":" + in.Action
	return policy.Result{Allow: slices.ContainsFunc(c, func(held string) bool { return policy.ScopeCovers(held, scope) })}, nil
}

func (c coveringScopes) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

// An Optional action widens the credential when held and never authorizes.
func TestEvaluatePolicy_Optional(t *testing.T) {
	conn := &credentials.Connection{Name: "github"}
	push := credentials.Classification{Action: "contents:write", Optional: []string{"workflows:write"}, Qualifier: "widgets"}
	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", RunID: "run-1"}
	both := heldScopes{"github:contents:widgets:write", "github:workflows:widgets:write"}
	cases := []struct {
		name       string
		pol        policyChecker
		delegation *policy.DelegationClaims
		want       []string
	}{
		{name: "held optional is minted", pol: both, want: []string{"contents:write", "workflows:write"}},
		{name: "optional not held is not minted, and the request passes", pol: heldScopes{"github:contents:widgets:write"}, want: []string{"contents:write"}},
		{name: "optional alone never authorizes", pol: heldScopes{"github:workflows:widgets:write"}},
		{name: "a delegator lacking it is not minted", pol: delegatorBound{agent: both, delegator: heldScopes{"github:contents:widgets:write"}}, delegation: delegation, want: []string{"contents:write"}},
		{name: "an optional that cannot be evaluated is not minted", pol: failingOn("github:workflows:widgets:write"), want: []string{"contents:write"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.delegation != nil {
				ctx = policy.WithDelegation(ctx, tc.delegation)
			}
			b := NewBroker(tc.pol, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
			got, err := b.evaluatePolicy(ctx, &token.PlatformClaims{}, conn, push, EgressRequest{Host: "github.com", Method: "POST"})
			if tc.want == nil {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("authorized = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}

// A GraphQL query is authorized by any repository category, or the metadata
// baseline, and the credential carries exactly the held ones. A mutation needs
// a write category.
func TestEvaluatePolicy_GraphQLHeldCategories(t *testing.T) {
	conn := &credentials.Connection{Name: "github"}
	graphql := func(verb, qualifier string) credentials.Classification {
		alts := []string{"workflows:" + verb, "issues:" + verb, "pulls:" + verb, "ci:" + verb, "deployments:" + verb, "security:" + verb, "secrets:" + verb, "admin:" + verb}
		if verb == "read" {
			alts = append(alts, "metadata:read")
		}
		return credentials.Classification{Action: "contents:" + verb, Alternates: alts, Qualifier: qualifier}
	}
	reviewer := coveringScopes{"github:metadata:read", "github:pulls:widgets:read", "github:pulls:widgets:write", "github:issues:widgets:read"}
	cases := []struct {
		name string
		held coveringScopes
		cls  credentials.Classification
		want []string
	}{
		{"a query mints the held categories and metadata", reviewer, graphql("read", "widgets"), []string{"issues:read", "pulls:read", "metadata:read"}},
		{"a mutation mints the held write categories", reviewer, graphql("write", "widgets"), []string{"pulls:write"}},
		{"another repository falls to the metadata baseline", reviewer, graphql("read", "gadgets"), []string{"metadata:read"}},
		{"a baseline-only agent gets a metadata-only query", coveringScopes{"github:metadata:read"}, graphql("read", ""), []string{"metadata:read"}},
		{"a baseline-only agent cannot mutate", coveringScopes{"github:metadata:read"}, graphql("write", "widgets"), nil},
		{"an untargeted mutation needs an unqualified write", reviewer, graphql("write", ""), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker(tc.held, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
			got, err := b.evaluatePolicy(context.Background(), &token.PlatformClaims{}, conn, tc.cls, EgressRequest{Host: "api.github.com", Method: "POST"})
			if tc.want == nil {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("authorized = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}

// A read accepts its category's write on the same repository, through the same
// policy check, so delegation still applies.
func TestEvaluatePolicy_WriteCoversRead(t *testing.T) {
	conn := &credentials.Connection{Name: "github"}
	read := func(qualifier string) credentials.Classification {
		return credentials.Classification{Action: "pulls:read", Alternates: []string{"pulls:write"}, Qualifier: qualifier}
	}
	writeOnly := heldScopes{"github:pulls:widgets:write"}
	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", RunID: "run-1"}
	cases := []struct {
		name       string
		pol        policyChecker
		qualifier  string
		delegation *policy.DelegationClaims
		want       []string
	}{
		{name: "a write-only grant reads its repository", pol: writeOnly, qualifier: "widgets", want: []string{"pulls:write"}},
		{name: "a write-only grant is denied on another repository", pol: writeOnly, qualifier: "gadgets"},
		{name: "a delegator lacking the write is denied", pol: delegatorBound{agent: writeOnly}, qualifier: "widgets", delegation: delegation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.delegation != nil {
				ctx = policy.WithDelegation(ctx, tc.delegation)
			}
			b := NewBroker(tc.pol, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
			got, err := b.evaluatePolicy(ctx, &token.PlatformClaims{}, conn, read(tc.qualifier), EgressRequest{Host: "api.github.com", Method: "GET"})
			if tc.want == nil {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("want ErrForbidden, got %v (authorized %v)", err, got)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("authorized = %v (err %v), want %v", got, err, tc.want)
			}
		})
	}
}
