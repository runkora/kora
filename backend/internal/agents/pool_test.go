package agents

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	for k, v := range poolPodLabels("acme") {
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

// The pool is created with its ServiceAccount, and an update replaces its
// pod template and replicas.
func TestEnsureTenantPool(t *testing.T) {
	const ns = "kora-tenant-acme"
	p := podSecurityProvisioner()
	p.cfg.PoolSize = 2
	p.client = fake.NewSimpleClientset()
	ctx := context.Background()
	if err := p.ensureTenantPool(ctx, "acme", ns, metav1.OwnerReference{}, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := p.client.CoreV1().ServiceAccounts(ns).Get(ctx, poolName, metav1.GetOptions{}); err != nil {
		t.Errorf("pool service account: %v", err)
	}
	p.cfg.PoolSize = 4
	p.cfg.SessionManagerImage = "ghcr.io/runkora/kora-session-manager:2.0.0"
	if err := p.ensureTenantPool(ctx, "acme", ns, metav1.OwnerReference{}, true); err != nil {
		t.Fatalf("update: %v", err)
	}
	dep, err := p.client.AppsV1().Deployments(ns).Get(ctx, poolName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != 4 {
		t.Errorf("replicas = %d, want 4", *dep.Spec.Replicas)
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == sessionManagerContainerName && c.Image != p.cfg.SessionManagerImage {
			t.Errorf("image = %q, want %q", c.Image, p.cfg.SessionManagerImage)
		}
	}
}
