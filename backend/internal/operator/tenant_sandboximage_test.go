package operator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

const bundledTestNS = "orbit-tenant-example"

func bundledTenant() *KoraTenant {
	return &KoraTenant{ObjectMeta: metav1.ObjectMeta{Name: "example"}}
}

// newBundledReconciler returns a tenant reconciler over a fake client holding objs.
func newBundledReconciler(logs io.Writer, bundled map[string]BundledSandboxImage, objs ...client.Object) (*TenantReconciler, client.Client) {
	return newBundledReconcilerWith(fake.NewClientBuilder().WithScheme(NewScheme()).WithObjects(objs...).Build(), logs, bundled)
}

func newBundledReconcilerWith(c client.Client, logs io.Writer, bundled map[string]BundledSandboxImage) (*TenantReconciler, client.Client) {
	return &TenantReconciler{Client: c, Logger: logging.NewLoggerWithOutput(logs), SandboxBundled: bundled}, c
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
	r, c := newBundledReconciler(io.Discard, map[string]BundledSandboxImage{
		"go":     {Enabled: true, Warm: true, Image: "ghcr.io/runkora/kora-sandbox-go:v1"},
		"python": {Enabled: true, Image: "ghcr.io/runkora/kora-sandbox-python:v1"},
		"node":   {Enabled: false, Image: "ghcr.io/runkora/kora-sandbox-node:v1"},
	})
	if err := r.ensureBundledSandboxImages(context.Background(), bundledTenant()); err != nil {
		t.Fatalf("ensureBundledSandboxImages: %v", err)
	}

	img, ok := getSandboxImage(t, c, "kora-go")
	if !ok {
		t.Fatal("kora-go was not created")
	}
	if img.Spec.Image != "ghcr.io/runkora/kora-sandbox-go:v1" || !img.Spec.Warm || img.Spec.Description == "" {
		t.Errorf("kora-go spec = %+v", img.Spec)
	}
	if !ownsBundledSandboxImage(img) || img.Labels["runkora.io/tenant"] != "example" {
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
		return bundledSandboxImage(bundledTestNS, "example", si.BundledImage{Name: name}, BundledSandboxImage{Image: image}, "Acme")
	}
	r, c := newBundledReconciler(io.Discard, map[string]BundledSandboxImage{
		"go": {Enabled: true, Warm: true, Image: "ghcr.io/runkora/kora-sandbox-go:v2"},
	},
		owned("kora-go", "ghcr.io/runkora/kora-sandbox-go:v1"),
		owned("kora-node", "ghcr.io/runkora/kora-sandbox-node:v1"),
	)
	if err := r.ensureBundledSandboxImages(context.Background(), bundledTenant()); err != nil {
		t.Fatalf("ensureBundledSandboxImages: %v", err)
	}

	if img, _ := getSandboxImage(t, c, "kora-go"); img == nil || img.Spec.Image != "ghcr.io/runkora/kora-sandbox-go:v2" || !img.Spec.Warm {
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
	var logs bytes.Buffer
	r, c := newBundledReconciler(&logs, map[string]BundledSandboxImage{
		"go":   {Enabled: true, Image: "ghcr.io/runkora/kora-sandbox-go:v1"},
		"node": {Enabled: false, Image: "ghcr.io/runkora/kora-sandbox-node:v1"},
	},
		tenantImage("kora-go"),
		tenantImage("kora-node"),
	)
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

// A failure on one bundled image still registers the others and is returned.
func TestEnsureBundledSandboxImages_ContinuesPastAFailure(t *testing.T) {
	errCreate := errors.New("create refused")
	c := fake.NewClientBuilder().WithScheme(NewScheme()).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetName() == "kora-go" {
				return errCreate
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	r, _ := newBundledReconcilerWith(c, io.Discard, map[string]BundledSandboxImage{
		"go":     {Enabled: true, Image: "ghcr.io/runkora/kora-sandbox-go:v1"},
		"python": {Enabled: true, Image: "ghcr.io/runkora/kora-sandbox-python:v1"},
		"node":   {Enabled: true, Image: "ghcr.io/runkora/kora-sandbox-node:v1"},
	})

	err := r.ensureBundledSandboxImages(context.Background(), bundledTenant())
	if !errors.Is(err, errCreate) {
		t.Fatalf("err = %v, want the kora-go create error", err)
	}
	for _, name := range []string{"kora-python", "kora-node"} {
		if _, ok := getSandboxImage(t, c, name); !ok {
			t.Errorf("%s was not created after kora-go failed", name)
		}
	}
}
