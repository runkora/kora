package operator

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
)

const bundledTestNS = "orbit-tenant-example"

func bundledTenant() *OrbitTenant {
	return &OrbitTenant{ObjectMeta: metav1.ObjectMeta{Name: "example"}}
}

func getSandboxImage(t *testing.T, c client.Client, name string) (*si.SandboxImage, bool) {
	t.Helper()
	img := &si.SandboxImage{}
	err := c.Get(context.Background(), client.ObjectKey{Namespace: bundledTestNS, Name: name}, img)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return img, true
}

func TestEnsureBundledSandboxImages_CreatesEnabledAndSkipsDisabled(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	r := &TenantReconciler{
		Client: c,
		Logger: logging.NewLoggerWithOutput(io.Discard),
		SandboxBundled: map[string]BundledSandboxImage{
			"go":     {Enabled: true, Warm: true, Image: "ghcr.io/clear-route/kora-sandbox-go:v1"},
			"python": {Enabled: true, Image: "ghcr.io/clear-route/kora-sandbox-python:v1"},
			"node":   {Enabled: false, Image: "ghcr.io/clear-route/kora-sandbox-node:v1"},
		},
	}
	if err := r.ensureBundledSandboxImages(context.Background(), bundledTenant()); err != nil {
		t.Fatalf("ensureBundledSandboxImages: %v", err)
	}

	img, ok := getSandboxImage(t, c, "kora-go")
	if !ok {
		t.Fatal("kora-go was not created")
	}
	if img.Spec.Image != "ghcr.io/clear-route/kora-sandbox-go:v1" || !img.Spec.Warm || img.Spec.Description == "" {
		t.Errorf("kora-go spec = %+v", img.Spec)
	}
	if !ownsBundledSandboxImage(img) || img.Labels["orbit.io/tenant"] != "example" {
		t.Errorf("kora-go labels = %v", img.Labels)
	}
	if img, _ := getSandboxImage(t, c, "kora-python"); img == nil || img.Spec.Warm {
		t.Errorf("kora-python = %+v, want created with warm false", img)
	}
	if _, ok := getSandboxImage(t, c, "kora-node"); ok {
		t.Error("kora-node was created while disabled")
	}
}

func TestEnsureBundledSandboxImages_DeletesDisabledAndPatchesEnabled(t *testing.T) {
	owned := func(name, image string) *si.SandboxImage {
		return bundledSandboxImage(bundledTestNS, "example", si.BundledImage{Name: name}, BundledSandboxImage{Image: image})
	}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(
		owned("kora-go", "ghcr.io/clear-route/kora-sandbox-go:v1"),
		owned("kora-node", "ghcr.io/clear-route/kora-sandbox-node:v1"),
	).Build()
	r := &TenantReconciler{
		Client: c,
		Logger: logging.NewLoggerWithOutput(io.Discard),
		SandboxBundled: map[string]BundledSandboxImage{
			"go": {Enabled: true, Warm: true, Image: "ghcr.io/clear-route/kora-sandbox-go:v2"},
		},
	}
	if err := r.ensureBundledSandboxImages(context.Background(), bundledTenant()); err != nil {
		t.Fatalf("ensureBundledSandboxImages: %v", err)
	}

	if img, _ := getSandboxImage(t, c, "kora-go"); img == nil || img.Spec.Image != "ghcr.io/clear-route/kora-sandbox-go:v2" || !img.Spec.Warm {
		t.Errorf("kora-go = %+v, want patched to v2 and warm", img)
	}
	// A key missing from the config counts as disabled.
	if _, ok := getSandboxImage(t, c, "kora-node"); ok {
		t.Error("kora-node survived while disabled")
	}
}

// A tenant that registered its own image under a bundled name keeps it,
// whether the bundled image is enabled or disabled.
func TestEnsureBundledSandboxImages_LeavesTenantImageAlone(t *testing.T) {
	tenantImage := func(name string) *si.SandboxImage {
		return &si.SandboxImage{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: bundledTestNS,
				// managed-by alone does not mark a bundled image.
				Labels: map[string]string{"app.kubernetes.io/managed-by": "orbit-operator"},
			},
			Spec: si.Spec{Image: "registry.example.com/team/" + name + ":1"},
		}
	}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(
		tenantImage("kora-go"),
		tenantImage("kora-node"),
	).Build()
	var logs bytes.Buffer
	r := &TenantReconciler{
		Client: c,
		Logger: logging.NewLoggerWithOutput(&logs),
		SandboxBundled: map[string]BundledSandboxImage{
			"go":   {Enabled: true, Image: "ghcr.io/clear-route/kora-sandbox-go:v1"},
			"node": {Enabled: false, Image: "ghcr.io/clear-route/kora-sandbox-node:v1"},
		},
	}
	if err := r.ensureBundledSandboxImages(context.Background(), bundledTenant()); err != nil {
		t.Fatalf("ensureBundledSandboxImages: %v", err)
	}

	for _, name := range []string{"kora-go", "kora-node"} {
		img, ok := getSandboxImage(t, c, name)
		if !ok || img.Spec.Image != "registry.example.com/team/"+name+":1" {
			t.Errorf("%s = %+v, want the tenant's image unchanged", name, img)
		}
	}
	out := logs.String()
	if !strings.Contains(out, "bundled image not registered") || !strings.Contains(out, "kora-go") {
		t.Errorf("log = %q, want the skipped kora-go logged", out)
	}
	if strings.Contains(out, "kora-node") {
		t.Errorf("log = %q, a disabled bundled image should not be logged", out)
	}
}
