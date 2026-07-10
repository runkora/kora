package operator

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	//
	// Pre-seeded with the in-vcluster OrbitPlatform already Ready: most tests
	// in this file are about the OUTER OrbitDevEnvironment's own lifecycle
	// (namespace/vcluster/sleep/wake/TTL), not about waiting on the nested
	// install to converge, and reconcileInVClusterPlatform's "existing found"
	// branch only ever patches .Spec, so this seeded .Status survives every
	// reconcile untouched. Tests that specifically exercise the
	// platform-readiness gate (e.g.
	// TestReconcile_DoesNotReportReadyUntilInVClusterPlatformReady) build
	// their own vcClient instead of calling this helper.
	vcClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&OrbitPlatform{
			ObjectMeta: metav1.ObjectMeta{Name: inVClusterOrbitPlatformName},
			Status:     OrbitPlatformStatus{Phase: PhaseReady},
		}).
		Build()

	r := &DevEnvironmentReconciler{
		Client:          c,
		Scheme:          scheme,
		Logger:          logging.NewLoggerWithOutput(io.Discard),
		HelmFactory:     func(string) HelmInterface { return helm },
		Clock:           clock,
		IngressDomain:   "dev.orbit.test",
		AppExternalURL:  "http://devenv.orbit.test:8880",
		VClusterVersion: "0.35.1",
		ChartDir:        "/etc/orbit/charts",
		ImageRegistry:   "orbit-registry:5001",
		VClusterRestConfigFactory: func(context.Context, string) (*rest.Config, error) {
			return &rest.Config{Host: "https://vcluster.fake"}, nil
		},
		VClusterClientFactory: func(*rest.Config) (client.Client, error) { return vcClient, nil },
		InVClusterHelmFactory: func(string, *rest.Config) HelmInterface { return inVClusterHelm },
		HostJWKSFetcher: func(context.Context) (string, string, error) {
			return "https://kubernetes.default.svc.cluster.local", `{"keys":[]}`, nil
		},
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

	gw := &unstructured.Unstructured{}
	gw.SetGroupVersionKind(istioGatewayGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIGatewayName, Namespace: devEnvAPIGatewayNamespace}, gw); err != nil {
		t.Fatalf("shared devenv API Gateway not created: %v", err)
	}
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(istioVirtualServiceGVK())
	// Lives alongside the shared Gateway (istio-system), not the environment's
	// own namespace — the host istiod's discoverySelectors exclude
	// orbit.io/role=devenv namespaces entirely, confirmed live (see
	// devenvironment_ingress.go's ensureDevEnvAPIRoute doc comment).
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, vs); err != nil {
		t.Fatalf("devenv API VirtualService not created: %v", err)
	}
	tlsRoutes, _, _ := unstructured.NestedSlice(vs.Object, "spec", "tls")
	if len(tlsRoutes) != 1 {
		t.Fatalf("VirtualService spec.tls has %d entries, want 1", len(tlsRoutes))
	}
	matches, _, _ := unstructured.NestedSlice(tlsRoutes[0].(map[string]any), "match")
	sniHosts, _, _ := unstructured.NestedStringSlice(matches[0].(map[string]any), "sniHosts")
	if len(sniHosts) != 1 || sniHosts[0] != "alice-env.dev.orbit.test" {
		t.Errorf("VirtualService sniHosts = %v, want [alice-env.dev.orbit.test]", sniHosts)
	}

	// The ServiceEntry (also istio-system, same reason) is what makes the
	// VirtualService's route destination resolvable despite the devenv
	// namespace's own Service being hidden from istiod by discoverySelectors.
	se := &unstructured.Unstructured{}
	se.SetGroupVersionKind(istioServiceEntryGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, se); err != nil {
		t.Fatalf("devenv API ServiceEntry not created: %v", err)
	}
	seHosts, _, _ := unstructured.NestedStringSlice(se.Object, "spec", "hosts")
	if len(seHosts) != 1 || seHosts[0] != "vcluster.orbit-devenv-alice-env.svc.cluster.local" {
		t.Errorf("ServiceEntry hosts = %v, want [vcluster.orbit-devenv-alice-env.svc.cluster.local]", seHosts)
	}
	if resolution, _, _ := unstructured.NestedString(se.Object, "spec", "resolution"); resolution != "DNS" {
		t.Errorf("ServiceEntry resolution = %q, want DNS", resolution)
	}
	// MESH_EXTERNAL, not MESH_INTERNAL: confirmed live that MESH_INTERNAL
	// makes Envoy attempt Istio auto mTLS against the (sidecar-less) vcluster
	// apiserver, breaking the TLS passthrough (see
	// ensureDevEnvAPIServiceEntry's doc comment).
	if location, _, _ := unstructured.NestedString(se.Object, "spec", "location"); location != "MESH_EXTERNAL" {
		t.Errorf("ServiceEntry location = %q, want MESH_EXTERNAL", location)
	}

	// Path-based app ingress (the ADR's 2026-07-10 amendment): the shared L7
	// Gateway, the aggregated routing VirtualService, and the per-environment
	// backend ServiceEntry.
	appGW := &unstructured.Unstructured{}
	appGW.SetGroupVersionKind(istioGatewayGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppGatewayName, Namespace: devEnvAPIGatewayNamespace}, appGW); err != nil {
		t.Fatalf("shared devenv app Gateway not created: %v", err)
	}
	appVS := &unstructured.Unstructured{}
	appVS.SetGroupVersionKind(istioVirtualServiceGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppVirtualServiceName, Namespace: devEnvAPIGatewayNamespace}, appVS); err != nil {
		t.Fatalf("aggregated devenv app VirtualService not created: %v", err)
	}
	appHosts, _, _ := unstructured.NestedStringSlice(appVS.Object, "spec", "hosts")
	if len(appHosts) != 1 || appHosts[0] != "devenv.orbit.test" {
		t.Errorf("app VirtualService hosts = %v, want [devenv.orbit.test] (hostname only, no scheme/port)", appHosts)
	}
	appRoutes, _, _ := unstructured.NestedSlice(appVS.Object, "spec", "http")
	if len(appRoutes) != 3 {
		t.Fatalf("app VirtualService has %d http routes, want 3 (exact + prefix + cookie)", len(appRoutes))
	}
	appSE := &unstructured.Unstructured{}
	appSE.SetGroupVersionKind(istioServiceEntryGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: "devenv-app-alice-env", Namespace: devEnvAPIGatewayNamespace}, appSE); err != nil {
		t.Fatalf("devenv app ServiceEntry not created: %v", err)
	}
	appSEHosts, _, _ := unstructured.NestedStringSlice(appSE.Object, "spec", "hosts")
	wantBackend := "gateway-x-istio-system-x-vcluster.orbit-devenv-alice-env.svc.cluster.local"
	if len(appSEHosts) != 1 || appSEHosts[0] != wantBackend {
		t.Errorf("app ServiceEntry hosts = %v, want [%s]", appSEHosts, wantBackend)
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
	if got.Status.AppURL != "http://devenv.orbit.test:8880/alice-env" {
		t.Errorf("status.appURL = %q, want the shared base URL + /{env-id}", got.Status.AppURL)
	}
	if got.Status.ExpiresAt == nil {
		t.Error("status.expiresAt not set")
	}
}

