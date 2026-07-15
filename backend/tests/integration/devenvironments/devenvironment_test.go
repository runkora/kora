//go:build integration

// Package devenvironments_test runs end-to-end tests for the Ephemeral Dev
// Environments feature against a live cloud staging cluster (never k3d —
// this feature is cloud-only per the ADR's Track V decision). Tests drive
// the real HTTP API (cmd/identity's devenvironments Controller), the real
// policy service (grants), and the real cluster (kubectl) — there is no
// fake or in-process substitute for a vcluster, Istio, or SPIRE.
//
// Prerequisites:
//
//	Track V cloud enablement complete (devenv node pool, image pre-pull,
//	  registry wiring, ingress/DNS, SPIRE/CSI, JWKS reachability — see
//	  code_plan.md §4).
//	The branch's operator + identity images deployed to staging.
//	A kubeconfig with cluster-admin (or namespace/CRD-scoped equivalent)
//	  against the staging host, and a fresh user token at
//	  /tmp/orbit_credentials.json (make auth-device / make auth-refresh
//	  against the staging identity API).
//
// Configuration (env vars, all read once per test via helpers below):
//
//	ORBIT_INTEGRATION_API_URL          identity API base URL (default http://localhost:8080)
//	ORBIT_INTEGRATION_KUBECONFIG       path to the staging kubeconfig (default: kubectl's own resolution)
//	ORBIT_INTEGRATION_DEVENV_REF       pinned image/chart ref to provision, e.g. "main-a1b2c3d" (required —
//	                                    deliberately no "main" default, so this suite always exercises a
//	                                    specific, reproducible build rather than a floating tag)
//	ORBIT_INTEGRATION_OPERATOR_NAMESPACE  namespace the operator runs in (default orbit-system)
//	ORBIT_INTEGRATION_HOST_API_SERVER  the staging cluster's real API server URL, used only by
//	                                    TestDevEnv_KubeconfigWorksInVClusterAndIsHostRejected's CA-isolation
//	                                    check; that sub-check is skipped (not failed) if unset
//	POLICY_SERVICE_URL                 policy service base URL (default http://localhost:8083, mirrors
//	                                    tests/integration/policy/authz_test.go)
//
// Run:
//
//	make test-integration-devenv
//
// Each provisioning-heavy test (Ready convergence, sleep/wake, TTL sweep,
// operator-kill) takes several minutes — this suite is designed to run on a
// schedule (see .github/workflows/devenv-integration.yml), not on every push.
package devenvironments_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/clear-route/orbit/internal/devenvironments"
	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/orbitapi"
)

// ═══════════════════════════════════════════════════════════════════════════
// Configuration helpers
// ═══════════════════════════════════════════════════════════════════════════

const defaultAPIURL = "http://localhost:8080"

func apiURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("ORBIT_INTEGRATION_API_URL"); v != "" {
		return v
	}
	return defaultAPIURL
}

// pinnedRef returns the image/chart ref every provisioning test installs.
// Skips (never defaults to "main") when unset: a floating ref would make
// this suite a moving target instead of the version-bump regression check
// it's meant to be (see also scripts/cluster/spike-*.sh, ported by this same
// WP) — and `make test-integration-devenv` is the only target expected to
// set it, so `go test ./... -tags=integration` elsewhere (e.g. the
// k3d-oriented `make test-integration`) skips cleanly instead of failing.
func pinnedRef(t *testing.T) string {
	t.Helper()
	v := os.Getenv("ORBIT_INTEGRATION_DEVENV_REF")
	if v == "" {
		t.Skip("ORBIT_INTEGRATION_DEVENV_REF not set (e.g. main-a1b2c3d) — skipping; refusing to float \"main\"")
	}
	return v
}

func operatorNamespace() string {
	if v := os.Getenv("ORBIT_INTEGRATION_OPERATOR_NAMESPACE"); v != "" {
		return v
	}
	return "orbit-system"
}

func hostAPIServer() (string, bool) {
	v := os.Getenv("ORBIT_INTEGRATION_HOST_API_SERVER")
	return v, v != ""
}

func kubeconfigPath() string {
	return os.Getenv("ORBIT_INTEGRATION_KUBECONFIG")
}

