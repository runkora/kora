package operator

import (
	"context"
	"io"
	"slices"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/clear-route/orbit/internal/platform/logging"
)

const appIngressTestAppExternalURL = "http://devenv.orbit.test:8880"

func newAppIngressTestReconciler(t *testing.T, objs ...client.Object) *DevEnvironmentReconciler {
	t.Helper()
	scheme := NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &DevEnvironmentReconciler{
		Client:         c,
		Logger:         logging.NewLoggerWithOutput(io.Discard),
		AppExternalURL: appIngressTestAppExternalURL,
	}
}

func newAppIngressTestEnv(name string) *OrbitDevEnvironment {
	return &OrbitDevEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func getAggregatedAppVS(t *testing.T, r *DevEnvironmentReconciler) *unstructured.Unstructured {
	t.Helper()
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(istioVirtualServiceGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppVirtualServiceName, Namespace: devEnvAPIGatewayNamespace}, vs); err != nil {
		t.Fatalf("get aggregated app VirtualService: %v", err)
	}
	return vs
}

func httpRouteNames(t *testing.T, vs *unstructured.Unstructured) []string {
	t.Helper()
	routes, _, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil {
		t.Fatalf("read spec.http: %v", err)
	}
	names := make([]string, len(routes))
	for i, r := range routes {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("http route %d is not a map: %#v", i, r)
		}
		name, _ := m["name"].(string)
		names[i] = name
	}
	return names
}

// TestReconcileDevEnvAppRoutes_AggregatesAcrossEnvironments guards pinned
// decision 12: every environment's app routes live in ONE VirtualService
// rebuilt from the full OrbitDevEnvironment list, not one VS per
// environment — this asserts both environments' routes land in the single
// aggregated object.
func TestReconcileDevEnvAppRoutes_AggregatesAcrossEnvironments(t *testing.T) {
	alice := newAppIngressTestEnv("alice")
	bob := newAppIngressTestEnv("bob")
	r := newAppIngressTestReconciler(t, alice, bob)

	if err := r.reconcileDevEnvAppRoutes(context.Background(), "devenv.orbit.test", ""); err != nil {
		t.Fatalf("reconcileDevEnvAppRoutes: %v", err)
	}

	vs := getAggregatedAppVS(t, r)
	gateways, _, _ := unstructured.NestedStringSlice(vs.Object, "spec", "gateways")
	if len(gateways) != 1 || gateways[0] != systemNamespace+"/"+hostOrbitGatewayName {
		t.Errorf("spec.gateways = %v, want [%s]", gateways, systemNamespace+"/"+hostOrbitGatewayName)
	}

	names := httpRouteNames(t, vs)
	// 2 environments x (2 dex + 2 prefix) = 8. No cookie routes — the
	// routing-cookie fallback was removed once the frontend became
	// prefix-aware (pinned decision 14; see
	// TestReconcileDevEnvAppRoutes_NoRouteEverMatchesOnCookieHeader).
	if len(names) != 8 {
		t.Fatalf("aggregated VirtualService has %d http routes, want 8 (both environments' dex+prefix routes): %v", len(names), names)
	}
	for _, want := range []string{
		"devenv-alice-dex-entry", "devenv-alice-dex-prefix", "devenv-alice-entry", "devenv-alice-prefix",
		"devenv-bob-dex-entry", "devenv-bob-dex-prefix", "devenv-bob-entry", "devenv-bob-prefix",
	} {
		found := slices.Contains(names, want)
		if !found {
			t.Errorf("aggregated VirtualService missing expected route %q, got %v", want, names)
		}
	}
}

// TestReconcileDevEnvAppRoutes_OrderingIsDexThenPrefix guards pinned decision
// 12: within one environment, its Dex routes must precede its general prefix
// route — otherwise the broader /{id}/ prefix match would capture
// /{id}/dex/* requests first and forward them, wrongly, to the environment's
// own frontend instead of Dex.
func TestReconcileDevEnvAppRoutes_OrderingIsDexThenPrefix(t *testing.T) {
	alice := newAppIngressTestEnv("alice")
	bob := newAppIngressTestEnv("bob")
	r := newAppIngressTestReconciler(t, alice, bob)

	if err := r.reconcileDevEnvAppRoutes(context.Background(), "devenv.orbit.test", ""); err != nil {
		t.Fatalf("reconcileDevEnvAppRoutes: %v", err)
	}
	names := httpRouteNames(t, getAggregatedAppVS(t, r))

	indexOf := func(name string) int {
		for i, n := range names {
			if n == name {
				return i
			}
		}
		t.Fatalf("route %q not found in %v", name, names)
		return -1
	}

	for _, env := range []string{"alice", "bob"} {
		dexIdx := indexOf("devenv-" + env + "-dex-prefix")
		prefixIdx := indexOf("devenv-" + env + "-prefix")
		if dexIdx > prefixIdx {
			t.Errorf("environment %q: dex route at %d, prefix route at %d — dex must come first", env, dexIdx, prefixIdx)
		}
	}
}