// TestReconcile_AppRoutesAggregateAcrossEnvironments pins the one property
// the aggregated app VirtualService exists for: with several environments on
// the shared hostname, every environment's path-prefix routes come BEFORE
// every environment's cookie-fallback route, so a stale routing cookie can
// never shadow an explicit /{env-id} entry (Istio's merge order across
// separate VirtualServices on one host is undefined — see
// devenvironment_app_ingress.go's package comment).
func TestReconcile_AppRoutesAggregateAcrossEnvironments(t *testing.T) {
	clock := newFakeClock(time.Now())
	envA := newTestEnv("env-a", nil)
	envB := newTestEnv("env-b", nil)
	r, _ := newDevEnvTestReconciler(t, clock, envA, envB)

	reconcileOnce(t, r, envA.Name)
	reconcileOnce(t, r, envB.Name)

	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(istioVirtualServiceGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppVirtualServiceName, Namespace: devEnvAPIGatewayNamespace}, vs); err != nil {
		t.Fatalf("aggregated app VirtualService not found: %v", err)
	}
	routes, _, _ := unstructured.NestedSlice(vs.Object, "spec", "http")
	var names []string
	for _, route := range routes {
		name, _, _ := unstructured.NestedString(route.(map[string]any), "name")
		names = append(names, name)
	}
	want := []string{
		"devenv-env-a-entry", "devenv-env-a-prefix",
		"devenv-env-b-entry", "devenv-env-b-prefix",
		"devenv-env-a-cookie", "devenv-env-b-cookie",
	}
	if len(names) != len(want) {
		t.Fatalf("route names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("route order mismatch at %d: %v, want %v", i, names, want)
		}
	}
}

