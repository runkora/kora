package agents

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// A pool pod is the agent pod with pool labels, the pool ServiceAccount and a
// workspace claim it owns (docs/adr/2026-11-22-agent-cold-tier.md).
func TestBuildTenantPool(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.PoolSize = 3
	dep, err := p.buildTenantPool("acme", "kora-tenant-acme")
	if err != nil {
		t.Fatal(err)
	}
	if dep.Name != poolName || dep.Namespace != "kora-tenant-acme" || *dep.Spec.Replicas != 3 {
		t.Errorf("deployment = %s/%s with %d replicas", dep.Namespace, dep.Name, *dep.Spec.Replicas)
	}
	if sel := dep.Spec.Selector.MatchLabels; len(sel) != 1 || sel[poolLabel] != "true" {
		t.Errorf("selector = %v, want only %s", sel, poolLabel)
	}
	labels := dep.Spec.Template.Labels
	for k, v := range map[string]string{
		poolLabel: "true", "runkora.io/tenant": "acme", "runkora.io/managed-agent": "true",
		"spiffe.io/spire-managed-identity": "true", "runkora.io/identity-proxy": "enabled",
	} {
		if labels[k] != v {
			t.Errorf("pod label %s = %q, want %q", k, labels[k], v)
		}
	}
	for _, k := range []string{"spiffe.io/agent", "app", "runkora.io/agent-type"} {
		if _, ok := labels[k]; ok {
			t.Errorf("pool pod carries the agent label %s", k)
		}
	}
	spec := dep.Spec.Template.Spec
	if spec.ServiceAccountName != poolName {
		t.Errorf("service account = %q, want %q", spec.ServiceAccountName, poolName)
	}
	var workspace *corev1.Volume
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == "workspaces" {
			workspace = &spec.Volumes[i]
		}
	}
	if workspace == nil || workspace.Ephemeral == nil || workspace.Ephemeral.VolumeClaimTemplate == nil {
		t.Fatalf("workspaces volume = %+v, want a generic ephemeral volume", workspace)
	}
	if got := workspace.Ephemeral.VolumeClaimTemplate.Spec.Resources.Requests.Storage().String(); got != "10Gi" {
		t.Errorf("workspace size = %s, want 10Gi", got)
	}
	var image string
	for _, c := range spec.Containers {
		if c.Name == sessionManagerContainerName {
			image = c.Image
		}
	}
	if image != p.cfg.SessionManagerImage {
		t.Errorf("session-manager image = %q, want %q", image, p.cfg.SessionManagerImage)
	}
}

// A StatefulSet patch for the tenant reaches the pool, and its pod labels stay
// next to the pool labels.
func TestBuildTenantPool_TenantPatch(t *testing.T) {
	p := podSecurityProvisioner()
	p.cfg.Patches = loadTestPatches(t, map[string]string{
		"10-sts.yaml": `target: {kind: StatefulSet, labelSelector: "runkora.io/tenant=acme"}
patch: |
  apiVersion: apps/v1
  kind: StatefulSet
  metadata: {name: any}
  spec:
    template:
      metadata:
        labels: {team: agents}
      spec:
        tolerations: [{key: dedicated, operator: Exists}]
`,
	})
	dep, err := p.buildTenantPool("acme", "kora-tenant-acme")
	if err != nil {
		t.Fatal(err)
	}
	if dep.Spec.Template.Labels["team"] != "agents" || dep.Spec.Template.Labels[poolLabel] != "true" {
		t.Errorf("pod labels = %v, want the patch label and the pool label", dep.Spec.Template.Labels)
	}
	if len(dep.Spec.Template.Spec.Tolerations) != 1 {
		t.Errorf("tolerations = %v, want the patch toleration", dep.Spec.Template.Spec.Tolerations)
	}
	other, err := p.buildTenantPool("globex", "kora-tenant-globex")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Spec.Template.Spec.Tolerations) != 0 {
		t.Errorf("another tenant's pool got the patch")
	}
}