// TestReconcileDevEnvAppRoutes_NoRouteEverMatchesOnCookieHeader is WP-14's
// regression test: the routing-cookie fallback (pinned decision 14) is gone,
// and must never come back by accident. A cookie-matched route is exactly
// the mechanism that could hijack a cookie-less or stale-cookie request to
// "/" away from the main Orbit app's own catch-all — asserting none of the
// generated routes match on any "cookie" header is the strongest guarantee
// available without a live gateway to actually send such a request through.
func TestReconcileDevEnvAppRoutes_NoRouteEverMatchesOnCookieHeader(t *testing.T) {
	alice := newAppIngressTestEnv("alice")
	r := newAppIngressTestReconciler(t, alice)

	if err := r.reconcileDevEnvAppRoutes(context.Background(), "devenv.orbit.test", ""); err != nil {
		t.Fatalf("reconcileDevEnvAppRoutes: %v", err)
	}
	vs := getAggregatedAppVS(t, r)
	routes, _, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil {
		t.Fatalf("read spec.http: %v", err)
	}
	for _, raw := range routes {
		route, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matches, _, _ := unstructured.NestedSlice(route, "match")
		for _, rawMatch := range matches {
			match, ok := rawMatch.(map[string]any)
			if !ok {
				continue
			}
			if _, hasCookieMatch := match["headers"]; hasCookieMatch {
				headers, _ := match["headers"].(map[string]any)
				if _, ok := headers["cookie"]; ok {
					t.Errorf("route %v matches on a cookie header — the routing-cookie fallback must stay removed", route["name"])
				}
			}
		}
	}
}

// TestReconcileDevEnvAppRoutes_EmptyListDeletesAggregatedVS confirms the
// aggregated VirtualService is removed entirely once no environments remain
// (rather than left behind as an empty, dangling object).
func TestReconcileDevEnvAppRoutes_EmptyListDeletesAggregatedVS(t *testing.T) {
	r := newAppIngressTestReconciler(t)
	if err := r.reconcileDevEnvAppRoutes(context.Background(), "devenv.orbit.test", ""); err != nil {
		t.Fatalf("reconcileDevEnvAppRoutes: %v", err)
	}
	vs := &unstructured.Unstructured{}
	vs.SetGroupVersionKind(istioVirtualServiceGVK())
	err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAppVirtualServiceName, Namespace: devEnvAPIGatewayNamespace}, vs)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected aggregated VirtualService to be absent when no environments remain, got err: %v", err)
	}
}

// TestEnsureDevEnvAppAccess_RemovesLegacyGatewayAndCreatesNoGateway guards
// pinned decision 11: dev-environment app ingress must never create a
// second Istio Gateway (it would silently conflict with the platform's own
// orbit-gateway serving the same host+port) — the app path attaches to the
// existing orbit-gateway instead, and any leftover Gateway from the earlier
// per-environment design is actively cleaned up.
func TestEnsureDevEnvAppAccess_RemovesLegacyGatewayAndCreatesNoGateway(t *testing.T) {
	env := newAppIngressTestEnv("alice")
	legacyGW := &unstructured.Unstructured{}
	legacyGW.SetGroupVersionKind(istioGatewayGVK())
	legacyGW.SetName(legacyDevEnvAppGatewayName)
	legacyGW.SetNamespace(devEnvAPIGatewayNamespace)
	legacyGW.SetUnstructuredContent(map[string]any{
		"apiVersion": "networking.istio.io/v1beta1",
		"kind":       "Gateway",
		"metadata": map[string]any{
			"name":      legacyDevEnvAppGatewayName,
			"namespace": devEnvAPIGatewayNamespace,
		},
		"spec": map[string]any{},
	})

	r := newAppIngressTestReconciler(t, env, legacyGW)
	if err := r.ensureDevEnvAppAccess(context.Background(), env); err != nil {
		t.Fatalf("ensureDevEnvAppAccess: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(istioGatewayGVK())
	err := r.Get(context.Background(), types.NamespacedName{Name: legacyDevEnvAppGatewayName, Namespace: devEnvAPIGatewayNamespace}, got)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("legacy devenv app Gateway should have been deleted, got err: %v", err)
	}

	// ensureDevEnvAppAccess must never create ANY Gateway of its own — only
	// ensureDevEnvAPIAccess (the kubectl L4 path, not exercised here) ever
	// creates devEnvAPIGatewayName, and app ingress attaches to the
	// pre-existing orbit-gateway instead of creating one.
	apiGW := &unstructured.Unstructured{}
	apiGW.SetGroupVersionKind(istioGatewayGVK())
	if err := r.Get(context.Background(), types.NamespacedName{Name: devEnvAPIGatewayName, Namespace: devEnvAPIGatewayNamespace}, apiGW); !apierrors.IsNotFound(err) {
		t.Fatalf("ensureDevEnvAppAccess must not create the kubectl API Gateway, got err: %v", err)
	}
}