func TestReconcile_DoesNotReportReadyUntilInVClusterPlatformReady(t *testing.T) {
	// Regression test for a real gap found running a live provisioning
	// walkthrough: the reconciler used to flip status.phase to Ready as soon
	// as the OrbitPlatform/OrbitTenant CRs were submitted inside the
	// vcluster, well before the nested orbit-operator had actually finished
	// installing identity/frontend/the gateways — the UI showed "Ready" and a
	// live ingress link ~60-90s before the environment could serve anything.
	clock := newFakeClock(time.Now())
	env := newTestEnv("converging-env", nil)
	// Unlike newDevEnvTestReconcilerFull, this vcClient starts with no
	// OrbitPlatform pre-seeded, so the first reconcile's Create leaves its
	// status.phase empty — exactly the "applied but not yet ready" state
	// this test exercises.
	r, _, vcClient, _ := newDevEnvTestReconcilerFull(t, clock, env)
	if err := vcClient.Delete(context.Background(), &OrbitPlatform{ObjectMeta: metav1.ObjectMeta{Name: inVClusterOrbitPlatformName}}); err != nil {
		t.Fatalf("remove pre-seeded in-vcluster OrbitPlatform: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseProvisioning {
		t.Fatalf("phase while nested platform not yet ready = %q, want %q", got.Status.Phase, DevEnvPhaseProvisioning)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, ConditionDevEnvPlatformReady)
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("ConditionDevEnvPlatformReady = %+v, want status False", cond)
	}
	// The CR was still applied even though it isn't ready yet.
	appliedCond := meta.FindStatusCondition(got.Status.Conditions, ConditionDevEnvPlatformApplied)
	if appliedCond == nil || appliedCond.Status != metav1.ConditionTrue {
		t.Fatalf("ConditionDevEnvPlatformApplied = %+v, want status True", appliedCond)
	}

	// Simulate the nested orbit-operator finishing its own release sequence.
	platform := &OrbitPlatform{}
	if err := vcClient.Get(context.Background(), types.NamespacedName{Name: inVClusterOrbitPlatformName}, platform); err != nil {
		t.Fatalf("get in-vcluster OrbitPlatform: %v", err)
	}
	platform.Status.Phase = PhaseReady
	if err := vcClient.Update(context.Background(), platform); err != nil {
		t.Fatalf("mark in-vcluster OrbitPlatform Ready: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	got = getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase after nested platform reports Ready = %q, want %q", got.Status.Phase, DevEnvPhaseReady)
	}
	cond = meta.FindStatusCondition(got.Status.Conditions, ConditionDevEnvPlatformReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("ConditionDevEnvPlatformReady after nested Ready = %+v, want status True", cond)
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

func TestReconcile_SleepScalesInVClusterWorkloadsNotJustHostVCluster(t *testing.T) {
	// Regression test for a real gap found running the ADR's Phase 2 local
	// acceptance checklist against a live vcluster: a vcluster's syncer only
	// mirrors Pods onto the host by default, so the Deployment/StatefulSet
	// controllers that own an environment's real workloads (Postgres, Redis,
	// istiod, the in-vcluster orbit-operator, ...) live entirely inside the
	// vcluster's own API server. scaleWorkloads, which only ever listed the
	// HOST namespace, could never reach them — "sleep" stopped the vcluster
	// control-plane pod but left every workload inside it running.
	clock := newFakeClock(time.Now())
	env := newTestEnv("bob-env", func(e *OrbitDevEnvironment) {
		e.Spec.IdleTimeout = metav1.Duration{Duration: 1 * time.Hour}
	})
	r, _, vcClient, _ := newDevEnvTestReconcilerFull(t, clock, env)

	reconcileOnce(t, r, env.Name)

	// A workload inside the vcluster, in a namespace distinct from the host
	// devenv namespace entirely — proving the scale reaches across the
	// vcluster's own namespaces, not just a mirrored one.
	inVCluster := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "some-in-vcluster-workload", Namespace: "orbit-devenv-fixtures"},
		Spec:       appsv1.DeploymentSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}},
	}
	if err := vcClient.Create(context.Background(), inVCluster); err != nil {
		t.Fatalf("create in-vcluster deployment: %v", err)
	}

	clock.Advance(90 * time.Minute)
	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseSleeping {
		t.Fatalf("phase after idle timeout = %q, want Sleeping", got.Status.Phase)
	}

	scaled := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), types.NamespacedName{Name: "some-in-vcluster-workload", Namespace: "orbit-devenv-fixtures"}, scaled); err != nil {
		t.Fatalf("get in-vcluster deployment: %v", err)
	}
	if scaled.Spec.Replicas == nil || *scaled.Spec.Replicas != 0 {
		t.Errorf("in-vcluster replicas after sleep = %v, want 0", scaled.Spec.Replicas)
	}
	if scaled.Annotations[preSleepReplicasAnnotation] != "1" {
		t.Errorf("in-vcluster pre-sleep replicas annotation = %q, want %q", scaled.Annotations[preSleepReplicasAnnotation], "1")
	}

	// Explicit wake.
	current := getEnv(t, r, env.Name)
	current.Spec.Awake = true
	if err := r.Update(context.Background(), current); err != nil {
		t.Fatalf("set awake=true: %v", err)
	}
	reconcileOnce(t, r, env.Name)

	woken := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), types.NamespacedName{Name: "some-in-vcluster-workload", Namespace: "orbit-devenv-fixtures"}, woken); err != nil {
		t.Fatalf("get woken in-vcluster deployment: %v", err)
	}
	if woken.Spec.Replicas == nil || *woken.Spec.Replicas != 1 {
		t.Errorf("in-vcluster replicas after wake = %v, want 1 (restored)", woken.Spec.Replicas)
	}
	if _, ok := woken.Annotations[preSleepReplicasAnnotation]; ok {
		t.Error("in-vcluster pre-sleep replicas annotation should be removed after restore")
	}
}