// The pool's AuthorizationPolicy admits only kora-agents to pool pods, the pool
// carries the owner reference, an apply with update false leaves an existing pool
// alone, and an unchanged render makes no further API call.
func TestEnsureTenantPool(t *testing.T) {
	const ns = "kora-tenant-acme"
	p := podSecurityProvisioner()
	p.cfg.PoolSize = 2
	p.cfg.AuthorizationPolicyEnabled = true
	cs := fake.NewSimpleClientset()
	p.client = cs
	p.dynamicClient = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	owner := metav1.OwnerReference{APIVersion: "runkora.io/v1alpha1", Kind: "KoraTenant", Name: "acme", UID: "u1"}
	ctx := context.Background()
	if err := p.ensureTenantPool(ctx, "acme", ns, owner, true); err != nil {
		t.Fatal(err)
	}
	ap, err := p.dynamicClient.Resource(authorizationPolicyGVR).Namespace(ns).Get(ctx, agentAuthorizationPolicyName(poolName), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pool authorizationpolicy: %v", err)
	}
	if sel, _, _ := unstructured.NestedStringMap(ap.Object, "spec", "selector", "matchLabels"); len(sel) != 1 || sel[poolLabel] != "true" {
		t.Errorf("policy selector = %v, want the pool label", sel)
	}
	dep, err := cs.AppsV1().Deployments(ns).Get(ctx, poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dep.OwnerReferences) != 1 || dep.OwnerReferences[0].Kind != "KoraTenant" {
		t.Errorf("owner references = %v", dep.OwnerReferences)
	}
	if _, err := cs.CoreV1().ServiceAccounts(ns).Get(ctx, poolName, metav1.GetOptions{}); err != nil {
		t.Errorf("pool service account: %v", err)
	}

	// A deleted pool policy comes back on the next apply, whatever the render.
	if err := p.dynamicClient.Resource(authorizationPolicyGVR).Namespace(ns).Delete(ctx, agentAuthorizationPolicyName(poolName), metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := p.ensureTenantPool(ctx, "acme", ns, owner, true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.dynamicClient.Resource(authorizationPolicyGVR).Namespace(ns).Get(ctx, agentAuthorizationPolicyName(poolName), metav1.GetOptions{}); err != nil {
		t.Errorf("pool policy after a delete and an apply: %v", err)
	}

	calls := len(cs.Actions())
	if err := p.ensureTenantPool(ctx, "acme", ns, owner, true); err != nil {
		t.Fatal(err)
	}
	if n := len(cs.Actions()); n != calls {
		t.Errorf("an unchanged render made %d Deployment or ServiceAccount calls", n-calls)
	}

	p.cfg.PoolSize = 5
	if err := p.ensureTenantPool(ctx, "acme", ns, owner, false); err != nil {
		t.Fatal(err)
	}
	if dep, _ = cs.AppsV1().Deployments(ns).Get(ctx, poolName, metav1.GetOptions{}); *dep.Spec.Replicas != 2 {
		t.Errorf("replicas after an apply with update false = %d, want 2", *dep.Spec.Replicas)
	}
	// The pool left alone is not remembered, and the next update writes it.
	p.cfg.SessionManagerImage = "ghcr.io/runkora/kora-session-manager:2.0.0"
	if err := p.ensureTenantPool(ctx, "acme", ns, owner, true); err != nil {
		t.Fatal(err)
	}
	if dep, _ = cs.AppsV1().Deployments(ns).Get(ctx, poolName, metav1.GetOptions{}); *dep.Spec.Replicas != 5 {
		t.Errorf("replicas after the next update = %d, want 5", *dep.Spec.Replicas)
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == sessionManagerContainerName && c.Image != p.cfg.SessionManagerImage {
			t.Errorf("image = %q, want %q", c.Image, p.cfg.SessionManagerImage)
		}
	}
}

func TestValidateAgentSlug_ReservesThePoolName(t *testing.T) {
	if err := validateAgentSlug(poolName); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("validateAgentSlug(%q) = %v, want ErrInvalidSlug", poolName, err)
	}
}
