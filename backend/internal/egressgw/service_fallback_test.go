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

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

// postureEvaluator answers the broker's egress posture check with ownEgress
// and denies every connection scope, or fails it with connErr.
type postureEvaluator struct {
	ownEgress    bool
	connErr      error
	egressChecks [][]string
}

func (p *postureEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	if in.ResourceType == "egress" {
		p.egressChecks = append(p.egressChecks, append(in.PathSegments, in.Action))
		return policy.Result{Allow: p.ownEgress}, nil
	}
	if p.connErr != nil {
		return policy.Result{}, p.connErr
	}
	return policy.Result{Reason: noMatchingGrant}, nil
}

func (p *postureEvaluator) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

// TestProxy_AnonymousReadFallback covers Amendment 5: a GitHub read the grant
// does not authorize is forwarded anonymously when the agent's own posture
// reaches the host, and every other case keeps its denial.
func TestProxy_AnonymousReadFallback(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		ownEgress  bool
		connErr    error
		wantStatus int
		wantReason DenialReason
	}{
		{"classify error read falls back", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", true, nil, http.StatusOK, ""},
		{"foreign fetch falls back", http.MethodPost, "/octocat/hello-world.git/git-upload-pack", true, nil, http.StatusOK, ""},
		{"policy deny read falls back", http.MethodGet, "/api/v3/repos/orbital/kora/pulls", true, nil, http.StatusOK, ""},
		{"unrecognized read falls back", http.MethodGet, "/orbital/kora/releases/download/v1/x.tgz", true, nil, http.StatusOK, ""},
		{"unrecognized write denied", http.MethodPost, "/orbital/kora/issues", true, nil, http.StatusForbidden, DenialUnclassified},
		{"write denied", http.MethodPost, "/octocat/hello-world.git/git-receive-pack", true, nil, http.StatusForbidden, DenialUnclassified},
		{"push discovery denied", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-receive-pack", true, nil, http.StatusForbidden, DenialUnclassified},
		{"granted-repo write denied by policy", http.MethodPost, "/orbital/kora.git/git-receive-pack", true, nil, http.StatusForbidden, DenialNotGranted},
		{"closed egress with connector scope only denied", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", false, nil, http.StatusForbidden, DenialUnclassified},
		{"policy unavailable never falls back", http.MethodGet, "/api/v3/repos/orbital/kora/pulls", true, errors.New("opa down"), http.StatusForbidden, DenialPolicyUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			cfg, _ := json.Marshal(map[string]any{"app_id": "12345", "installation_id": 42, "owner": "orbital"})
			f.repo.items["acme|github"].Config = cfg
			eval := &postureEvaluator{ownEgress: tc.ownEgress, connErr: tc.connErr}
			f.broker.policy = eval

			resp, err := f.callGateway(tc.method, tc.path, f.connectionHost)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}

			if tc.wantStatus != http.StatusOK {
				if f.upstreamSawMethod != "" {
					t.Errorf("upstream reached on a denied call")
				}
				if body := decodeDenial(t, resp); body.Details["reason"] != string(tc.wantReason) {
					t.Errorf("reason = %v, want %s", body.Details["reason"], tc.wantReason)
				}
				return
			}
			if f.upstreamSawAuth != "" {
				t.Errorf("anonymous read carried Authorization %q, want none", f.upstreamSawAuth)
			}
			if f.upstreamSawMethod != tc.method {
				t.Errorf("upstream method = %q, want %q", f.upstreamSawMethod, tc.method)
			}
			if len(eval.egressChecks) != 1 || eval.egressChecks[0][0] != "127.0.0.1" || eval.egressChecks[0][1] != "connect" {
				t.Errorf("posture checks = %v, want one egress:127.0.0.1:connect", eval.egressChecks)
			}
		})
	}
}

// GitHub refusing an anonymous fallback read with 401, 403 or 404 returns the
// original denial and none of GitHub's response; any other status is relayed.
// See docs/adr/2026-11-15-github-connector-route-coverage.md, Decision item 8.
func TestProxy_AnonymousReadRefusedReturnsDenial(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		upstream   int
		wantReason DenialReason
	}{
		{"404 on a policy-denied read", "/api/v3/repos/orbital/kora/pulls?per_page=1", http.StatusNotFound, DenialNotGranted},
		{"401 on an unclassified clone", "/octocat/hello-world.git/info/refs?service=git-upload-pack", http.StatusUnauthorized, DenialUnclassified},
		{"403 on an unclassified read", "/orbital/kora/releases/download/v1/x.tgz", http.StatusForbidden, DenialUnclassified},
		{"200 is relayed", "/api/v3/repos/orbital/kora/pulls", http.StatusOK, ""},
		{"302 is relayed", "/orbital/kora/releases/download/v1/x.tgz", http.StatusFound, ""},
		{"500 is relayed", "/api/v3/repos/orbital/kora/pulls", http.StatusInternalServerError, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			cfg, _ := json.Marshal(map[string]any{"app_id": "12345", "installation_id": 42, "owner": "orbital"})
			f.repo.items["acme|github"].Config = cfg
			f.broker.policy = &postureEvaluator{ownEgress: true}
			f.upstreamStatus = tc.upstream

			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, _ := http.NewRequest(http.MethodGet, f.gateway.URL+tc.path, nil)
			req.Host = f.connectionHost
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			defer resp.Body.Close()
			if f.upstreamSawMethod != http.MethodGet || f.upstreamSawAuth != "" {
				t.Fatalf("upstream saw %q with Authorization %q, want an anonymous GET", f.upstreamSawMethod, f.upstreamSawAuth)
			}
			if tc.wantReason == "" {
				if resp.StatusCode != tc.upstream || resp.Header.Get("X-Upstream") != "github" {
					t.Errorf("status = %d, X-Upstream = %q, want GitHub's %d relayed", resp.StatusCode, resp.Header.Get("X-Upstream"), tc.upstream)
				}
				return
			}
			if resp.Header.Get("X-Upstream") != "" {
				t.Errorf("GitHub's headers reached the agent")
			}
			body := decodeDenial(t, resp)
			if body.Details["reason"] != string(tc.wantReason) {
				t.Errorf("reason = %v, want %s", body.Details["reason"], tc.wantReason)
			}
			if strings.Contains(body.Message, "per_page") || strings.Contains(body.Message, "service=") {
				t.Errorf("message carries the query string: %q", body.Message)
			}
		})
	}
}

