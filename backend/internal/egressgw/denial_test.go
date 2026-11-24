package egressgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

type denialBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func decodeDenial(t *testing.T, resp *http.Response) denialBody {
	t.Helper()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	var body denialBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden (stable for existing clients)", body.Code)
	}
	if strings.Contains(body.Message, "—") {
		t.Errorf("message contains an em dash: %q", body.Message)
	}
	return body
}

// Each broker deny path, end to end through the proxy, names the host and the
// fix without reaching the upstream.
func TestProxy_DenialBodyPerReason(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		setup       func(f *fixture)
		wantReason  DenialReason
		wantAction  string
		wantScope   string
		wantInMsg   []string
		wantNoScope bool
	}{
		{
			name:       "not granted names the request, the action and the scope that allows it",
			method:     http.MethodGet,
			path:       "/api/v3/repos/kora/kora/pulls",
			setup:      func(f *fixture) { f.eval.allow, f.eval.reason = false, "deny:no_matching_grant" },
			wantReason: DenialNotGranted,
			wantAction: "pulls:read",
			wantScope:  "connection:github:pulls:read",
			wantInMsg:  []string{"denied GET %s/api/v3/repos/kora/kora/pulls:", "not granted pulls:read on connection github", "grant connection:github:pulls:read"},
		},
		{
			name:        "a held grant that did not apply names the policy reason, not a scope",
			method:      http.MethodGet,
			path:        "/api/v3/repos/kora/kora/pulls",
			setup:       func(f *fixture) { f.eval.allow, f.eval.reason = false, "deny:delegation_required" },
			wantReason:  DenialGrantNotApplied,
			wantAction:  "pulls:read",
			wantInMsg:   []string{"grant for pulls:read on connection github does not apply", "deny:delegation_required"},
			wantNoScope: true,
		},
		{
			name:        "unclassified request names the request and says no grant can allow it",
			method:      "PROPFIND",
			path:        "/x",
			wantReason:  DenialUnclassified,
			wantInMsg:   []string{"denied PROPFIND %s/x:", "connection github", "an unsupported API shape", "no grant can allow it"},
			wantNoScope: true,
		},
		{
			name:        "an unrecognized route names the request",
			method:      http.MethodPost,
			path:        "/api/v3/repos/kora/kora/not-a-route",
			wantReason:  DenialUnclassified,
			wantInMsg:   []string{"denied POST %s/api/v3/repos/kora/kora/not-a-route:", "no grant can allow it"},
			wantNoScope: true,
		},
		{
			name:        "policy evaluation failure is reported as retryable",
			method:      http.MethodGet,
			path:        "/api/v3/repos/kora/kora/pulls",
			setup:       func(f *fixture) { f.eval.err = errors.New("opa unreachable at 10.0.0.1") },
			wantReason:  DenialPolicyUnavailable,
			wantAction:  "pulls:read",
			wantInMsg:   []string{"could not be evaluated", "Retry"},
			wantNoScope: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.callGateway(tc.method, tc.path+"?access_token=query-secret", f.connectionHost)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			defer resp.Body.Close()
			body := decodeDenial(t, resp)
			if raw, _ := json.Marshal(body); strings.Contains(string(raw), "query-secret") {
				t.Errorf("denial carries the query string: %s", raw)
			}
			if body.Details["method"] != tc.method || body.Details["path"] != tc.path {
				t.Errorf("details method/path = %v %v, want %s %s", body.Details["method"], body.Details["path"], tc.method, tc.path)
			}

			if f.upstreamSawAuth != "" {
				t.Errorf("upstream was reached on a denied egress")
			}
			if body.Details["reason"] != string(tc.wantReason) {
				t.Errorf("reason = %v, want %s", body.Details["reason"], tc.wantReason)
			}
			if body.Details["host"] != f.connectionHost || !strings.Contains(body.Message, f.connectionHost) {
				t.Errorf("body does not name the host %q: %+v", f.connectionHost, body)
			}
			if tc.wantAction != "" && body.Details["action"] != tc.wantAction {
				t.Errorf("action = %v, want %s", body.Details["action"], tc.wantAction)
			}
			if tc.wantScope != "" && body.Details["required_scope"] != tc.wantScope {
				t.Errorf("required_scope = %v, want %s", body.Details["required_scope"], tc.wantScope)
			}
			if _, ok := body.Details["required_scope"]; tc.wantNoScope && ok {
				t.Errorf("required_scope present on a %s denial, where no grant fixes it", tc.wantReason)
			}
			for _, want := range tc.wantInMsg {
				if strings.Contains(want, "%s") {
					want = fmt.Sprintf(want, f.connectionHost)
				}
				if !strings.Contains(body.Message, want) {
					t.Errorf("message %q does not contain %q", body.Message, want)
				}
			}
			if strings.Contains(body.Message, "10.0.0.1") {
				t.Errorf("message leaks the underlying error: %q", body.Message)
			}
		})
	}
}

