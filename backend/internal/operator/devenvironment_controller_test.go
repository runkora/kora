package operator

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

var errImmediateFailure = errors.New("simulated helm failure")

func newDevEnvTestReconciler(t *testing.T, clock *fakeClock, objs ...client.Object) (*DevEnvironmentReconciler, *fakeHelm) {
	t.Helper()
	r, helm, _, _ := newDevEnvTestReconcilerFull(t, clock, objs...)
	return r, helm
}

// newDevEnvTestReconcilerFull is newDevEnvTestReconciler plus the two
// in-vcluster-install seams (the vcluster's own fake client and its
// orbit-operator fakeHelm) for tests that assert on what got applied inside
// the vcluster, not just the host side.
func newDevEnvTestReconcilerFull(t *testing.T, clock *fakeClock, objs ...client.Object) (*DevEnvironmentReconciler, *fakeHelm, client.Client, *fakeHelm) {
	t.Helper()
	scheme := NewScheme()
	helm := newFakeHelm()
	inVClusterHelm := newFakeHelm()

	hostPlatform := &OrbitPlatform{
		ObjectMeta: metav1.ObjectMeta{Name: "orbit"},
		Spec:       OrbitPlatformSpec{TrustDomain: "orbit.test"},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&OrbitDevEnvironment{}).
		WithObjects(append(objs, hostPlatform)...).
		Build()

	// The vcluster client is a second, independent fake — standing in for the
	// vcluster's own API server, which is why applying OrbitPlatform/OrbitTenant
	// here doesn't affect the host fake client c above.
	vcClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &DevEnvironmentReconciler{
		Client:          c,
		Scheme:          scheme,
		Logger:          logging.NewLoggerWithOutput(io.Discard),
		HelmFactory:     func(string) HelmInterface { return helm },
		Clock:           clock,
		IngressDomain:   "dev.orbit.test",
		VClusterVersion: "0.35.1",
		ChartDir:        "/etc/orbit/charts",
		ImageRegistry:   "orbit-registry:5001",
		VClusterRestConfigFactory: func(context.Context, string) (*rest.Config, error) {
			return &rest.Config{Host: "https://vcluster.fake"}, nil
		},
		VClusterClientFactory: func(*rest.Config) (client.Client, error) { return vcClient, nil },
		InVClusterHelmFactory: func(string, *rest.Config) HelmInterface { return inVClusterHelm },
	}
	return r, helm, vcClient, inVClusterHelm
}

func reconcileOnce(t *testing.T, r *DevEnvironmentReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

func getEnv(t *testing.T, r *DevEnvironmentReconciler, name string) *OrbitDevEnvironment {
	t.Helper()
	env := &OrbitDevEnvironment{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: name}, env); err != nil {
		t.Fatalf("get env %q: %v", name, err)
	}
	return env
}

func newTestEnv(name string, mutate func(*OrbitDevEnvironment)) *OrbitDevEnvironment {
	env := &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Now(),
		},
		Spec: OrbitDevEnvironmentSpec{
			Owner: "dev@example.com",
			Awake: true,
			TTL:   metav1.Duration{Duration: 24 * time.Hour},
		},
	}
	if mutate != nil {
		mutate(env)
	}
	return env
}

func TestReconcile_ProvisionsNamespaceQuotaAndVCluster(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("alice-env", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)

	ns := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-alice-env"}, ns); err != nil {
		t.Fatalf("namespace not created: %v", err)
	}
	if ns.Labels["orbit.io/role"] != "devenv" {
		t.Errorf("orbit.io/role = %q, want devenv", ns.Labels["orbit.io/role"])
	}
	if ns.Labels["orbit.io/owner"] != "dev-example.com" {
		t.Errorf("orbit.io/owner = %q, want sanitized owner", ns.Labels["orbit.io/owner"])
	}

	quota := &corev1.ResourceQuota{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-quota", Namespace: "orbit-devenv-alice-env"}, quota); err != nil {
		t.Fatalf("ResourceQuota not created: %v", err)
	}
	limits := &corev1.LimitRange{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-limits", Namespace: "orbit-devenv-alice-env"}, limits); err != nil {
		t.Fatalf("LimitRange not created: %v", err)
	}

	if helm.InstallOrUpgradeCalls == 0 {
		t.Error("expected vcluster InstallOrUpgrade to be called")
	}

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseReady {
		t.Errorf("phase = %q, want %q", got.Status.Phase, DevEnvPhaseReady)
	}
	if got.Status.Namespace != "orbit-devenv-alice-env" {
		t.Errorf("status.namespace = %q", got.Status.Namespace)
	}
	if got.Status.IngressHost != "alice-env.dev.orbit.test" {
		t.Errorf("status.ingressHost = %q", got.Status.IngressHost)
	}
	if got.Status.ExpiresAt == nil {
		t.Error("status.expiresAt not set")
	}
}

