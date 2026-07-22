package operator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/clear-route/orbit/internal/platform/orbitapi"
)

var errImmediateFailure = errors.New("simulated helm failure")

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
	// orbit.io/role=devenv namespaces entirely.
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
	// MESH_EXTERNAL, not MESH_INTERNAL: MESH_INTERNAL makes Envoy attempt
	// Istio auto mTLS against the (sidecar-less) vcluster apiserver, breaking
	// the TLS passthrough.
	if location, _, _ := unstructured.NestedString(se.Object, "spec", "location"); location != "MESH_EXTERNAL" {
		t.Errorf("ServiceEntry location = %q, want MESH_EXTERNAL", location)
	}

	// Path-based app ingress: NO devenv-owned Gateway (it would silently
	// conflict with the platform's own orbit-gateway on the same host) —
	// just the aggregated routing VirtualService attached to orbit-gateway,
	// and the per-environment backend ServiceEntry.
	appGW := &unstructured.Unstructured{}
	appGW.SetGroupVersionKind(istioGatewayGVK())
	getErr := r.Get(context.Background(), types.NamespacedName{Name: legacyDevEnvAppGatewayName, Namespace: devEnvAPIGatewayNamespace}, appGW)
	if !apierrors.IsNotFound(getErr) {
		t.Fatalf("legacy devenv app Gateway should not exist, got err: %v", getErr)
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
	appGateways, _, _ := unstructured.NestedStringSlice(appVS.Object, "spec", "gateways")
	if len(appGateways) != 1 || appGateways[0] != systemNamespace+"/"+hostOrbitGatewayName {
		t.Errorf("app VirtualService gateways = %v, want [%s]", appGateways, systemNamespace+"/"+hostOrbitGatewayName)
	}
	appRoutes, _, _ := unstructured.NestedSlice(appVS.Object, "spec", "http")
	if len(appRoutes) != 4 {
		t.Fatalf("app VirtualService has %d http routes, want 4 (dex-exact + dex-prefix + exact + prefix)", len(appRoutes))
	}
	appSE := &unstructured.Unstructured{}
	appSE.SetGroupVersionKind(istioServiceEntryGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: "devenv-app-alice-env", Namespace: devEnvAPIGatewayNamespace}, appSE); err != nil {
		t.Fatalf("devenv app ServiceEntry not created: %v", err)
	}
	appSEHosts, _, _ := unstructured.NestedStringSlice(appSE.Object, "spec", "hosts")
	wantBackend := "gateway-x-istio-system-x-vcluster.orbit-devenv-alice-env.svc.cluster.local"
	wantDexBackend := "orbit-dex-x-orbit-devenv-fixtures-x-vcluster.orbit-devenv-alice-env.svc.cluster.local"
	if len(appSEHosts) != 2 || appSEHosts[0] != wantBackend || appSEHosts[1] != wantDexBackend {
		t.Errorf("app ServiceEntry hosts = %v, want [%s %s]", appSEHosts, wantBackend, wantDexBackend)
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
// the shared hostname, every environment's routes land in ONE aggregated
// object in a deterministic order (Istio's merge order across separate
// VirtualServices on one host is undefined), each environment's Dex routes
// preceding its own app-prefix routes.
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
		"devenv-env-a-dex-entry", "devenv-env-a-dex-prefix", "devenv-env-a-entry", "devenv-env-a-prefix",
		"devenv-env-b-dex-entry", "devenv-env-b-dex-prefix", "devenv-env-b-entry", "devenv-env-b-prefix",
	}
	if len(names) != len(want) {
		t.Fatalf("route names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("route order mismatch at %d: %v, want %v", i, names, want)
		}
	}

	// Regression guard: Dex's own issuer is the FULL /{env-id}/dex URL, and
	// Dex mounts every one of its routes under that issuer path — a route
	// that rewrites the prefix away (as the app's own routes correctly do)
	// sends Dex a path it never registered and 404s. Dex routes must carry
	// no "rewrite" at all.
	for _, route := range routes {
		name, _, _ := unstructured.NestedString(route.(map[string]any), "name")
		if !strings.Contains(name, "-dex-") {
			continue
		}
		if _, found, _ := unstructured.NestedMap(route.(map[string]any), "rewrite"); found {
			t.Errorf("route %q must not rewrite the URI — Dex expects the full /{env-id}/dex path", name)
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

// TestReconcile_IdempotentReReconcileAfterPartialFailure adapts the
// reference's "a reconcile that fails partway through must converge cleanly
// on the next attempt" intent to WP-5's transient-error rework: a single
// stage failure no longer stamps phase Failed — it keeps the current phase
// and reports Progressing=False, only escalating to Failed after
// devEnvFailureThreshold (5) CONSECUTIVE failures of the same stage. This
// asserts BOTH halves: the single-failure behavior (namespace/quota still
// created, phase not yet Failed, Progressing=False with reason HelmError)
// and that clearing the error lets it recover to Ready on the very next
// reconcile — proving the retry-without-duplication claim the original test
// was written for.
func TestReconcile_IdempotentReReconcileAfterPartialFailure(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("flaky-env", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)
	helm.InstallOrUpgradeErr = errImmediateFailure

	reconcileOnce(t, r, env.Name)
	got := getEnv(t, r, env.Name)
	if got.Status.Phase == DevEnvPhaseFailed {
		t.Fatalf("phase after a SINGLE failed helm install = %q, want anything but Failed (threshold is %d consecutive failures)", got.Status.Phase, devEnvFailureThreshold)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, ConditionDevEnvProgressing)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "HelmError" {
		t.Fatalf("Progressing condition = %+v, want Status=False Reason=HelmError", cond)
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

// TestReconcile_RepeatedStageFailureEscalatesToFailed guards the other half
// of the transient-error rework: a stage that never recovers must still
// eventually report Failed, exactly at the configured threshold — not one
// reconcile sooner or later.
func TestReconcile_RepeatedStageFailureEscalatesToFailed(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("always-flaky", nil)
	r, helm := newDevEnvTestReconciler(t, clock, env)
	helm.InstallOrUpgradeErr = errImmediateFailure

	for i := 1; i < devEnvFailureThreshold; i++ {
		reconcileOnce(t, r, env.Name)
		got := getEnv(t, r, env.Name)
		if got.Status.Phase == DevEnvPhaseFailed {
			t.Fatalf("phase became Failed after only %d consecutive failure(s), want it to stay non-Failed until %d", i, devEnvFailureThreshold)
		}
	}

	reconcileOnce(t, r, env.Name)
	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseFailed {
		t.Fatalf("phase after %d consecutive failures = %q, want %q", devEnvFailureThreshold, got.Status.Phase, DevEnvPhaseFailed)
	}
}

// TestReconcile_UnchangedReadyEnvironmentMakesNoStatusWrites is the hot-loop
// regression test: reconciling an already-Ready, otherwise-unchanged
// environment a second time must not issue a single Status().Patch call —
// statusFieldsEqual's diff-gate (patchStatusFields) and setDevEnvPhase's/
// setDevEnvCondition's own no-op guards together should make the whole pass
// a read-only no-op once nothing has actually changed. Before this rework,
// status.lastReconciledAt alone guaranteed a write (and therefore a
// self-inflicted watch event) on literally every reconcile.
func TestReconcile_UnchangedReadyEnvironmentMakesNoStatusWrites(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("steady-state", nil)
	r, _ := newDevEnvTestReconciler(t, clock, env)

	reconcileOnce(t, r, env.Name)
	if got := getEnv(t, r, env.Name); got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase after first reconcile = %q, want Ready", got.Status.Phase)
	}

	watchClient, ok := r.Client.(client.WithWatch)
	if !ok {
		t.Fatalf("test harness's fake client %T does not implement client.WithWatch", r.Client)
	}
	var statusPatchCount int
	r.Client = interceptor.NewClient(watchClient, interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			if subResourceName == "status" {
				statusPatchCount++
			}
			return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
		},
	})

	reconcileOnce(t, r, env.Name)

	if statusPatchCount != 0 {
		t.Errorf("Status().Patch called %d time(s) on an unchanged Ready environment, want 0", statusPatchCount)
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
	want := created.Add(orbitapi.DevEnvTTLHardCap)
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

	// Sleep is automatic on idle, but wake is only ever explicit — reconciling
	// again without touching spec.Awake must NOT wake it.
	reconcileOnce(t, r, env.Name)
	if got := getEnv(t, r, env.Name); got.Status.Phase != DevEnvPhaseSleeping {
		t.Fatalf("phase stayed asleep without explicit wake = %q, want Sleeping", got.Status.Phase)
	}

	// Explicit wake: flip spec.Awake false->true (simulating the UI/API
	// action) and reconcile again.
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

	woken := &appsv1.Deployment{}
	if err := r.Get(context.Background(), types.NamespacedName{Name: "some-workload", Namespace: "orbit-devenv-idle-env"}, woken); err != nil {
		t.Fatalf("get woken deployment: %v", err)
	}
	if woken.Spec.Replicas == nil || *woken.Spec.Replicas != 3 {
		t.Errorf("replicas after wake = %v, want 3 (restored)", woken.Spec.Replicas)
	}
	if _, ok := woken.Annotations[preSleepReplicasAnnotation]; ok {
		t.Error("pre-sleep replicas annotation should be removed after restore")
	}
	if got := getEnv(t, r, env.Name); got.Status.Phase != DevEnvPhaseReady {
		t.Fatalf("phase after explicit wake = %q, want Ready", got.Status.Phase)
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
	// Regression test for a real gap: a vcluster's syncer only mirrors Pods
	// onto the host by default, so the Deployment/StatefulSet controllers
	// that own an environment's real workloads (Postgres, Redis, istiod, the
	// in-vcluster orbit-operator, ...) live entirely inside the vcluster's
	// own API server. scaleWorkloads, which only ever listed the HOST
	// namespace, could never reach them — "sleep" stopped the vcluster
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
	// namespace (istio-system, alongside the shared Gateway), so it needs
	// its own explicit-cleanup assertion: namespace deletion alone would not
	// remove it.
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

	// App-ingress cleanup: the per-environment ServiceEntry goes, and with
	// the last environment gone the aggregated routing VirtualService goes
	// too.
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

// TestReconcile_InstallsInVClusterPlatformOnceReady and
// TestReconcile_NoHostOrbitPlatformFailsInVClusterInstall land here (rather
// than devenvironment_platform_test.go, where the rest of that file's tests
// live) because they exercise the full reconcileReady flow through this
// package's shared test harness — see the comment left in
// devenvironment_platform_test.go pointing here.

func TestReconcile_InstallsInVClusterPlatformOnceReady(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("bob-env", nil)
	r, _, vcClient, inVClusterHelm := newDevEnvTestReconcilerFull(t, clock, env)

	reconcileOnce(t, r, env.Name)

	if inVClusterHelm.InstallOrUpgradeCalls == 0 {
		t.Fatal("expected the in-vcluster orbit-operator chart to be installed")
	}

	platform := &OrbitPlatform{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterOrbitPlatformName}, platform); err != nil {
		t.Fatalf("in-vcluster OrbitPlatform not applied: %v", err)
	}
	if platform.Spec.SPIRE.Enabled {
		t.Error("in-vcluster OrbitPlatform must have spire.enabled=false (identity is host-provided)")
	}
	if !platform.Spec.Istio.Enabled {
		t.Error("in-vcluster OrbitPlatform must have istio.enabled=true")
	}
	if platform.Spec.TrustDomain != "orbit.test" {
		t.Errorf("TrustDomain = %q, want the HOST OrbitPlatform's trust domain %q", platform.Spec.TrustDomain, "orbit.test")
	}
	if platform.Spec.Config.DB.Host == "" || platform.Spec.Config.Redis.Host == "" {
		t.Error("in-vcluster OrbitPlatform must point at the fixture Postgres/Redis hosts")
	}
	// The in-vcluster istiod must be able to validate HOST-signed workload
	// tokens: the host issuer/JWKS ride the CR spec.
	if !platform.Spec.MeshJWTValidation.Enabled {
		t.Error("in-vcluster OrbitPlatform must have meshJWTValidation.enabled=true")
	}
	if platform.Spec.MeshJWTValidation.Issuer == "" || platform.Spec.MeshJWTValidation.JWKS == "" {
		t.Errorf("meshJWTValidation must carry the host issuer and JWKS, got issuer=%q jwks=%q",
			platform.Spec.MeshJWTValidation.Issuer, platform.Spec.MeshJWTValidation.JWKS)
	}

	// The in-vcluster frontend/identity need their own environment's ID
	// (not baked into the shared image) — threaded as a chart values
	// override, same mechanism as frontendHost.
	orbitValues, ok := platform.Spec.OrbitValues["frontend"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"frontend\"] missing or wrong type: %#v", platform.Spec.OrbitValues["frontend"])
	}
	frontendConfig, ok := orbitValues["config"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"frontend\"][\"config\"] missing or wrong type: %#v", orbitValues["config"])
	}
	if frontendConfig["devEnvironmentId"] != env.Name {
		t.Errorf("OrbitValues frontend.config.devEnvironmentId = %v, want %q", frontendConfig["devEnvironmentId"], env.Name)
	}
	identityValues, ok := platform.Spec.OrbitValues["identity"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"identity\"] missing or wrong type: %#v", platform.Spec.OrbitValues["identity"])
	}
	identityConfig, ok := identityValues["config"].(map[string]any)
	if !ok {
		t.Fatalf("OrbitValues[\"identity\"][\"config\"] missing or wrong type: %#v", identityValues["config"])
	}
	if identityConfig["devEnvironmentId"] != env.Name {
		t.Errorf("OrbitValues identity.config.devEnvironmentId = %v, want %q", identityConfig["devEnvironmentId"], env.Name)
	}

	// Every orbit-chart service image pinned to the resolved ref, never a
	// baked chart default.
	for _, svc := range inVClusterServiceImageServices {
		values, ok := platform.Spec.OrbitValues[svc].(map[string]any)
		if !ok {
			t.Fatalf("OrbitValues[%q] missing or wrong type: %#v", svc, platform.Spec.OrbitValues[svc])
		}
		image, ok := values["image"].(map[string]any)
		if !ok || image["tag"] != "main-abc1234" {
			t.Errorf("OrbitValues[%q].image.tag = %v, want %q (the harness's resolved ref)", svc, image["tag"], "main-abc1234")
		}
	}

	tenant := &OrbitTenant{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterOrbitTenantName}, tenant); err != nil {
		t.Fatalf("in-vcluster OrbitTenant not applied: %v", err)
	}
	if tenant.Spec.OIDC.IssuerURL == "" {
		t.Error("seeded OrbitTenant must have an OIDC issuer URL (the fixture Dex)")
	}

	secret := &corev1.Secret{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: "orbit-secrets", Namespace: "orbit-system"}, secret); err != nil {
		t.Fatalf("orbit-secrets not created inside the vcluster: %v", err)
	}
	if len(secret.Data["ORBIT_TOKEN_SIGNING_KEY"]) == 0 {
		t.Error("orbit-secrets missing ORBIT_TOKEN_SIGNING_KEY")
	}

	// The devenv-admin ClusterRoleBinding must exist so decision E2's
	// kubeconfig subject (orbit:devenv-admin) can actually authorize
	// anything inside the vcluster once WP-10 mints one. The binding's own
	// shape (Subjects/RoleRef) is asserted directly in
	// devenvironment_platform_test.go's
	// TestEnsureInVClusterDevEnvAdminBinding_CreatesGroupBoundToClusterAdmin
	// — this just confirms reconcileInVClusterPlatform actually calls it.
	crb := &rbacv1.ClusterRoleBinding{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterDevEnvAdminBindingName}, crb); err != nil {
		t.Fatalf("devenv-admin ClusterRoleBinding not created: %v", err)
	}

	// The fixture Postgres must be migrated or orbit-identity cannot boot —
	// a goose Job using the operator's own image. The fake client runs no
	// Deployment controller, so the fixture Postgres Deployment this first
	// reconcile created never gains a Ready replica on its own — the
	// migration Job must therefore still be absent.
	job := &batchv1.Job{}
	jobErr := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, job)
	if jobErr == nil {
		t.Fatal("migration Job created before the fixture Postgres reported a Ready replica")
	}

	postgres := &appsv1.Deployment{}
	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterPostgresName, Namespace: inVClusterFixturesNamespace}, postgres); err != nil {
		t.Fatalf("fixture postgres deployment not created: %v", err)
	}
	postgres.Status.ReadyReplicas = 1
	if err := vcClient.Status().Update(context.Background(), postgres); err != nil {
		t.Fatalf("mark fixture postgres ready: %v", err)
	}

	reconcileOnce(t, r, env.Name)

	if err := vcClient.Get(context.Background(), client.ObjectKey{Name: inVClusterMigrationsJobName, Namespace: "orbit-system"}, job); err != nil {
		t.Fatalf("migration Job not created inside the vcluster once postgres was ready: %v", err)
	}
	// resolvedRef pins this to the harness's fake tag, not :latest — see
	// devenvironment_ref.go/inVClusterOperatorImage.
	if img := job.Spec.Template.Spec.Containers[0].Image; img != "orbit-registry:5001/orbit-operator:main-abc1234" {
		t.Errorf("migration Job image = %q, want the operator's own registry-prefixed image at the resolved ref", img)
	}
	if inject := job.Spec.Template.Annotations["sidecar.istio.io/inject"]; inject != "false" {
		t.Error("migration Job pod must opt out of sidecar injection or it never completes")
	}

	got := getEnv(t, r, env.Name)
	found := false
	for _, c := range got.Status.Conditions {
		if c.Type == ConditionDevEnvPlatformApplied && c.Status == metav1.ConditionTrue {
			found = true
		}
	}
	if !found {
		t.Error("expected InVClusterPlatformApplied condition to be True")
	}
}