// The upstream refusing a credential is not fixable by a grant, so the body
// points at the upstream permission and carries no credential material.
func TestHandleBrokerError_CredentialRefused(t *testing.T) {
	p := &ProxyHandler{brand: testBrand, logger: logging.NewLoggerWithOutput(io.Discard)}
	cause := errors.New("installation permissions rejected; token ghs_secret")
	err := deny(DenialCredentialRefused, EgressRequest{Host: "api.github.com"},
		&credentials.Connection{Name: "github"},
		credentials.Classification{Action: "issues:write", Qualifier: "kora"},
		errors.Join(credentials.ErrCredentialScopeRejected, cause))

	rec := httptest.NewRecorder()
	p.handleBrokerError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
	body := decodeDenial(t, rec.Result())

	if body.Details["reason"] != string(DenialCredentialRefused) {
		t.Errorf("reason = %v, want credential_refused", body.Details["reason"])
	}
	for _, want := range []string{"issues:write for kora", "connection github", "App installation"} {
		if !strings.Contains(body.Message, want) {
			t.Errorf("message %q does not contain %q", body.Message, want)
		}
	}
	if strings.Contains(body.Message, "ghs_") {
		t.Errorf("message leaks credential material: %q", body.Message)
	}
}

// Denial must stay matchable as ErrForbidden and as its cause, so existing
// errors.Is callers keep working.
func TestDenial_Unwraps(t *testing.T) {
	err := deny(DenialCredentialRefused, EgressRequest{Host: "h"}, &credentials.Connection{Name: "c"}, credentials.Classification{}, credentials.ErrCredentialScopeRejected)
	if !errors.Is(err, ErrForbidden) || !errors.Is(err, credentials.ErrCredentialScopeRejected) {
		t.Fatalf("Denial does not unwrap to ErrForbidden and its cause: %v", err)
	}
}