// kubectlArgs prefixes --kubeconfig when ORBIT_INTEGRATION_KUBECONFIG is
// set; otherwise kubectl falls back to its own default resolution (current
// context), exactly like a developer's shell.
func kubectlArgs(args ...string) []string {
	if kc := kubeconfigPath(); kc != "" {
		return append([]string{"--kubeconfig", kc}, args...)
	}
	return args
}

func kubectl(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("kubectl", kubectlArgs(args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func policyServiceURL() string {
	if v := os.Getenv("POLICY_SERVICE_URL"); v != "" {
		return v
	}
	return "http://localhost:8083"
}

// loadUserToken reads the platform token cached by `make auth-device` /
// `make auth-refresh` — the same file and shape as
// tests/integration/policy/authz_test.go's loadUserToken. Duplicated rather
// than shared: this repo has no cross-integration-package test-support
// package, and authz_test.go doesn't export one either.
func loadUserToken(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("/tmp/orbit_credentials.json")
	if err != nil {
		t.Fatal("no credentials file at /tmp/orbit_credentials.json — run: make auth-device (or make auth-refresh)")
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

// jwtSub decodes a JWT without verifying its signature and returns its sub
// claim — used only to identify the caller for grant management, never for
// authorization decisions.
func jwtSub(t *testing.T, rawToken string) string {
	t.Helper()
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		t.Fatalf("token does not look like a JWT (parts=%d)", len(parts))
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		t.Fatalf("decode jwt payload: %v", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal jwt claims: %v", err)
	}
	if claims.Sub == "" {
		t.Fatal("token has no sub claim")
	}
	return claims.Sub
}

// ═══════════════════════════════════════════════════════════════════════════
// devenvironments HTTP client helpers
// ═══════════════════════════════════════════════════════════════════════════

type devenvClient struct {
	t     *testing.T
	base  string
	token string
}

func newDevenvClient(t *testing.T, token string) *devenvClient {
	t.Helper()
	return &devenvClient{t: t, base: apiURL(t) + "/api/v1/dev-environments", token: token}
}

// do issues an HTTP request and decodes either a 2xx JSON body into out, or
// a non-2xx body into an *api.APIError, returned alongside the status code.
func (c *devenvClient) do(method, path string, body any, out any) (int, *api.APIError) {
	c.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && resp.StatusCode != http.StatusNoContent {
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				c.t.Fatalf("%s %s: decode response: %v", method, path, err)
			}
		}
		return resp.StatusCode, nil
	}

	var apiErr api.APIError
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	return resp.StatusCode, &apiErr
}

func (c *devenvClient) create(ref string) (*devenvironments.Environment, int, *api.APIError) {
	var env devenvironments.Environment
	status, apiErr := c.do(http.MethodPost, "", devenvironments.CreateRequest{Ref: ref}, &env)
	if apiErr != nil {
		return nil, status, apiErr
	}
	return &env, status, nil
}

func (c *devenvClient) get(id string) (*devenvironments.Environment, int, *api.APIError) {
	var env devenvironments.Environment
	status, apiErr := c.do(http.MethodGet, "/"+id, nil, &env)
	if apiErr != nil {
		return nil, status, apiErr
	}
	return &env, status, nil
}

func (c *devenvClient) listAll() (int, *api.APIError) {
	return c.do(http.MethodGet, "/all", nil, &struct {
		Environments []devenvironments.Environment `json:"environments"`
	}{})
}

func (c *devenvClient) wake(id string) (*devenvironments.Environment, int, *api.APIError) {
	var env devenvironments.Environment
	status, apiErr := c.do(http.MethodPost, "/"+id+"/wake", nil, &env)
	if apiErr != nil {
		return nil, status, apiErr
	}
	return &env, status, nil
}

func (c *devenvClient) kubeconfig(id string) (*devenvironments.KubeconfigResponse, int, *api.APIError) {
	var resp devenvironments.KubeconfigResponse
	status, apiErr := c.do(http.MethodPost, "/"+id+"/kubeconfig", nil, &resp)
	if apiErr != nil {
		return nil, status, apiErr
	}
	return &resp, status, nil
}

func (c *devenvClient) destroy(id string) (int, *api.APIError) {
	return c.do(http.MethodDelete, "/"+id, nil, nil)
}

// destroyBestEffort is used from t.Cleanup — logs rather than fails, since
// cleanup runs after the test's own assertions have already determined
// pass/fail.
func (c *devenvClient) destroyBestEffort(id string) {
	status, apiErr := c.destroy(id)
	if status != http.StatusNoContent && status != http.StatusNotFound {
		msg := ""
		if apiErr != nil {
			msg = apiErr.Message
		}
		c.t.Logf("cleanup: destroy %s returned %d: %s", id, status, msg)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Waiters
// ═══════════════════════════════════════════════════════════════════════════

// waitForPhase polls the CR (not the API's cached registry row) until
// status.phase equals want or the timeout elapses, returning the final
// observed CR JSON for condition inspection.
func waitForPhase(t *testing.T, id, want string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		out, err := kubectl(t, "get", "orbitdevenvironment", id, "-o", "json")
		if err == nil {
			var cr map[string]any
			if jsonErr := json.Unmarshal([]byte(out), &cr); jsonErr == nil {
				last = cr
				if status, ok := cr["status"].(map[string]any); ok {
					if phase, ok := status["phase"].(string); ok && phase == want {
						return cr
					}
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("timed out after %s waiting for orbitdevenvironment/%s to reach phase %q (last observed: %+v)",
		timeout, id, want, last)
	return nil
}

// conditionsOf extracts status.conditions from a decoded CR (as produced by
// waitForPhase) into a type→status map for terse assertions.
func conditionsOf(cr map[string]any) map[string]string {
	out := map[string]string{}
	status, ok := cr["status"].(map[string]any)
	if !ok {
		return out
	}
	conds, ok := status["conditions"].([]any)
	if !ok {
		return out
	}
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		st, _ := m["status"].(string)
		if typ != "" {
			out[typ] = st
		}
	}
	return out
}

func hostNamespace(id string) string {
	return orbitapi.DevEnvNamespacePrefix + id
}

// ═══════════════════════════════════════════════════════════════════════════
// 1. Provision → Ready with per-stage conditions
// ═══════════════════════════════════════════════════════════════════════════

// TestDevEnvironmentLifecycle provisions ONE environment and exercises most
// of the ADR §11 checklist against it in sequence (subtests run in
// declaration order, sharing the same environment) — provisioning a fresh
// vcluster per checklist item would multiply an already 5+ minute-per-item
// suite. Quota, TTL-expiry, and operator-kill-mid-provision need their own
// environments and are separate top-level tests below.
func TestDevEnvironmentLifecycle(t *testing.T) {
	ref := pinnedRef(t)
	token := loadUserToken(t)
	client := newDevenvClient(t, token)

	env, status, apiErr := client.create(ref)
	if apiErr != nil {
		t.Fatalf("create environment: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
	}
	if status != http.StatusCreated {
		t.Fatalf("create environment: expected 201, got %d", status)
	}
	id := env.ID
	t.Logf("provisioned dev environment %q (ref=%s)", id, ref)
	t.Cleanup(func() { client.destroyBestEffort(id) })

	t.Run("ProvisionReachesReadyWithPerStageConditions", func(t *testing.T) {
		cr := waitForPhase(t, id, orbitapi.DevEnvPhaseReady, 6*time.Minute)
		conds := conditionsOf(cr)
		for _, want := range []string{
			orbitapi.ConditionDevEnvNamespaceReady,
			orbitapi.ConditionDevEnvVClusterReady,
			orbitapi.ConditionDevEnvKubeconfigCSR,
			orbitapi.ConditionDevEnvPlatformApplied,
			orbitapi.ConditionDevEnvPlatformReady,
		} {
			if got := conds[want]; got != "True" {
				t.Errorf("condition %s = %q, want True (all conditions: %+v)", want, got, conds)
			}
		}
		if got := conds[orbitapi.ConditionDevEnvProgressing]; got != "True" {
			t.Errorf("condition %s = %q, want True once Ready", orbitapi.ConditionDevEnvProgressing, got)
		}

		spec, _ := cr["spec"].(map[string]any)
		if spec["ref"] != ref {
			t.Errorf("spec.ref = %v, want %q", spec["ref"], ref)
		}
		st, _ := cr["status"].(map[string]any)
		if resolved, _ := st["resolvedRef"].(string); resolved == "" {
			t.Error("status.resolvedRef is empty — expected the pinned ref to be recorded even when spec.ref is symbolic")
		}
	})

	t.Run("KubeconfigWorksInVClusterAndIsHostRejected", func(t *testing.T) {
		kc, status, apiErr := client.kubeconfig(id)
		if apiErr != nil {
			t.Fatalf("mint kubeconfig: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
		}
		if status != http.StatusOK {
			t.Fatalf("mint kubeconfig: expected 200, got %d", status)
		}
		if time.Until(kc.ExpiresAt) > 8*time.Hour+time.Minute {
			t.Errorf("kubeconfig expiresAt %s is more than 8h out — expected the per-request 8h lifetime (pinned decision)", kc.ExpiresAt)
		}

		kcFile, err := os.CreateTemp(t.TempDir(), "devenv-*.kubeconfig")
		if err != nil {
			t.Fatalf("create temp kubeconfig file: %v", err)
		}
		if _, err := kcFile.WriteString(kc.Kubeconfig); err != nil {
			t.Fatalf("write kubeconfig: %v", err)
		}
		kcFile.Close()

		out, err := exec.Command("kubectl", "--kubeconfig", kcFile.Name(), "get", "ns").CombinedOutput()
		if err != nil {
			t.Errorf("brokered kubeconfig failed against the vcluster (expected success): %v\n%s", err, out)
		}

		hostServer, ok := hostAPIServer()
		if !ok {
			t.Log("ORBIT_INTEGRATION_HOST_API_SERVER not set — skipping host-rejection sub-check")
			return
		}
		hostRejectKC, err := retargetKubeconfigServer(kc.Kubeconfig, hostServer)
		if err != nil {
			t.Fatalf("retarget kubeconfig server: %v", err)
		}
		hostKCFile, err := os.CreateTemp(t.TempDir(), "devenv-host-reject-*.kubeconfig")
		if err != nil {
			t.Fatalf("create temp kubeconfig file: %v", err)
		}
		if _, err := hostKCFile.WriteString(hostRejectKC); err != nil {
			t.Fatalf("write retargeted kubeconfig: %v", err)
		}
		hostKCFile.Close()

		out, err = exec.Command("kubectl", "--kubeconfig", hostKCFile.Name(), "get", "ns").CombinedOutput()
		if err == nil {
			t.Errorf("brokered kubeconfig was ACCEPTED by the host API server (%s) — vcluster client-CA isolation is broken\n%s", hostServer, out)
		} else {
			t.Logf("host API server correctly rejected the vcluster-scoped kubeconfig: %s", strings.TrimSpace(string(out)))
		}
	})

	t.Run("NestedProvisioningForbiddenFromInsideTheEnvironment", func(t *testing.T) {
		env, _, apiErr := client.get(id)
		if apiErr != nil {
			t.Fatalf("get environment: %s", apiErr.Message)
		}
		if env.AppURL == "" {
			t.Fatal("environment has no appURL yet — expected it to be Ready by this point in the lifecycle")
		}
		// The nested guard is exercised from inside the environment's own
		// vcluster: its identity-service has ORBIT_DEV_ENVIRONMENT_ID set
		// (charts/orbit/templates/identity/deployment.yaml), which
		// CreateEnvironment refuses unconditionally. Reached over the
		// environment's own AppURL-relative API path, using the SAME caller
		// token — the guard is on the environment's config, not the caller.
		req, err := http.NewRequest(http.MethodPost, env.AppURL+"/api/v1/dev-environments", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatalf("build nested-create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("nested create request: %v", err)
		}
		defer resp.Body.Close()
		var apiErr2 api.APIError
		_ = json.NewDecoder(resp.Body).Decode(&apiErr2)
		if resp.StatusCode != http.StatusForbidden || apiErr2.Code != "devenvironments_nested_forbidden" {
			t.Errorf("expected 403 devenvironments_nested_forbidden from inside the environment, got status=%d code=%s", resp.StatusCode, apiErr2.Code)
		}
	})

	t.Run("SleepLeavesNoHostStrays", func(t *testing.T) {
		t.Log("waiting for the environment to idle-sleep (IdleTimeout) — this sub-test only observes, it does not force an early sleep")
		waitForPhase(t, id, orbitapi.DevEnvPhaseSleeping, 3*time.Hour)

		out, err := kubectl(t, "get", "all,pvc", "-n", hostNamespace(id), "-o", "json")
		if err != nil {
			t.Fatalf("list host namespace resources: %v\n%s", err, out)
		}
		var list struct {
			Items []struct {
				Kind string `json:"kind"`
				Spec struct {
					Replicas *int `json:"replicas"`
				} `json:"spec"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			t.Fatalf("parse resource list: %v", err)
		}
		for _, item := range list.Items {
			switch item.Kind {
			case "PersistentVolumeClaim":
				// expected to remain — PKI and data persist on the PVC (pinned decision 2).
			case "StatefulSet":
				if item.Spec.Replicas == nil || *item.Spec.Replicas != 0 {
					t.Errorf("StatefulSet still has replicas=%v while Sleeping — expected 0", item.Spec.Replicas)
				}
			default:
				t.Errorf("unexpected %s in host namespace %s while Sleeping — sleep should leave only PVCs and a scaled-to-zero StatefulSet", item.Kind, hostNamespace(id))
			}
		}
	})

	t.Run("WakeRestoresReplicas", func(t *testing.T) {
		if _, status, apiErr := client.wake(id); apiErr != nil {
			t.Fatalf("wake environment: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
		}
		waitForPhase(t, id, orbitapi.DevEnvPhaseReady, 90*time.Second)

		out, err := kubectl(t, "get", "statefulsets", "-n", hostNamespace(id), "-o", "json")
		if err != nil {
			t.Fatalf("list statefulsets: %v\n%s", err, out)
		}
		var list struct {
			Items []struct {
				Spec struct {
					Replicas *int `json:"replicas"`
				} `json:"spec"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			t.Fatalf("parse statefulset list: %v", err)
		}
		if len(list.Items) == 0 {
			t.Fatal("no StatefulSets found in host namespace after wake")
		}
		for _, item := range list.Items {
			if item.Spec.Replicas == nil || *item.Spec.Replicas == 0 {
				t.Errorf("StatefulSet replicas=%v after wake — expected > 0", item.Spec.Replicas)
			}
		}
	})

	t.Run("ReconcileCadenceHasNoHotLoop", func(t *testing.T) {
		// Sample the operator's reconcile log lines for this environment
		// twice, several seconds apart, and assert the count doesn't imply
		// a sub-second (or even sub-computed-interval) reconcile loop. This
		// is a coarse signal, not a precise cadence measurement — precise
		// cadence is what the OTel histogram (orbit_devenv_reconcile_*) is
		// for, once Track V's metrics pipeline is live.
		countReconciles := func() int {
			out, _ := kubectl(t, "logs", "-n", operatorNamespace(), "-l", "app=orbit-operator",
				"--since=15s", "--tail=-1")
			return strings.Count(out, id)
		}
		first := countReconciles()
		time.Sleep(15 * time.Second)
		second := countReconciles()
		// An unchanged Ready environment should reconcile at most a small,
		// bounded number of times per 15s window (the computed interval is
		// on the order of minutes) — a hot loop reconciles roughly once per
		// controller-runtime requeue backoff tick (~12s in the historical
		// bug), which would show up as a reconcile line in nearly every
		// window. More than 3 in a single 15s window is treated as a signal
		// worth failing on rather than a hard, precisely-derived threshold.
		if first > 3 || second > 3 {
			t.Errorf("observed %d and %d reconcile log lines for %s in consecutive 15s windows — possible hot loop (expected near-zero for an unchanged Ready environment)", first, second, id)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// 2. Quota
// ═══════════════════════════════════════════════════════════════════════════

func TestDevEnv_QuotaExceeded(t *testing.T) {
	ref := pinnedRef(t)
	token := loadUserToken(t)
	client := newDevenvClient(t, token)

	// maxActivePerOwner defaults to 2 (internal/devenvironments/config/base.yaml).
	// Read it back from the list envelope's limits so this test tracks
	// whatever staging is actually configured with, rather than assuming 2.
	var listResp struct {
		Environments []devenvironments.Environment `json:"environments"`
		Limits       struct {
			MaxActivePerOwner int `json:"maxActivePerOwner"`
		} `json:"limits"`
	}
	status, apiErr := client.do(http.MethodGet, "", nil, &listResp)
	if apiErr != nil {
		t.Fatalf("list environments: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
	}
	quota := listResp.Limits.MaxActivePerOwner
	if quota <= 0 {
		t.Fatalf("unexpected maxActivePerOwner=%d from list envelope", quota)
	}
	existing := len(listResp.Environments)
	need := quota - existing
	if need < 0 {
		t.Skipf("caller already has %d active environments against a quota of %d — cannot test the boundary without destroying pre-existing environments", existing, quota)
	}

	var provisioned []string
	t.Cleanup(func() {
		for _, id := range provisioned {
			client.destroyBestEffort(id)
		}
	})
	for i := 0; i < need; i++ {
		env, status, apiErr := client.create(ref)
		if apiErr != nil {
			t.Fatalf("create environment %d/%d up to quota: status=%d code=%s message=%s", i+1, need, status, apiErr.Code, apiErr.Message)
		}
		provisioned = append(provisioned, env.ID)
	}

	_, status, apiErr = client.create(ref)
	if apiErr == nil {
		t.Fatal("expected quota-exceeding create to fail, but it succeeded")
	}
	if status != http.StatusForbidden || apiErr.Code != "devenvironments_quota_exceeded" {
		t.Errorf("expected 403 devenvironments_quota_exceeded, got status=%d code=%s", status, apiErr.Code)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// 3. Per-route scope pairs
// ═══════════════════════════════════════════════════════════════════════════

// TestDevEnv_RouteScopePairs asserts each devenvironments:{read,write,admin}
// scope gates exactly the routes it should, by temporarily narrowing the
// live test principal's grants through the policy service (mirrors
// tests/integration/policy/authz_test.go's ensureGrant/revokeGrants — same
// live-mutation trade-off: this rewrites the caller's real grants for the
// duration of the test and restores nothing afterward beyond a final
// re-grant of the full scope set, since the pre-test grant set isn't
// recorded).
func TestDevEnv_RouteScopePairs(t *testing.T) {
	token := loadUserToken(t)
	sub := jwtSub(t, token)
	client := newDevenvClient(t, token)

	t.Cleanup(func() {
		revokeAllGrants(t, "HUMAN", sub)
		grant(t, "HUMAN", sub, []string{"devenvironments:read", "devenvironments:write", "devenvironments:admin"})
		waitForGatewayBundleRefresh(t)
	})

	t.Run("ReadOnly_CannotCreate", func(t *testing.T) {
		revokeAllGrants(t, "HUMAN", sub)
		grant(t, "HUMAN", sub, []string{"devenvironments:read"})
		waitForGatewayBundleRefresh(t)

		env, status, apiErr := client.create(pinnedRef(t))
		if apiErr == nil {
			client.destroyBestEffort(env.ID)
			t.Fatal("expected create to be denied with only devenvironments:read")
		}
		if status != http.StatusForbidden {
			t.Errorf("expected 403 with only devenvironments:read, got %d (code=%s)", status, apiErr.Code)
		}
	})

	t.Run("WriteOnly_CannotListAll", func(t *testing.T) {
		revokeAllGrants(t, "HUMAN", sub)
		grant(t, "HUMAN", sub, []string{"devenvironments:read", "devenvironments:write"})
		waitForGatewayBundleRefresh(t)

		status, apiErr := client.listAll()
		if apiErr == nil {
			t.Fatal("expected GET /all to be denied without devenvironments:admin")
		}
		if status != http.StatusForbidden {
			t.Errorf("expected 403 without devenvironments:admin, got %d (code=%s)", status, apiErr.Code)
		}
	})

	t.Run("Admin_CanListAll", func(t *testing.T) {
		revokeAllGrants(t, "HUMAN", sub)
		grant(t, "HUMAN", sub, []string{"devenvironments:read", "devenvironments:write", "devenvironments:admin"})
		waitForGatewayBundleRefresh(t)

		status, apiErr := client.listAll()
		if apiErr != nil {
			t.Errorf("expected GET /all to succeed with devenvironments:admin, got %d (code=%s message=%s)", status, apiErr.Code, apiErr.Message)
		}
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// 4. TTL expiry orphan sweep
// ═══════════════════════════════════════════════════════════════════════════

func TestDevEnv_TTLExpirySweepsEverything(t *testing.T) {
	ref := pinnedRef(t)
	token := loadUserToken(t)
	client := newDevenvClient(t, token)

	env, status, apiErr := client.create(ref)
	if apiErr != nil {
		t.Fatalf("create environment: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
	}
	id := env.ID
	t.Cleanup(func() { client.destroyBestEffort(id) })

	waitForPhase(t, id, orbitapi.DevEnvPhaseReady, 6*time.Minute)

	// Shrink the TTL directly via kubectl: the API's ExtendEnvironment only
	// ever raises spec.ttl (ADR decision — extension is one-directional), so
	// there is no product path to force a fast expiry. Direct CR mutation is
	// consistent with this suite's other cluster-admin-level checks
	// (operator-kill-mid-provision below).
	if out, err := kubectl(t, "patch", "orbitdevenvironment", id, "--type=merge", "-p", `{"spec":{"ttl":"45s"}}`); err != nil {
		t.Fatalf("patch ttl: %v\n%s", err, out)
	}

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := kubectl(t, "get", "orbitdevenvironment", id); err != nil {
			break // CR gone — expiry deleted it.
		}
		time.Sleep(5 * time.Second)
	}
	if out, err := kubectl(t, "get", "orbitdevenvironment", id); err == nil {
		t.Fatalf("orbitdevenvironment/%s still exists after its shortened TTL elapsed:\n%s", id, out)
	}

	// Assert the sweep is exhaustive, not just "the CR is gone" — every
	// resource kind the reconciler creates for an environment must also be
	// gone, host-side and cluster-scoped alike.
	out, err := kubectl(t, "get", "all,pvc,clusterspiffeids,virtualservices,serviceentries", "-A",
		"-o", "jsonpath={range .items[*]}{.metadata.namespace}{\"/\"}{.metadata.name}{\"\\n\"}{end}")
	if err != nil {
		t.Fatalf("cluster-wide resource sweep: %v\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, id) {
			t.Errorf("found orphaned resource referencing expired environment %s: %s", id, line)
		}
	}
	if _, err := kubectl(t, "get", "namespace", hostNamespace(id)); err == nil {
		t.Errorf("host namespace %s still exists after TTL expiry", hostNamespace(id))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// 5. Operator-kill-mid-provision convergence
// ═══════════════════════════════════════════════════════════════════════════

func TestDevEnv_OperatorKillMidProvisionConverges(t *testing.T) {
	ref := pinnedRef(t)
	token := loadUserToken(t)
	client := newDevenvClient(t, token)

	env, status, apiErr := client.create(ref)
	if apiErr != nil {
		t.Fatalf("create environment: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
	}
	id := env.ID
	t.Cleanup(func() { client.destroyBestEffort(id) })

	// Give provisioning a moment to actually start (namespace/vcluster
	// Helm install in flight) before killing the operator mid-flight —
	// killing it before any reconcile has run would just test a cold start,
	// not resumption from a partially-applied state.
	time.Sleep(30 * time.Second)

	out, err := kubectl(t, "delete", "pod", "-n", operatorNamespace(), "-l", "app=orbit-operator", "--wait=false")
	if err != nil {
		t.Fatalf("delete operator pod: %v\n%s", err, out)
	}
	t.Log("deleted operator pod mid-provision; waiting for the replacement to pick reconciliation back up")

	waitForPhase(t, id, orbitapi.DevEnvPhaseReady, 8*time.Minute)
}

// ═══════════════════════════════════════════════════════════════════════════
// 6. Main app never hijacked by dev-environment routes
// ═══════════════════════════════════════════════════════════════════════════

// TestDevEnv_MainAppNeverHijacked is the backend half of pinned decision 14's
// regression coverage (the Go-level route-ordering unit test lives in
// internal/operator/devenvironment_app_ingress_test.go). This test exercises
// the LIVE gateway: with at least one dev environment provisioned, an
// unprefixed request to the shared app host must still reach the main
// Orbit app, never a dev environment's route.
func TestDevEnv_MainAppNeverHijacked(t *testing.T) {
	appURL := os.Getenv("ORBIT_INTEGRATION_APP_URL")
	if appURL == "" {
		t.Skip("ORBIT_INTEGRATION_APP_URL not set — skipping live gateway hijack check")
	}

	ref := pinnedRef(t)
	token := loadUserToken(t)
	client := newDevenvClient(t, token)

	env, status, apiErr := client.create(ref)
	if apiErr != nil {
		t.Fatalf("create environment: status=%d code=%s message=%s", status, apiErr.Code, apiErr.Message)
	}
	t.Cleanup(func() { client.destroyBestEffort(env.ID) })
	waitForPhase(t, env.ID, orbitapi.DevEnvPhaseReady, 6*time.Minute)

	req, err := http.NewRequest(http.MethodGet, appURL+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// No routing cookie is ever sent (the fallback was deleted — pinned
	// decision 14) and no X-Forwarded-Prefix is set by a real browser
	// request to the bare host, so this simply proves the aggregated
	// VirtualService's ordering still falls through to the main app.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s/: %v", appURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Errorf("unprefixed request to the shared app host returned %d — expected the main Orbit app to serve it", resp.StatusCode)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// 7. Operator startup logsink assertion
// ═══════════════════════════════════════════════════════════════════════════

// TestDevEnv_OperatorStartupLogSinkNotDiscarded guards against the
// historical logr.Discard() bug (cmd/operator/logsink.go) resurfacing: it
// asserts the operator's own startup log line is observable through
// `kubectl logs`, proving controller-runtime output is flowing through the
// injected logger rather than being silently dropped.
func TestDevEnv_OperatorStartupLogSinkNotDiscarded(t *testing.T) {
	out, err := kubectl(t, "logs", "-n", operatorNamespace(), "-l", "app=orbit-operator", "--tail=500")
	if err != nil {
		t.Fatalf("read operator logs: %v\n%s", err, out)
	}
	if !strings.Contains(out, "orbit-operator starting") {
		t.Error(`operator logs do not contain the "orbit-operator starting" startup line — controller-runtime logging may be discarded again (cmd/operator/logsink.go)`)
	}
	if !strings.Contains(out, "starting controller-runtime manager") {
		t.Error(`operator logs do not contain "starting controller-runtime manager"`)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Grant management (mirrors tests/integration/policy/authz_test.go)
// ═══════════════════════════════════════════════════════════════════════════

func grant(t *testing.T, principalType, principalID string, scopes []string) {
	t.Helper()
	token := loadUserToken(t)
	body := map[string]any{
		"principal_type": principalType,
		"principal_id":   principalID,
		"scopes":         scopes,
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, policyServiceURL()+"/api/v1/policy/grants", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("build grant request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("create grant: unexpected status %d", resp.StatusCode)
	}
}

func revokeAllGrants(t *testing.T, principalType, principalID string) {
	t.Helper()
	token := loadUserToken(t)
	listURL := fmt.Sprintf("%s/api/v1/policy/grants?principal_type=%s&principal_id=%s",
		policyServiceURL(), principalType, principalID)
	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		t.Logf("cleanup: build list request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("cleanup: list grants: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
		delReq, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/policy/grants/%s", policyServiceURL(), g.ID), nil)
		if err != nil {
			continue
		}
		delReq.Header.Set("Authorization", "Bearer "+token)
		if resp, err := http.DefaultClient.Do(delReq); err == nil {
			resp.Body.Close()
		}
	}
}

// waitForGatewayBundleRefresh waits long enough for the gateway/service OPA
// sidecars to pick up a grant change (min=30s/max=60s poll — mirrors
// authz_test.go's identical helper).
func waitForGatewayBundleRefresh(t *testing.T) {
	t.Helper()
	t.Log("waiting 65s for OPA bundle refresh...")
	time.Sleep(65 * time.Second)
}

// ═══════════════════════════════════════════════════════════════════════════
// Kubeconfig helpers
// ═══════════════════════════════════════════════════════════════════════════

// retargetKubeconfigServer rewrites every cluster entry's server: field to
// hostServer, keeping the client certificate/key (and thus the vcluster
// client-CA-signed identity) intact — this is what proves the host API
// server's own CA trust rejects a vcluster-scoped client certificate.
// Uses client-go's clientcmd (already a direct dependency) rather than a
// raw YAML library, per this repo's no-raw-yaml convention.
func retargetKubeconfigServer(kubeconfigYAML, hostServer string) (string, error) {
	cfg, err := clientcmd.Load([]byte(kubeconfigYAML))
	if err != nil {
		return "", fmt.Errorf("load kubeconfig: %w", err)
	}
	for _, cluster := range cfg.Clusters {
		cluster.Server = hostServer
	}
	out, err := clientcmd.Write(*cfg)
	if err != nil {
		return "", fmt.Errorf("write kubeconfig: %w", err)
	}
	return string(out), nil
}

// decodeJWTSegment base64url-decodes a JWT segment, tolerating both padded
// and unpadded encodings.
func decodeJWTSegment(seg string) ([]byte, error) {
	if m := len(seg) % 4; m != 0 {
		seg += strings.Repeat("=", 4-m)
	}
	return base64.URLEncoding.DecodeString(seg)
}