func TestReconcile_IdempotentReReconcileAfterPartialFailure(t *testing.T) {
	// Simulates B3's self-healing claim: a reconcile that fails partway
	// through (Helm error) must converge cleanly on the next attempt rather
	// than getting stuck or duplicating the namespace/quota work.
	clock := newFakeClock(time.Now())
	env := newTestEnv("flaky-env", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)
	helm.InstallOrUpgradeErr = errImmediateFailure

	reconcileOnce(t, r, env.Name)
	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseFailed {
		t.Fatalf("phase after failed helm install = %q, want %q", got.Status.Phase, DevEnvPhaseFailed)
	}

	// Namespace/quota/limits must still have been created despite the
	// downstream Helm failure.
	ns := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-flaky-env"}, ns); err != nil {
		t.Fatalf("namespace not created despite helm failure: %v", err)
	}

	helm.InstallOrUpgradeErr = nil
	reconcileOnce(t, r, env.Name)
	got = getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseReady {
		t.Errorf("phase after recovery = %q, want %q", got.Status.Phase, DevEnvPhaseReady)
	}
}

func TestReconcile_TTLExpiryDeletesEnvironmentAndCleansUp(t *testing.T) {
	start := time.Now()
	clock := newFakeClock(start)
	env := newTestEnv("short-lived", func(e *OrbitDevEnvironment) {
		e.Spec.TTL = metav1.Duration{Duration: 1 * time.Hour}
	})
	r, helm := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)
	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase before expiry = %q, want Ready", got.Status.Phase)
	}

	clock.Advance(2 * time.Hour)
	// First reconcile sets DeletionTimestamp (the finalizer keeps the object
	// alive, exactly like a real API server); the second simulates the
	// watch event that DeletionTimestamp change triggers, running the actual
	// finalizer cleanup.
	reconcileOnce(t, r, env.Name)
	reconcileOnce(t, r, env.Name)

	env2 := &OrbitDevEnvironment{}
	err := r.Get(context.Background(), types.NamespacedName{Name: env.Name}, env2)
	if err == nil {
		t.Fatalf("environment still exists after TTL expiry (finalizer should have completed deletion via the fake client)")
	}

	if helm.UninstallCalls == 0 {
		t.Error("expected vcluster Uninstall to be called on TTL expiry")
	}
	ns := &corev1.Namespace{}
	nsErr := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-short-lived"}, ns)
	if nsErr == nil {
		t.Error("namespace still exists after TTL expiry")
	}
}

func TestComputeExpiry_ExtendableButCappedAtHardCap(t *testing.T) {
	created := time.Now()
	env := &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(created)},
		Spec:       OrbitDevEnvironmentSpec{TTL: metav1.Duration{Duration: 30 * 24 * time.Hour}},
	}
	got := computeExpiry(env)
	want := created.Add(devEnvTTLHardCap)
	if !got.Equal(want) {
		t.Errorf("computeExpiry with TTL beyond hard cap = %v, want %v (capped)", got, want)
	}

	env.Spec.TTL = metav1.Duration{Duration: 48 * time.Hour}
	got = computeExpiry(env)
	want = created.Add(48 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("computeExpiry with extended TTL = %v, want %v", got, want)
	}
}