// TestReconcile_NoHostOrbitPlatformFailsInVClusterInstall adapts the
// reference's single-reconcile "phase = Failed" assertion to WP-5's
// transient-error rework: hostTrustDomain's lookup failure propagates as an
// ordinary retryable "InVClusterPlatformError" stage failure, so phase only
// reaches Failed once the SAME stage has failed devEnvFailureThreshold
// times in a row — this is not classified as a non-retryable cause (unlike
// an unresolvable spec.ref, which sets Failed immediately).
func TestReconcile_NoHostOrbitPlatformFailsInVClusterInstall(t *testing.T) {
	clock := newFakeClock(time.Now())
	env := newTestEnv("no-host-platform", nil)
	r, _, _, _ := newDevEnvTestReconcilerFull(t, clock, env)

	// Remove the host OrbitPlatform newDevEnvTestReconcilerFull seeds by
	// default — hostTrustDomain has nothing to read from.
	hostPlatform := &OrbitPlatform{ObjectMeta: metav1.ObjectMeta{Name: "orbit"}}
	if err := r.Delete(context.Background(), hostPlatform); err != nil {
		t.Fatalf("delete seeded host OrbitPlatform: %v", err)
	}

	for i := 1; i < devEnvFailureThreshold; i++ {
		reconcileOnce(t, r, env.Name)
		if got := getEnv(t, r, env.Name); got.Status.Phase == DevEnvPhaseFailed {
			t.Fatalf("phase became Failed after only %d consecutive failure(s), want it to stay non-Failed until %d", i, devEnvFailureThreshold)
		}
	}
	reconcileOnce(t, r, env.Name)

	got := getEnv(t, r, env.Name)
	if got.Status.Phase != DevEnvPhaseFailed {
		t.Errorf("phase after %d consecutive failures = %q, want %q (no host trust domain to derive)", devEnvFailureThreshold, got.Status.Phase, DevEnvPhaseFailed)
	}
}
