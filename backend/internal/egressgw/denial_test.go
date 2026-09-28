package egressgw

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
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
	p := &ProxyHandler{logger: logging.NewLoggerWithOutput(io.Discard)}
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
	want := "Orbit egress policy denied api.github.com: this agent is not granted pulls:read for orbit on connection github. To allow it, grant connection:github:pulls:orbit:read."
	if d.Message() != want {
		t.Errorf("Message() = %q, want %q", d.Message(), want)
	}
}