func TestHandleDeletion_DirectDeleteRunsSameCleanupAsTTLExpiry(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("deleted-directly", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)

	// The API-routing VirtualService lives outside the environment's own
	// namespace (istio-system, alongside the shared Gateway — see
	// devenvironment_ingress.go), so it needs its own explicit-cleanup
	// assertion: namespace deletion alone would not remove it.
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(istioVirtualServiceGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, vs); err != nil {
		t.Fatalf("devenv API VirtualService not created: %v", err)
	}
	se := &unstructured.Unstructured{}
	se.SetGroupVersionKind(istioServiceEntryGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, se); err != nil {
		t.Fatalf("devenv API ServiceEntry not created: %v", err)
	}

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
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, se); err == nil {
		t.Error("devenv API ServiceEntry still exists after direct delete + finalizer cleanup")
	}
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIVirtualServiceName(env), Namespace: devEnvAPIGatewayNamespace}, vs); err == nil {
		t.Error("devenv API VirtualService still exists after direct delete + finalizer cleanup")
	}

	// App-ingress cleanup: the per-environment ServiceEntry goes, and with the
	// last environment gone the aggregated routing VirtualService goes too.
	appSE := &unstructured.Unstructured{}
	appSE.SetGroupVersionKind(istioServiceEntryGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: "devenv-app-" + env.Name, Namespace: devEnvAPIGatewayNamespace}, appSE); err == nil {
		t.Error("devenv app ServiceEntry still exists after direct delete + finalizer cleanup")
	}
	appVS := &unstructured.Unstructured{}
	appVS.SetGroupVersionKind(istioVirtualServiceGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppVirtualServiceName, Namespace: devEnvAPIGatewayNamespace}, appVS); err == nil {
		t.Error("aggregated devenv app VirtualService still exists after the last environment's cleanup")
	}
}

//go:fix inline
func int32Ptr(v int32) *int32 { return new(v) }