// A connection of another adapter never takes the fallback, even on a read.
func TestAnonymousReadOrDeny_OnlyGitHub(t *testing.T) {
	eval := &postureEvaluator{ownEgress: true}
	b := NewBroker(eval, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
	conn := &credentials.Connection{Name: "cluster", Adapter: credentials.AdapterKubernetesBearer}
	req, _ := http.NewRequest(http.MethodGet, "https://k8s.example.com/api", nil)
	denial := deny(DenialUnclassified, FromHTTP(req), conn, credentials.Classification{}, nil)
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	_, err := b.anonymousReadOrDeny(context.Background(), span, claimsFor("acme", "agent", "run"), conn, FromHTTP(req), denial)
	if !errors.Is(err, ErrForbidden) || len(eval.egressChecks) != 0 {
		t.Errorf("err = %v with %d posture checks, want the denial and no fallback", err, len(eval.egressChecks))
	}
}

// A request the fallback serves logs only the anonymous-read decision. A
// request it declines keeps the denial line.
func TestAuthorize_FallbackLogsOneLine(t *testing.T) {
	tests := []struct {
		name, method, path string
		ownEgress          bool
		want, notWant      string
	}{
		{"unclassified read served", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", true, "egress authorized: anonymous read", "egress denied"},
		{"policy-denied read served", http.MethodGet, "/api/v3/repos/orbital/kora/pulls", true, "egress authorized: anonymous read", "egress denied"},
		{"unclassified read declined", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", false, "egress denied: adapter could not classify action", "anonymous read"},
		{"policy-denied write declined", http.MethodPost, "/api/v3/repos/orbital/kora/pulls", true, "egress denied by policy", "anonymous read"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			cfg, _ := json.Marshal(map[string]any{"app_id": "12345", "installation_id": 42, "owner": "orbital"})
			f.repo.items["acme|github"].Config = cfg
			var logs bytes.Buffer
			b := NewBroker(&postureEvaluator{ownEgress: tc.ownEgress}, f.repo, f.registry, passthrough.New(), logging.NewLoggerWithOutput(&logs))

			r := httptest.NewRequest(tc.method, "https://"+f.connectionHost+tc.path, nil)
			_, _ = b.Authorize(context.Background(), claimsFor("acme", "agent", "run-1"), FromHTTP(r))

			out := logs.String()
			if strings.Count(out, tc.want) != 1 || strings.Contains(out, tc.notWant) {
				t.Errorf("want one %q and no %q, got:\n%s", tc.want, tc.notWant, out)
			}
		})
	}
}

// The fallback keeps the connection's CA trust for a GitHub Enterprise host
// behind a private CA, and carries no credential.
func TestAnonymousReadOrDeny_KeepsConnectionTrust(t *testing.T) {
	b := NewBroker(&postureEvaluator{ownEgress: true}, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
	conn := &credentials.Connection{ID: "conn-ghe", Name: "ghe", Adapter: credentials.AdapterGitHubApp, CACert: "-----BEGIN CERTIFICATE-----"}
	req, _ := http.NewRequest(http.MethodGet, "https://ghe.example.com/other/repo.git/info/refs?service=git-upload-pack", nil)
	_, span := tracer.Start(context.Background(), "test")
	defer span.End()

	d, err := b.anonymousReadOrDeny(context.Background(), span, claimsFor("acme", "agent", "run"), conn, FromHTTP(req), deny(DenialUnclassified, FromHTTP(req), conn, credentials.Classification{}, nil))
	if err != nil {
		t.Fatalf("anonymousReadOrDeny: %v", err)
	}
	if d.Connection != conn || d.Credential.Header != "" || d.Action != anonymousReadAction {
		t.Errorf("decision = %+v, want the original connection, no credential, action %s", d, anonymousReadAction)
	}
}
