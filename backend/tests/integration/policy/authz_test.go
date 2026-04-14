//go:build integration

// Package policy_test runs end-to-end authorisation tests against a live cluster.
//
// These tests exec into the tenant agent pod and make real HTTP requests
// through the Envoy sidecar → ext_authz → OPA sidecar chain with real JWTs
// from the identity-proxy. They also test the policy.Evaluator fetching
// bundles from the running policy-service.
//
// Prerequisites:
//
//	make dev-cluster && make cluster-deploy   (cluster running with services)
//	make auth-device-k3d                      (fresh user token at /tmp/orbit_credentials.json)
//
// Run:
//
//	make test-integration
package policy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	jwt "github.com/golang-jwt/jwt/v5"
)

const (
	defaultPolicyServiceURL = "http://localhost:8083"
	defaultTenantID         = "example"
	defaultTenantNamespace  = "orbit-tenant-example"
	defaultExamplePodLabel  = "app=orbit-example"

	testAgentID = "spiffe://orbit.test/tenant/orbit-tenant-example/agent/integration-test"
)

func policyServiceURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("POLICY_SERVICE_URL"); v != "" {
		return v
	}
	return defaultPolicyServiceURL
}

func agentClaims(principalID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: principalID},
		Orbit: token.OrbitClaims{
			PrincipalType: token.PrincipalTypeAgent,
			TenantID:      defaultTenantID,
		},
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 1: live Envoy ext_authz through the OPA sidecar
// ═══════════════════════════════════════════════════════════════════════════

func TestLiveCluster_Layer1_AllowWithGrant(t *testing.T) {
	ensureGrant(t, "AGENT", testAgentID, []string{"inference:invoke", "inference:list"})
	waitForBundleRefresh(t)

	pod := findExamplePod(t)
	status := kubectlExecWget(t, pod, "http://orbit-inference-gateway.orbit-system.svc/v1/models")

	// 503 is acceptable — OPA allowed the request but the upstream has no
	// providers configured. Only 403 means the policy denied the request.
	if status == 403 {
		t.Error("expected OPA to allow the request (got 403)")
	}
}

func TestLiveCluster_Layer1_DenyWithoutGrant(t *testing.T) {
	pod := findExamplePod(t)
	// The agent should have no grant for platform:roles:read.
	status := kubectlExecWget(t, pod, "http://orbit-policy-service.orbit-system.svc/api/v1/policy/roles")
	if status != 403 {
		t.Errorf("expected 403 for unauthorized route, got %d", status)
	}
}

