package operator

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

const testNS = "orbit-tenant-acme"

// fakeRegistry answers Read from a map and records the auth it was given.
type fakeRegistry struct {
	configs map[string]imageConfig
	err     error
	auth    []dockerConfig
	// deadline records the read's context deadline.
	deadline    time.Time
	hasDeadline bool
}

func (f *fakeRegistry) Read(ctx context.Context, ref string, auth []dockerConfig) (imageConfig, error) {
	f.auth = auth
	f.deadline, f.hasDeadline = ctx.Deadline()
	if f.err != nil {
		return imageConfig{}, f.err
	}
	c, ok := f.configs[ref]
	if !ok {
		return imageConfig{}, errors.New("manifest unknown")
	}
	return c, nil
}

var goodConfig = imageConfig{Digest: "sha256:good", User: "100:100", Labels: map[string]string{si.BaseLabel: "v1.0.0"}}

func sandboxImage(name, image string, warm bool) *si.SandboxImage {
	return &si.SandboxImage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNS, Generation: 1, UID: types.UID(name + "-uid")},
		Spec:       si.Spec{Image: image, Warm: warm},
	}
}

func newImageReconciler(reg registryReader, objs ...client.Object) (*SandboxImageReconciler, client.Client) {
	tenantNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNS, Labels: map[string]string{nsLabelRole: "tenant"}}}
	objs = append(objs, tenantNS)
	c := fake.NewClientBuilder().
		WithScheme(NewScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&si.SandboxImage{}).
		Build()
	return &SandboxImageReconciler{
		Client:      c,
		Scheme:      NewScheme(),
		Registry:    reg,
		Logger:      logging.NewLoggerWithOutput(io.Discard),
		PullSecrets: []string{"ghcr-pull"},
		Placement:   WarmerPlacement{NodeSelector: map[string]string{"pool": "sandbox"}},
		Resync:      time.Minute,
	}, c
}

func reconcileImage(t *testing.T, r *SandboxImageReconciler, c client.Client, name string) (*si.SandboxImage, ctrl.Result) {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNS, Name: name}})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := &si.SandboxImage{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: name}, got); err != nil {
		t.Fatal(err)
	}
	return got, res
}

func condition(img *si.SandboxImage, typ string) metav1.Condition {
	if c := meta.FindStatusCondition(img.Status.Conditions, typ); c != nil {
		return *c
	}
	return metav1.Condition{}
}

func warmer(t *testing.T, c client.Client, name string) (*appsv1.DaemonSet, bool) {
	t.Helper()
	ds := &appsv1.DaemonSet{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: "sandbox-image-" + name}, ds)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return ds, true
}

func TestSandboxImage_ValidAndWarm(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", true))

	img, res := reconcileImage(t, r, c, "go")
	if !img.Valid() || img.Status.Digest != "sha256:good" {
		t.Fatalf("status = %+v, want valid with the resolved digest", img.Status)
	}
	if res.RequeueAfter != time.Minute {
		t.Errorf("RequeueAfter = %v, want the resync interval", res.RequeueAfter)
	}
	if !reg.hasDeadline || time.Until(reg.deadline) > registryReadTimeout {
		t.Errorf("registry read deadline = %v (set %v), want within %v", reg.deadline, reg.hasDeadline, registryReadTimeout)
	}
	if w := condition(img, si.ConditionWarm); w.Status != metav1.ConditionFalse || w.Reason != reasonWarmPulling {
		t.Errorf("Warm = %+v, want Pulling before the DaemonSet is ready", w)
	}

	ds, ok := warmer(t, c, "go")
	if !ok {
		t.Fatal("no warmer DaemonSet for a warm image")
	}
	pod := ds.Spec.Template.Spec
	ctr := pod.Containers[0]
	if ctr.Image != "ghcr.io/acme/go@sha256:good" || ctr.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("warmer pulls %s with %s, want the pinned digest with IfNotPresent", ctr.Image, ctr.ImagePullPolicy)
	}
	if *pod.SecurityContext.RunAsUser != si.SandboxUID || *ctr.SecurityContext.RunAsUser != si.SandboxUID {
		t.Errorf("warmer runs as %d/%d, want the sandbox uid %d", *pod.SecurityContext.RunAsUser, *ctr.SecurityContext.RunAsUser, si.SandboxUID)
	}
	if pod.NodeSelector["pool"] != "sandbox" {
		t.Errorf("warmer nodeSelector = %v, want the sandbox placement", pod.NodeSelector)
	}
	if len(ds.OwnerReferences) != 1 || ds.OwnerReferences[0].Name != "go" {
		t.Errorf("owner references = %+v, want the SandboxImage", ds.OwnerReferences)
	}
	if len(pod.ImagePullSecrets) != 0 {
		t.Errorf("pull secrets = %v, want none: the pod webhook adds the install's", pod.ImagePullSecrets)
	}

	ds.Status = appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, ObservedGeneration: ds.Generation}
	if err := c.Status().Update(context.Background(), ds); err != nil {
		t.Fatal(err)
	}
	img, _ = reconcileImage(t, r, c, "go")
	if w := condition(img, si.ConditionWarm); w.Status != metav1.ConditionTrue || w.Reason != reasonWarmPulled {
		t.Errorf("Warm = %+v, want Pulled", w)
	}
}

