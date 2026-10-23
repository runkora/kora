package agents

import (
	"context"
	"io"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func authPolicyProvisioner(t *testing.T, cfg K8sProvisionerConfig) (*K8sProvisioner, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		authorizationPolicyGVR: "AuthorizationPolicyList",
	})
	return &K8sProvisioner{
		dynamicClient: dc,
		logger:        logging.NewLoggerWithOutput(io.Discard),
		cfg:           cfg,
	}, dc
}

func TestEnsureAuthorizationPolicy_DisabledCreatesNothing(t *testing.T) {
	p, dc := authPolicyProvisioner(t, K8sProvisionerConfig{
		TrustDomain:                "orbit.test",
		AuthorizationPolicyEnabled: false,
	})

	if err := p.ensureAuthorizationPolicy(context.Background(), AgentType{Slug: "coder"}, "kora-tenant-acme", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureAuthorizationPolicy: %v", err)
	}

	list, err := dc.Resource(authorizationPolicyGVR).Namespace("kora-tenant-acme").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("created %d policies with the gate off, want 0", len(list.Items))
	}
}

func TestEnsureAuthorizationPolicy_RestrictsPortToKoraAgents(t *testing.T) {
	p, dc := authPolicyProvisioner(t, K8sProvisionerConfig{
		TrustDomain:                "orbit.test",
		AuthorizationPolicyEnabled: true,
	})

	const ns = "kora-tenant-acme"
	if err := p.ensureAuthorizationPolicy(context.Background(), AgentType{Slug: "coder"}, ns, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("ensureAuthorizationPolicy: %v", err)
	}

	// Lives in the TENANT namespace: an AuthorizationPolicy selects workloads
	// in its own namespace, and that is where agent pods run.
	got, err := dc.Resource(authorizationPolicyGVR).Namespace(ns).Get(context.Background(), "agent-coder-callers", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get policy in %s: %v", ns, err)
	}

	if action, _, _ := unstructured.NestedString(got.Object, "spec", "action"); action != "ALLOW" {
		t.Errorf("action = %q, want ALLOW (a DENY list must be kept complete forever)", action)
	}
	if app, _, _ := unstructured.NestedString(got.Object, "spec", "selector", "matchLabels", "app"); app != "coder" {
		t.Errorf("selector app = %q, want coder", app)
	}

	rules, _, err := unstructured.NestedSlice(got.Object, "spec", "rules")
	if err != nil || len(rules) != 2 {
		t.Fatalf("rules = %v (err %v), want 2", rules, err)
	}

	// Rule 1: only kora-agents, only the session-manager port.
	caller, _ := rules[0].(map[string]any)
	principals := nestedStringsAtIndex(t, caller, "from", 0, "source", "principals")
	if len(principals) != 1 || principals[0] != "orbit.test/system/kora-agents" {
		t.Errorf("principals = %v, want [orbit.test/system/kora-agents]", principals)
	}
	// Istio prepends spiffe:// itself; writing it produces spiffe://spiffe://
	// and never matches.
	for _, pr := range principals {
		if len(pr) >= 9 && pr[:9] == "spiffe://" {
			t.Errorf("principal %q carries the spiffe:// prefix; Istio adds it and the rule will never match", pr)
		}
	}
	ports := nestedStringsAtIndex(t, caller, "to", 0, "operation", "ports")
	if len(ports) != 1 || ports[0] != "8090" {
		t.Errorf("ports = %v, want [8090]", ports)
	}

	// Rule 2: /health stays reachable so the kubelet readiness probe passes
	// regardless of whether Istio's probe rewrite is in play.
	probe, _ := rules[1].(map[string]any)
	if _, found, _ := unstructured.NestedSlice(probe, "from"); found {
		t.Error("the health rule must have no source restriction; the kubelet has no SPIFFE identity")
	}
	paths := nestedStringsAtIndex(t, probe, "to", 0, "operation", "paths")
	if len(paths) != 1 || paths[0] != "/health" {
		t.Errorf("health paths = %v, want [/health]", paths)
	}
}

func TestEnsureAuthorizationPolicy_IsIdempotent(t *testing.T) {
	p, _ := authPolicyProvisioner(t, K8sProvisionerConfig{
		TrustDomain:                "orbit.test",
		AuthorizationPolicyEnabled: true,
	})

	ctx := context.Background()
	at := AgentType{Slug: "coder"}
	if err := p.ensureAuthorizationPolicy(ctx, at, "kora-tenant-acme", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	// Provisioning re-runs at every kora-agents startup; an AlreadyExists must
	// not fail the reconcile.
	if err := p.ensureAuthorizationPolicy(ctx, at, "kora-tenant-acme", metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestDeleteAuthorizationPolicy_MissingIsNotAnError(t *testing.T) {
	p, _ := authPolicyProvisioner(t, K8sProvisionerConfig{
		TrustDomain:                "orbit.test",
		AuthorizationPolicyEnabled: true,
	})

	// Deprovision runs against agent types provisioned before this policy
	// existed, and against a disabled gate.
	if err := p.deleteAuthorizationPolicy(context.Background(), "coder", "kora-tenant-acme"); err != nil {
		t.Fatalf("delete missing policy: %v", err)
	}
}

// nestedStringsAtIndex reads obj[listKey][index][path...] as a string slice.
// unstructured.Nested* cannot index into a slice, and these rules are lists.
func nestedStringsAtIndex(t *testing.T, obj map[string]any, listKey string, index int, path ...string) []string {
	t.Helper()
	list, found, err := unstructured.NestedSlice(obj, listKey)
	if err != nil || !found || len(list) <= index {
		t.Fatalf("%s[%d] missing (found %v, err %v)", listKey, index, found, err)
	}
	entry, ok := list[index].(map[string]any)
	if !ok {
		t.Fatalf("%s[%d] is not an object", listKey, index)
	}
	out, _, err := unstructured.NestedStringSlice(entry, path...)
	if err != nil {
		t.Fatalf("read %v from %s[%d]: %v", path, listKey, index, err)
	}
	return out
}
