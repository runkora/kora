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
	"github.com/clear-route/orbit/internal/platform/policy"
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
		{"policy deny read falls back", http.MethodGet, "/orbital/orbit/pulls", true, nil, http.StatusOK, ""},
		{"write denied", http.MethodPost, "/octocat/hello-world.git/git-receive-pack", true, nil, http.StatusForbidden, DenialUnclassified},
		{"push discovery denied", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-receive-pack", true, nil, http.StatusForbidden, DenialUnclassified},
		{"granted-repo write denied by policy", http.MethodPost, "/orbital/orbit.git/git-receive-pack", true, nil, http.StatusForbidden, DenialNotGranted},
		{"closed egress with connector scope only denied", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", false, nil, http.StatusForbidden, DenialUnclassified},
		{"policy unavailable never falls back", http.MethodGet, "/orbital/orbit/pulls", true, errors.New("opa down"), http.StatusForbidden, DenialPolicyUnavailable},
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

// A connection of another adapter never takes the fallback, even on a read.
func TestAnonymousReadOrDeny_OnlyGitHub(t *testing.T) {
	eval := &postureEvaluator{ownEgress: true}
	b := NewBroker(eval, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))
	conn := &credentials.Connection{Name: "cluster", Adapter: credentials.AdapterKubernetesBearer}
	req, _ := http.NewRequest(http.MethodGet, "https://k8s.example.com/api", nil)
	denial := deny(DenialUnclassified, "k8s.example.com", conn, credentials.Classification{}, nil)
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
		{"policy-denied read served", http.MethodGet, "/orbital/orbit/pulls", true, "egress authorized: anonymous read", "egress denied"},
		{"unclassified read declined", http.MethodGet, "/octocat/hello-world.git/info/refs?service=git-upload-pack", false, "egress denied: adapter could not classify action", "anonymous read"},
		{"policy-denied write declined", http.MethodPost, "/orbital/orbit/pulls", true, "egress denied by policy", "anonymous read"},
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