func TestSandboxImage_NotWarm(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", false))

	img, _ := reconcileImage(t, r, c, "go")
	if !img.Valid() {
		t.Fatalf("status = %+v, want valid", img.Status)
	}
	if w := condition(img, si.ConditionWarm); w.Reason != reasonWarmDisabled {
		t.Errorf("Warm = %+v, want Disabled", w)
	}
	if _, ok := warmer(t, c, "go"); ok {
		t.Error("an image that is not warm got a DaemonSet")
	}
}

// A rejected image loses its digest and its warmer. The rejection reasons are tested in TestValidateImageConfig.
func TestSandboxImage_Invalid(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": {Digest: "sha256:x", User: "0", Labels: goodConfig.Labels}}}
	stale := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "sandbox-image-go", Namespace: testNS}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", true), stale)

	img, _ := reconcileImage(t, r, c, "go")
	if img.Valid() || !img.Rejected() || img.Status.Digest != "" {
		t.Fatalf("status = %+v, want rejected without a digest", img.Status)
	}
	if _, ok := warmer(t, c, "go"); ok {
		t.Error("an invalid image keeps its DaemonSet")
	}
}

// A registry outage reports the error on a new image. An accepted image stays in service.
func TestSandboxImage_RegistryError(t *testing.T) {
	reg := &fakeRegistry{err: errors.New("registry unavailable")}
	r, c := newImageReconciler(reg, sandboxImage("new", "ghcr.io/acme/new:1", false))
	img, res := reconcileImage(t, r, c, "new")
	if v := condition(img, si.ConditionValid); v.Status != metav1.ConditionFalse || v.Reason != reasonRegistryError {
		t.Errorf("Valid = %+v, want RegistryError", v)
	}
	if res.RequeueAfter != registryRetry {
		t.Errorf("RequeueAfter = %v, want the short registry retry", res.RequeueAfter)
	}

	reg.err = nil
	reg.configs = map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}
	r, c = newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", false))
	reconcileImage(t, r, c, "go")
	reg.err = errors.New("registry unavailable")
	img, _ = reconcileImage(t, r, c, "go")
	if !img.Valid() || img.Status.Digest != "sha256:good" {
		t.Errorf("status = %+v, want the accepted digest kept", img.Status)
	}
}

func TestSandboxImage_PullSecret(t *testing.T) {
	img := sandboxImage("go", "ghcr.io/acme/go:1", false)
	img.Spec.PullSecretRef = &si.SecretNameRef{Name: "acme-pull"}
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}

	r, c := newImageReconciler(reg, img.DeepCopy())
	got, _ := reconcileImage(t, r, c, "go")
	if v := condition(got, si.ConditionValid); v.Reason != reasonPullSecretError {
		t.Errorf("Valid = %+v, want PullSecretError for a missing secret", v)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-pull", Namespace: testNS},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"ghcr.io":{"username":"u","password":"p"}}}`)},
	}
	r, c = newImageReconciler(reg, img.DeepCopy(), secret)
	got, _ = reconcileImage(t, r, c, "go")
	if !got.Valid() {
		t.Fatalf("status = %+v, want valid", got.Status)
	}
	if len(reg.auth) != 1 || reg.auth[0].Auths["ghcr.io"].Username != "u" {
		t.Errorf("registry auth = %+v, want the image's pull secret", reg.auth)
	}
}

func TestEnsureBaseSandboxImage(t *testing.T) {
	tenant := &OrbitTenant{ObjectMeta: metav1.ObjectMeta{Name: "acme"}, Spec: OrbitTenantSpec{Namespace: testNS}}
	c := fake.NewClientBuilder().WithScheme(NewScheme()).Build()
	r := &TenantReconciler{Client: c, SandboxBase: SandboxBase{Image: "ghcr.io/runkora/kora-sandbox-base:1", Warm: true}}

	if err := r.ensureBaseSandboxImage(context.Background(), tenant); err != nil {
		t.Fatal(err)
	}
	// A tenant edit is restored on the next reconcile.
	got := &si.SandboxImage{}
	key := types.NamespacedName{Namespace: testNS, Name: si.BaseName}
	if err := c.Get(context.Background(), key, got); err != nil {
		t.Fatal(err)
	}
	got.Spec.Image = "ghcr.io/evil/image:1"
	got.Spec.Warm = false
	got.Spec.PullSecretRef = &si.SecretNameRef{Name: "tenant-added"}
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureBaseSandboxImage(context.Background(), tenant); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), key, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Image != "ghcr.io/runkora/kora-sandbox-base:1" || !got.Spec.Warm {
		t.Errorf("kora-base spec = %+v, want the install's base image, warm", got.Spec)
	}
	if got.Spec.PullSecretRef != nil {
		t.Errorf("pullSecretRef = %+v, want the tenant's addition removed", got.Spec.PullSecretRef)
	}
}

