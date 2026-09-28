package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/githubapp"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
)

// grantEvaluator answers per connection action, as the policy service does for
// a grant holding exactly the actions in held.
type grantEvaluator struct {
	held map[string]bool
	errs map[string]error
}

func (g grantEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	action := in.PathSegments[1] + ":" + in.Action
	if err := g.errs[action]; err != nil {
		return policy.Result{}, err
	}
	return policy.Result{Allow: g.held[action], Reason: "test grant"}, nil
}

// reactOnPR authorizes the 👀 reaction on PR 1054 for a caller whose grant is
// eval, and returns the decision, the permissions the installation token was
// requested with, and how many tokens were minted.
func reactOnPR(t *testing.T, eval grantEvaluator) (Decision, map[string]string, int, error) {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)

	var minted ghclient.InstallationTokenRequest
	mints := 0
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mints++
		_ = json.NewDecoder(r.Body).Decode(&minted)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_tok", "expires_at": "2099-01-01T00:00:00Z"})
	}))
	t.Cleanup(gh.Close)

	cfg, _ := json.Marshal(map[string]any{"app_id": "1", "installation_id": 1, "owner": "clear-route"})
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-gh", TenantID: "acme", Name: "github",
		Adapter: credentials.AdapterGitHubApp, BaseHost: "api.github.com",
		SecretRef: "ref", Config: cfg,
	})
	registry := NewAdapterRegistry()
	registry.Register(githubapp.New(ghclient.NewClient(gh.URL, logger), stubSecrets{pem: testKeyPEM(t)}, logger))
	broker := NewBroker(eval, repo, registry, passthrough.New(), logger)

	u, _ := url.Parse("https://api.github.com/repos/clear-route/constellation-iac/issues/1054/reactions")
	httpReq, _ := http.NewRequest(http.MethodPost, u.String(), nil)
	dec, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/code-reviewer", "run-1"),
		EgressRequest{Host: "api.github.com", Method: http.MethodPost, URL: u, HTTPRequest: httpReq})
	return dec, minted.Permissions, mints, err
}

// The grant is the boundary: the token carries exactly the categories the grant
// holds, and the request is denied only when it holds neither.
func TestAuthorize_IssueNumberRouteFollowsTheGrant(t *testing.T) {
	cases := []struct {
		name          string
		held          []string
		wantAction    string
		wantIssues    bool
		wantPulls     bool
		wantForbidden bool
	}{
		{name: "issues and pulls", held: []string{"issues:write", "pulls:write"}, wantAction: "issues:write", wantIssues: true, wantPulls: true},
		{name: "issues only", held: []string{"issues:write"}, wantAction: "issues:write", wantIssues: true},
		{name: "pulls only", held: []string{"pulls:write"}, wantAction: "pulls:write", wantPulls: true},
		{name: "neither", held: nil, wantForbidden: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			held := map[string]bool{}
			for _, a := range tc.held {
				held[a] = true
			}
			dec, perms, mints, err := reactOnPR(t, grantEvaluator{held: held})

			if tc.wantForbidden {
				if !errors.Is(err, ErrForbidden) {
					t.Fatalf("err = %v, want ErrForbidden", err)
				}
				if mints != 0 {
					t.Errorf("minted %d tokens for a denied request", mints)
				}
				return
			}
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if dec.Action != tc.wantAction {
				t.Errorf("decision action = %q, want %q", dec.Action, tc.wantAction)
			}
			if got := perms["issues"] == "write"; got != tc.wantIssues {
				t.Errorf("issues:write in token = %v, want %v (permissions %v)", got, tc.wantIssues, perms)
			}
			if got := perms["pull_requests"] == "write"; got != tc.wantPulls {
				t.Errorf("pull_requests:write in token = %v, want %v (permissions %v)", got, tc.wantPulls, perms)
			}
		})
	}
}

// An evaluator error on the classified action still fails closed, exactly as
// before alternates existed, even when an alternate would have been allowed.
func TestAuthorize_PrimaryEvaluationErrorFailsClosed(t *testing.T) {
	_, _, mints, err := reactOnPR(t, grantEvaluator{
		held: map[string]bool{"pulls:write": true},
		errs: map[string]error{"issues:write": errors.New("policy service unavailable")},
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if mints != 0 {
		t.Errorf("minted %d tokens after a failed evaluation", mints)
	}
}

// An evaluator error on an alternate only drops that alternate. The request
// proceeds under the classified action with no widening.
func TestAuthorize_AlternateEvaluationErrorDoesNotWiden(t *testing.T) {
	dec, perms, _, err := reactOnPR(t, grantEvaluator{
		held: map[string]bool{"issues:write": true},
		errs: map[string]error{"pulls:write": errors.New("policy service unavailable")},
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if dec.Action != "issues:write" {
		t.Errorf("decision action = %q, want issues:write", dec.Action)
	}
	if _, ok := perms["pull_requests"]; ok {
		t.Errorf("token widened by an alternate that could not be evaluated: %v", perms)
	}
}
