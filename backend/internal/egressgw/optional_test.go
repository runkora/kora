package egressgw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/githubapp"
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

// TestEvaluatePolicy_HeldActions covers which held actions authorize a request
// and reach the credential: Optional actions, GraphQL categories, and a write
// meeting a read on a GitHub connection, all through policy with delegation.
func TestEvaluatePolicy_HeldActions(t *testing.T) {
	github := githubConn(t, "github", "example-org")
	cluster := &credentials.Connection{Name: "github", Adapter: credentials.AdapterKubernetesBearer}
	gh, err := githubapp.New(nil, nil, logging.NewLoggerWithOutput(io.Discard)).Open(*github)
	if err != nil {
		t.Fatal(err)
	}
	graphql := func(query string) credentials.Classification {
		r := httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", strings.NewReader(`{"query":"`+query+`"}`))
		cls, err := gh.Classify(r)
		if err != nil {
			t.Fatalf("Classify %s: %v", query, err)
		}
		return cls
	}
	query := func(repo string) credentials.Classification {
		return graphql(`{ repository(owner: \"example-org\", name: \"` + repo + `\") { id } }`)
	}
	mutation := graphql(`mutation { mergePullRequest(input: {pullRequestId: \"PR_x\"}) { clientMutationId } }`)
	untargetedQuery := graphql(`{ viewer { login } }`)
	targetedMutation := mutation
	targetedMutation.Qualifier = "widgets"

	push := credentials.Classification{Action: "contents:write", Optional: []string{"workflows:write"}, Qualifier: "widgets"}
	read := func(qualifier string) credentials.Classification {
		return credentials.Classification{Action: "pulls:read", Qualifier: qualifier}
	}
	fork := credentials.Classification{Action: "admin:write", AlsoRequired: []string{"contents:read"}, Qualifier: "widgets"}

	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", RunID: "run-1"}
	pushBoth := heldScopes{"github:contents:widgets:write", "github:workflows:widgets:write"}
	reviewer := coveringScopes{"github:metadata:read", "github:pulls:widgets:read", "github:pulls:widgets:write", "github:issues:widgets:read"}
	pullsWriter := heldScopes{"github:pulls:widgets:write"}
	forkWrites := heldScopes{"github:admin:widgets:write", "github:contents:widgets:write"}

	cases := []struct {
		name       string
		conn       *credentials.Connection
		cls        credentials.Classification
		pol        policyChecker
		delegation *policy.DelegationClaims
		want       []string
	}{
		{name: "held optional is minted", cls: push, pol: pushBoth, want: []string{"contents:write", "workflows:write"}},
		{name: "optional not held is not minted, and the request passes", cls: push, pol: heldScopes{"github:contents:widgets:write"}, want: []string{"contents:write"}},
		{name: "optional alone never authorizes", cls: push, pol: heldScopes{"github:workflows:widgets:write"}},
		{name: "a delegator lacking the optional does not mint it", cls: push, pol: delegatorBound{agent: pushBoth, delegator: heldScopes{"github:contents:widgets:write"}}, delegation: delegation, want: []string{"contents:write"}},
		{name: "an optional that cannot be evaluated is not minted", cls: push, pol: failingOn("github:workflows:widgets:write"), want: []string{"contents:write"}},

		{name: "a query mints the held categories and metadata", cls: query("widgets"), pol: reviewer, want: []string{"issues:read", "pulls:read", "metadata:read"}},
		{name: "a mutation mints the held write categories", cls: targetedMutation, pol: reviewer, want: []string{"pulls:write"}},
		{name: "another repository falls to the metadata baseline", cls: query("gadgets"), pol: reviewer, want: []string{"metadata:read"}},
		{name: "a baseline-only agent gets a metadata-only query", cls: untargetedQuery, pol: coveringScopes{"github:metadata:read"}, want: []string{"metadata:read"}},
		{name: "a baseline-only agent cannot mutate", cls: targetedMutation, pol: coveringScopes{"github:metadata:read"}},
		{name: "an untargeted mutation needs an unqualified write", cls: mutation, pol: reviewer},
		{name: "a write-only query authorizes the read", cls: query("widgets"), pol: pullsWriter, want: []string{"pulls:read"}},

		{name: "a write-only grant reads its repository as a read", cls: read("widgets"), pol: pullsWriter, want: []string{"pulls:read"}},
		{name: "a write-only grant is denied on another repository", cls: read("gadgets"), pol: pullsWriter},
		{name: "a delegator lacking the write is denied the read", cls: read("widgets"), pol: delegatorBound{agent: pullsWriter}, delegation: delegation},
		{name: "another adapter's write does not cover its read", conn: cluster, cls: credentials.Classification{Action: "workloads:read"}, pol: heldScopes{"github:workloads:write"}},

		{name: "the write meets a required read", cls: fork, pol: forkWrites, want: []string{"admin:write", "contents:read"}},
		{name: "the read itself still meets it", cls: fork, pol: heldScopes{"github:admin:widgets:write", "github:contents:widgets:read"}, want: []string{"admin:write", "contents:read"}},
		{name: "neither held is denied", cls: fork, pol: heldScopes{"github:admin:widgets:write"}},
		{name: "a write on another repository does not meet it", cls: fork, pol: heldScopes{"github:admin:widgets:write", "github:contents:gadgets:write"}},
		{name: "a delegator lacking the write does not meet it", cls: fork, pol: delegatorBound{agent: forkWrites, delegator: heldScopes{"github:admin:widgets:write"}}, delegation: delegation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.delegation != nil {
				ctx = policy.WithDelegation(ctx, tc.delegation)
			}
			conn := tc.conn
			if conn == nil {
				conn = github
			}
			b := NewBroker(tc.pol, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
			got, err := b.evaluatePolicy(ctx, &token.PlatformClaims{}, conn, tc.cls, EgressRequest{Host: "api.github.com", Method: "POST"})
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