// A SandboxImage outside a tenant namespace gets no status and no warmer.
func TestSandboxImage_OutsideTenantNamespace(t *testing.T) {
	img := sandboxImage("go", "ghcr.io/acme/go:1", true)
	img.Namespace = "default"
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, img, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "go"}}); err != nil {
		t.Fatal(err)
	}
	got := &si.SandboxImage{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "go"}, got)
	if got.Status.Digest != "" || len(got.Status.Conditions) != 0 {
		t.Errorf("status = %+v, want untouched", got.Status)
	}
	ds := &appsv1.DaemonSet{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "sandbox-image-go"}, ds); !apierrors.IsNotFound(err) {
		t.Errorf("warmer outside a tenant namespace: %v", err)
	}
}

// A placement field the operator drops from its apply is removed from the warmer.
func TestSandboxImage_WarmerDropsRemovedPlacement(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", true))
	reconcileImage(t, r, c, "go")

	r.Placement = WarmerPlacement{}
	reconcileImage(t, r, c, "go")
	ds, ok := warmer(t, c, "go")
	if !ok {
		t.Fatal("warmer gone")
	}
	if len(ds.Spec.Template.Spec.NodeSelector) != 0 {
		t.Errorf("nodeSelector = %v, want it removed", ds.Spec.Template.Spec.NodeSelector)
	}
}

// Toggling warm changes the generation and keeps the image. The image stays
// valid while the registry is down.
func TestSandboxImage_WarmToggleKeepsValid(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", true))
	img, _ := reconcileImage(t, r, c, "go")

	img.Spec.Warm = false
	img.Generation++
	if err := c.Update(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	got := &si.SandboxImage{}
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: testNS, Name: "go"}, got)
	if !got.Valid() {
		t.Fatalf("status = %+v, want valid before the next read", got.Status)
	}
	reg.err = errors.New("registry unavailable")
	got, _ = reconcileImage(t, r, c, "go")
	if !got.Valid() || got.Status.Digest != "sha256:good" {
		t.Errorf("status = %+v, want the accepted digest kept", got.Status)
	}
	if _, ok := warmer(t, c, "go"); ok {
		t.Error("the warmer stayed after warm was turned off")
	}
}

// An upgrade that changes the image while the registry is down leaves the image
// neither valid nor rejected. Status keeps the old digest.
func TestSandboxImage_ImageChangeWithRegistryDown(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": goodConfig}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", false))
	img, _ := reconcileImage(t, r, c, "go")

	img.Spec.Image = "ghcr.io/acme/go:2"
	img.Generation++
	if err := c.Update(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	reg.err = errors.New("registry unavailable")
	got, res := reconcileImage(t, r, c, "go")
	if got.Valid() || got.Rejected() {
		t.Errorf("valid=%v rejected=%v, want neither while the new image is unread", got.Valid(), got.Rejected())
	}
	if got.Status.Image != "ghcr.io/acme/go:1" || got.Status.Digest != "sha256:good" {
		t.Errorf("status = %+v, want the last read image and digest kept", got.Status)
	}
	if v := condition(got, si.ConditionValid); v.Reason != reasonRegistryError {
		t.Errorf("Valid = %+v, want RegistryError", v)
	}
	if res.RequeueAfter != registryRetry {
		t.Errorf("RequeueAfter = %v, want the registry retry", res.RequeueAfter)
	}
}

// A registry outage keeps a rejected image rejected.
func TestSandboxImage_RejectedWithRegistryDown(t *testing.T) {
	reg := &fakeRegistry{configs: map[string]imageConfig{"ghcr.io/acme/go:1": {Digest: "sha256:x", User: "0", Labels: goodConfig.Labels}}}
	r, c := newImageReconciler(reg, sandboxImage("go", "ghcr.io/acme/go:1", true))
	reconcileImage(t, r, c, "go")

	reg.err = errors.New("registry unavailable")
	got, res := reconcileImage(t, r, c, "go")
	if !got.Rejected() || condition(got, si.ConditionValid).Reason != si.ReasonWrongUser {
		t.Errorf("status = %+v, want the WrongUser rejection kept", got.Status)
	}
	if _, ok := warmer(t, c, "go"); ok {
		t.Error("a rejected image got a DaemonSet")
	}
	if res.RequeueAfter != registryRetry {
		t.Errorf("RequeueAfter = %v, want the registry retry", res.RequeueAfter)
	}
}