func TestLiveCluster_Layer1_PlatformBypass(t *testing.T) {
	pod := findExamplePod(t)
	// Identity exchange is a platform-internal path — allowed without JWT.
	// The POST returns a non-403 error (empty body → 400/500), which proves
	// OPA's ext_authz did not block it.
	status := kubectlExecWget(t, pod, "http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/exchange")
	if status == 403 {
		t.Error("expected platform bypass to allow identity exchange (got 403)")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Layer 2: policy.Evaluator against real bundle endpoint
// ═══════════════════════════════════════════════════════════════════════════

func TestLiveCluster_Layer2_EvaluatorAgainstRealBundle(t *testing.T) {
	url := policyServiceURL(t)
	evaluator := policy.NewEvaluator(url+"/api/v1/policy/bundles", 30*time.Second, logging.NewLogger())
	ctx := context.Background()

	ensureGrant(t, "AGENT", testAgentID, []string{"inference:invoke"})
	waitForBundleRefresh(t)

	// Allow
	result, err := evaluator.Check(ctx, policy.CheckInput{
		Claims:       agentClaims(testAgentID),
		ResourceType: "inference",
		Action:       "invoke",
	})
	if err != nil {
		t.Fatalf("evaluator.Check: %v", err)
	}
	if !result.Allow {
		t.Errorf("expected allow, got deny (reason: %s)", result.Reason)
	}

	// Deny
	result, err = evaluator.Check(ctx, policy.CheckInput{
		Claims:       agentClaims(testAgentID),
		ResourceType: "platform",
		PathSegments: []string{"tenants"},
		Action:       "manage",
	})
	if err != nil {
		t.Fatalf("evaluator.Check: %v", err)
	}
	if result.Allow {
		t.Error("expected deny for scope not in grant")
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func findExamplePod(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("kubectl", "get", "pod",
		"-n", defaultTenantNamespace,
		"-l", defaultExamplePodLabel,
		"-o", "jsonpath={.items[0].metadata.name}",
	).Output()
	if err != nil {
		t.Fatalf("find example pod: %v", err)
	}
	pod := strings.TrimSpace(string(out))
	if pod == "" {
		t.Fatal("no example pod found")
	}
	return pod
}

func kubectlExecWget(t *testing.T, pod, url string) int {
	t.Helper()
	cmd := exec.Command("kubectl", "exec", pod,
		"-n", defaultTenantNamespace,
		"-c", "orbit-example",
		"--",
		"wget", "-q", "-O-", "-S", "--timeout=5", url,
	)
	output, err := cmd.CombinedOutput()
	outStr := string(output)

	for _, line := range strings.Split(outStr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "HTTP/") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				var code int
				if _, scanErr := fmt.Sscanf(parts[1], "%d", &code); scanErr == nil {
					return code
				}
			}
		}
	}

	// Fallback: parse wget's exit-based status. wget exits 8 for server errors.
	// Only match "server returned error: HTTP/1.1 NNN" to avoid false positives
	// from NNN appearing in URLs or other output.
	if err != nil {
		for _, code := range []int{403, 503, 400, 500, 502} {
			if strings.Contains(outStr, fmt.Sprintf("server returned error: HTTP/1.1 %d", code)) ||
				strings.Contains(outStr, fmt.Sprintf("server returned error: HTTP/1.0 %d", code)) {
				return code
			}
		}
	}
	if err == nil {
		return 200
	}

	t.Logf("wget output: %s", outStr)
	return 0
}

func ensureGrant(t *testing.T, principalType, principalID string, scopes []string) {
	t.Helper()

	url := policyServiceURL(t)
	userToken := loadUserToken(t)

	body := map[string]any{
		"principal_type": principalType,
		"principal_id":   principalID,
		"scopes":         scopes,
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", url+"/api/v1/policy/grants", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("create grant request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 200 && resp.StatusCode != 409 {
		t.Fatalf("create grant: unexpected status %d", resp.StatusCode)
	}

	// Clean up the grant when the test finishes so subsequent runs are hermetic.
	t.Cleanup(func() {
		revokeGrants(t, principalType, principalID)
	})
}

// revokeGrants deletes all grants for the given principal. Best-effort — logs
// failures but does not fail the test, since cleanup runs after assertions.
func revokeGrants(t *testing.T, principalType, principalID string) {
	t.Helper()

	base := policyServiceURL(t)
	userToken := loadUserToken(t)

	listURL := fmt.Sprintf("%s/api/v1/policy/grants?principal_type=%s&principal_id=%s&tenant_id=%s",
		base, principalType, principalID, defaultTenantID)

	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		t.Logf("cleanup: create list request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("cleanup: list grants: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Logf("cleanup: list grants returned %d", resp.StatusCode)
		return
	}

	var body struct {
		Grants []struct {
			ID string `json:"id"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Logf("cleanup: decode grant list: %v", err)
		return
	}

	for _, g := range body.Grants {
		delReq, err := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/policy/grants/%s", base, g.ID), nil)
		if err != nil {
			t.Logf("cleanup: create delete request: %v", err)
			continue
		}
		delReq.Header.Set("Authorization", "Bearer "+userToken)
		delResp, err := http.DefaultClient.Do(delReq)
		if err != nil {
			t.Logf("cleanup: delete grant %s: %v", g.ID, err)
			continue
		}
		delResp.Body.Close()
	}
}

func loadUserToken(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile("/tmp/orbit_credentials.json")
	if err != nil {
		t.Fatal("no credentials file at /tmp/orbit_credentials.json — run: make auth-device-k3d")
	}

	var creds struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		t.Fatalf("parse credentials: %v", err)
	}
	if creds.AccessToken == "" {
		t.Fatal("empty access_token in credentials file")
	}
	return creds.AccessToken
}

func waitForBundleRefresh(t *testing.T) {
	t.Helper()
	t.Log("waiting 35s for OPA bundle refresh...")
	time.Sleep(35 * time.Second)
}