func TestReconcile_IdleTimeoutSleepsThenExplicitWakeRestoresReplicas(t *testing.T) {
	start := time.Now()
	clock := newFakeClock(start)
	env := newTestEnv("idle-env", func(e *OrbitDevEnvironment) {
		e.Spec.IdleTimeout = metav1.Duration{Duration: 1 * time.Hour}
	})
	r, _ := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)
	if got := getEnv(t, r, env.Name); got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase before idle = %q, want Ready", got.Status.Phase)
	}

	// Give the environment a real Deployment to scale, mimicking a
	// synced/in-vcluster workload in its namespace.
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "some-workload", Namespace: "orbit-devenv-idle-env"},
		Spec:       appsv1.DeploymentSpec{Replicas: new(int32(3)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}},
	}
	if err := r.Create(context.Background(), dep); err != nil {
		t.Fatalf("create test deployment: %v", err)
	}

	clock.Advance(90 * time.Minute)
	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseSleeping {
		t.Fatalf("phase after idle timeout = %q, want Sleeping", got.Status.Phase)
	}

	scaled := &appsv1.Deployment{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "some-workload", Namespace: "orbit-devenv-idle-env"}, scaled); err != nil {
		t.Fatalf("get scaled deployment: %v", err)
	}
	if scaled.Spec.Replicas == nil || *scaled.Spec.Replicas != 0 {
		t.Errorf("replicas after sleep = %v, want 0", scaled.Spec.Replicas)
	}
	if scaled.Annotations[preSleepReplicasAnnotation] != "3" {
		t.Errorf("pre-sleep replicas annotation = %q, want %q", scaled.Annotations[preSleepReplicasAnnotation], "3")
	}

	// Sleep is automatic on idle, but wake is only ever explicit (decision 8)
	// — reconciling again without touching spec.Awake must NOT wake it.
	reconcileOnce(t, r, env.Name)
	if got := getEnv(t, r, env.Name); got.Status.Phase != DevEnvPhaseSleeping {
		t.Fatalf("phase stayed asleep without explicit wake = %q, want Sleeping", got.Status.Phase)
	}

	// Explicit wake: flip spec.Awake false->true (simulating the UI/API
	// action from decision 8) and reconcile again.
	current := getEnv(t, r, env.Name)
	current.Spec.Awake = false
	if err := r.Update(context.Background(), current); err != nil {
		t.Fatalf("set awake=false: %v", err)
	}
	reconcileOnce(t, r, env.Name)
	current = getEnv(t, r, env.Name)
	current.Spec.Awake = true
	if err := r.Update(context.Background(), current); err != nil {
		t.Fatalf("set awake=true: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	got = getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase after explicit wake = %q, want Ready", got.Status.Phase)
	}

	woken := &appsv1.Deployment{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "some-workload", Namespace: "orbit-devenv-idle-env"}, woken); err != nil {
		t.Fatalf("get woken deployment: %v", err)
	}
	if woken.Spec.Replicas == nil || *woken.Spec.Replicas != 3 {
		t.Errorf("replicas after wake = %v, want 3 (restored from pre-sleep annotation)", woken.Spec.Replicas)
	}
	if _, ok := woken.Annotations[preSleepReplicasAnnotation]; ok {
		t.Error("pre-sleep replicas annotation should be removed after restore")
	}
}

func TestReconcile_ExplicitSleepViaSpecAwakeFalse(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("manual-sleep-env", nil)
	r, _ := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)

	current := getEnv(t, r, env.Name)
	current.Spec.Awake = false
	if err := r.Update(context.Background(), current); err != nil {
		t.Fatalf("set awake=false: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseSleeping {
		t.Errorf("phase after spec.awake=false = %q, want Sleeping", got.Status.Phase)
	}
}

func TestHandleDeletion_DirectDeleteRunsSameCleanupAsTTLExpiry(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("deleted-directly", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)

	current := getEnv(t, r, env.Name)
	if err := r.Delete(context.Background(), current); err != nil {
		t.Fatalf("delete: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	if helm.UninstallCalls == 0 {
		t.Error("expected Uninstall on direct delete")
	}
	ns := &corev1.Namespace{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "orbit-devenv-deleted-directly"}, ns); err == nil {
		t.Error("namespace still exists after direct delete + finalizer cleanup")
	}
}

//go:fix inline
func int32Ptr(v int32) *int32 { return new(v) }