// A qualified action names the request, its resource and the qualified scope a
// grant needs, and never the query string.
func TestDeny_NotGrantedQualified(t *testing.T) {
	req := FromHTTP(httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/example-org/kora/pulls?access_token=query-secret", nil))
	d := deny(DenialNotGranted, req, &credentials.Connection{Name: "github"},
		credentials.Classification{Action: "pulls:read", Qualifier: "kora"}, nil)
	if d.RequiredScope != "connection:github:pulls:kora:read" {
		t.Errorf("RequiredScope = %q, want connection:github:pulls:kora:read", d.RequiredScope)
	}
	want := "Acme egress policy denied GET api.github.com/repos/example-org/kora/pulls: this agent is not granted pulls:read for kora on connection github. To allow it, grant connection:github:pulls:kora:read."
	if d.Message("Acme") != want {
		t.Errorf("Message() = %q, want %q", d.Message("Acme"), want)
	}
}

// An unclassified GraphQL request states why its body could not be classified.
func TestDeny_UnclassifiedGraphQLStatesReason(t *testing.T) {
	req := FromHTTP(httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", nil))
	cause := &credentials.UnclassifiedError{Detail: "GraphQL query does not parse"}
	d := deny(DenialUnclassified, req, &credentials.Connection{Name: "github"}, credentials.Classification{}, cause)
	want := "Acme egress policy denied POST api.github.com/graphql: connection github does not map this request to a grantable action (GraphQL query does not parse), so no grant can allow it."
	if d.Message("Acme") != want {
		t.Errorf("Message() = %q, want %q", d.Message("Acme"), want)
	}
	if d.Details()["unrecognized"] != cause.Detail {
		t.Errorf("details = %v, want unrecognized %q", d.Details(), cause.Detail)
	}
}

// The hint names the classified action, never an alternate, so it points at the
// narrowest grant that allows the call.
func TestDeny_ScopeNamesClassifiedActionNotAlternate(t *testing.T) {
	d := deny(DenialNotGranted, EgressRequest{Host: "api.github.com"}, &credentials.Connection{Name: "github"},
		credentials.Classification{Action: "issues:read", Qualifier: "kora", Alternates: []string{"pulls:read"}}, nil)
	if d.RequiredScope != "connection:github:issues:kora:read" {
		t.Errorf("RequiredScope = %q, want connection:github:issues:kora:read", d.RequiredScope)
	}
}

// A GraphQL mutation denial states that any category at write on the
// repository allows it, not contents:write alone.
func TestDeny_NotGrantedNamesAlternates(t *testing.T) {
	req := FromHTTP(httptest.NewRequest(http.MethodPost, "https://api.github.com/graphql", nil))
	d := deny(DenialNotGranted, req, &credentials.Connection{Name: "github"},
		credentials.Classification{Action: "contents:write", Qualifier: "kora", Alternates: []string{"issues:write", "pulls:write"}}, nil)
	want := "Example egress policy denied POST api.github.com/graphql: this agent is not granted contents:write for kora on connection github, nor any of issues:write, pulls:write, each of which would allow it. To allow it, grant one of them, such as connection:github:contents:kora:write."
	if got := d.Message("Example"); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}

// A held grant that did not apply, such as one missing its delegator, states
// the request like a not-granted denial, never its query string.
func TestDeny_GrantNotAppliedStatesRequest(t *testing.T) {
	req := FromHTTP(httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/example-org/kora/pulls?access_token=query-secret", nil))
	d := deny(DenialGrantNotApplied, req, &credentials.Connection{Name: "github"}, credentials.Classification{Action: "pulls:read", Qualifier: "kora"}, nil)
	d.PolicyReason = "deny:no_delegator_grant"
	want := "Example egress policy denied GET api.github.com/repos/example-org/kora/pulls: this agent's grant for pulls:read for kora on connection github does not apply to this call (deny:no_delegator_grant)."
	if got := d.Message("Example"); got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}

// The denial reason follows the most informative decision across the primary
// and alternate actions, and operational results never read as a grant problem.
func TestPolicyDenialReason(t *testing.T) {
	tests := []struct {
		reasons   []string
		want      DenialReason
		wantLabel string
	}{
		{[]string{"deny:no_matching_grant"}, DenialNotGranted, ""},
		{[]string{"deny:delegation_required"}, DenialGrantNotApplied, "deny:delegation_required"},
		{[]string{"deny:no_matching_grant", "deny:delegation_required"}, DenialGrantNotApplied, "deny:delegation_required"},
		{[]string{"no policy configured for tenant"}, DenialPolicyUnavailable, ""},
		{[]string{"deny:no_policy_result", "deny:no_matching_grant"}, DenialNotGranted, ""},
	}
	for _, tc := range tests {
		got, label := policyDenialReason(tc.reasons)
		if got != tc.want || label != tc.wantLabel {
			t.Errorf("policyDenialReason(%v) = (%s, %q), want (%s, %q)", tc.reasons, got, label, tc.want, tc.wantLabel)
		}
	}
}

// Deny logs carry the method, path and principal so a denial can be diagnosed
// and attributed to an agent, but never the query string or headers, which can
// carry tokens.
func TestAuthorize_DenyLogsMethodAndPathOnly(t *testing.T) {
	tests := []struct {
		name, method, path, wantLog string
		setup                       func(f *fixture)
	}{
		{name: "policy denial", method: http.MethodGet, path: "/api/v3/repos/kora/kora/pulls", wantLog: "egress denied by policy",
			setup: func(f *fixture) { f.eval.allow = false }},
		{name: "classify denial", method: "PROPFIND", path: "/other-owner/repo.git/info/refs", wantLog: "egress denied: adapter could not classify action"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			var logs bytes.Buffer
			broker := NewBroker(f.eval, f.repo, f.registry, passthrough.New(), logging.NewLoggerWithOutput(&logs))

			r := httptest.NewRequest(tc.method, "https://"+f.connectionHost+tc.path+"?access_token=query-secret", nil)
			r.Header.Set("Authorization", "Bearer header-secret")
			_, err := broker.Authorize(context.Background(), claimsFor("acme", "agent", "run-1"), FromHTTP(r))
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}

			out := logs.String()
			for _, want := range []string{tc.wantLog, tc.method, tc.path, `"principal_id":"agent"`} {
				if !strings.Contains(out, want) {
					t.Errorf("deny log missing %q:\n%s", want, out)
				}
			}
			for _, leak := range []string{"query-secret", "header-secret", "access_token"} {
				if strings.Contains(out, leak) {
					t.Errorf("deny log leaks %q:\n%s", leak, out)
				}
			}
		})
	}
}

// refusingAdapter classifies every request and fails Mint with the sentinel the
// GitHub App adapter returns when the installation lacks the permission.
type refusingAdapter struct{}

func (refusingAdapter) Type() credentials.AdapterType { return credentials.AdapterGitHubApp }
func (refusingAdapter) Actions() []credentials.ActionDeclaration {
	return []credentials.ActionDeclaration{{Action: "contents:read"}}
}
func (refusingAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	return refusingConnector{}, nil
}

type refusingConnector struct{}

func (refusingConnector) Classify(*http.Request) (credentials.Classification, error) {
	return credentials.Classification{Action: "contents:read"}, nil
}
func (refusingConnector) Mint(context.Context, credentials.CallerIdentity) (credentials.Credential, error) {
	return credentials.Credential{}, credentials.ErrCredentialScopeRejected
}
func (refusingConnector) QualifierCeiling() []string { return nil }

// A credential refusal at mint time is attributed to the agent in the log.
func TestAuthorize_CredentialRefusedLogsPrincipal(t *testing.T) {
	f := newFixture(t)
	registry := NewAdapterRegistry()
	registry.Register(refusingAdapter{})
	var logs bytes.Buffer
	broker := NewBroker(f.eval, f.repo, registry, passthrough.New(), logging.NewLoggerWithOutput(&logs))

	r := httptest.NewRequest(http.MethodGet, "https://"+f.connectionHost+"/anything", nil)
	_, err := broker.Authorize(context.Background(), claimsFor("acme", "agent", "run-1"), FromHTTP(r))
	if !errors.Is(err, ErrForbidden) || !errors.Is(err, credentials.ErrCredentialScopeRejected) {
		t.Fatalf("err = %v, want forbidden credential refusal", err)
	}
	out := logs.String()
	for _, want := range []string{"upstream refused credential scope", `"principal_id":"agent"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}
