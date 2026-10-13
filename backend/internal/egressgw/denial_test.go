package egressgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/platform/logging"
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
			name:       "not granted names the action and the scope that allows it",
			method:     http.MethodGet,
			path:       "/anything",
			setup:      func(f *fixture) { f.eval.allow, f.eval.reason = false, "deny:no_matching_grant" },
			wantReason: DenialNotGranted,
			wantAction: "repo:read",
			wantScope:  "connection:github:repo:read",
			wantInMsg:  []string{"not granted repo:read on connection github", "grant connection:github:repo:read"},
		},
		{
			name:        "a held grant that did not apply names the policy reason, not a scope",
			method:      http.MethodGet,
			path:        "/anything",
			setup:       func(f *fixture) { f.eval.allow, f.eval.reason = false, "deny:delegation_required" },
			wantReason:  DenialGrantNotApplied,
			wantAction:  "repo:read",
			wantInMsg:   []string{"grant for repo:read on connection github does not apply", "deny:delegation_required"},
			wantNoScope: true,
		},
		{
			name:        "unclassified request says no grant can allow it",
			method:      "PROPFIND",
			path:        "/x",
			wantReason:  DenialUnclassified,
			wantInMsg:   []string{"connection github", "no grant can allow it"},
			wantNoScope: true,
		},
		{
			name:        "policy evaluation failure is reported as retryable",
			method:      http.MethodGet,
			path:        "/anything",
			setup:       func(f *fixture) { f.eval.err = errors.New("opa unreachable at 10.0.0.1") },
			wantReason:  DenialPolicyUnavailable,
			wantAction:  "repo:read",
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
			resp, err := f.callGateway(tc.method, tc.path, f.connectionHost)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			defer resp.Body.Close()
			body := decodeDenial(t, resp)

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
	err := deny(DenialCredentialRefused, "api.github.com",
		&credentials.Connection{Name: "github"},
		credentials.Classification{Action: "issues:write", Qualifier: "orbit"},
		errors.Join(credentials.ErrCredentialScopeRejected, cause))

	rec := httptest.NewRecorder()
	p.handleBrokerError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
	body := decodeDenial(t, rec.Result())

	if body.Details["reason"] != string(DenialCredentialRefused) {
		t.Errorf("reason = %v, want credential_refused", body.Details["reason"])
	}
	for _, want := range []string{"issues:write for orbit", "connection github", "App installation"} {
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
	err := deny(DenialCredentialRefused, "h", &credentials.Connection{Name: "c"}, credentials.Classification{}, credentials.ErrCredentialScopeRejected)
	if !errors.Is(err, ErrForbidden) || !errors.Is(err, credentials.ErrCredentialScopeRejected) {
		t.Fatalf("Denial does not unwrap to ErrForbidden and its cause: %v", err)
	}
}

// A qualified action names its resource and the qualified scope a grant needs.
func TestDeny_NotGrantedQualified(t *testing.T) {
	d := deny(DenialNotGranted, "api.github.com", &credentials.Connection{Name: "github"},
		credentials.Classification{Action: "pulls:read", Qualifier: "orbit"}, nil)
	if d.RequiredScope != "connection:github:pulls:orbit:read" {
		t.Errorf("RequiredScope = %q, want connection:github:pulls:orbit:read", d.RequiredScope)
	}
	want := "Acme egress policy denied api.github.com: this agent is not granted pulls:read for orbit on connection github. To allow it, grant connection:github:pulls:orbit:read."
	if d.Message("Acme") != want {
		t.Errorf("Message() = %q, want %q", d.Message("Acme"), want)
	}
}

// The hint names the classified action, never an alternate, so it points at the
// narrowest grant that allows the call.
func TestDeny_ScopeNamesClassifiedActionNotAlternate(t *testing.T) {
	d := deny(DenialNotGranted, "api.github.com", &credentials.Connection{Name: "github"},
		credentials.Classification{Action: "issues:read", Qualifier: "orbit", Alternates: []string{"pulls:read"}}, nil)
	if d.RequiredScope != "connection:github:issues:orbit:read" {
		t.Errorf("RequiredScope = %q, want connection:github:issues:orbit:read", d.RequiredScope)
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

// Deny logs carry the method and path so a denial can be diagnosed, but never
// the query string or headers, which can carry tokens.
func TestAuthorize_DenyLogsMethodAndPathOnly(t *testing.T) {
	tests := []struct {
		name, method, path, wantLog string
		setup                       func(f *fixture)
	}{
		{name: "policy denial", method: http.MethodGet, path: "/anything", wantLog: "egress denied by policy",
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
			for _, want := range []string{tc.wantLog, tc.method, tc.path} {
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
