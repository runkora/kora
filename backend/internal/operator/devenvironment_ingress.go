// Host-side routing to each environment's own vcluster API server.
//
// The vcluster's own API server Service (VClusterRestConfig's
// "{release}.{namespace}.svc.cluster.local:443") only exists inside the host
// cluster's network — nothing outside the cluster can reach it directly, and
// kubectl (running on a developer's laptop) needs to. The mechanism:
// SNI-based TLS passthrough on the SAME shared Istio ingress gateway that
// already fronts the app traffic (reconcileGateway, platform_controller.go),
// on a distinct port.
//
// Shape:
//   - One shared, cluster-wide Gateway (devEnvAPIGatewayName) opens a TLS
//     PASSTHROUGH listener on devEnvAPIGatewayPort. PASSTHROUGH means Envoy
//     never terminates the TLS session — it only reads the ClientHello's SNI
//     to pick a route, then relays raw bytes. The vcluster apiserver's own
//     serving certificate (and, critically, the client certificate the
//     connecting kubectl presents) travels untouched end-to-end, so the
//     existing client-cert auth model (devenvironment_csr.go) needs no
//     changes at all.
//   - One VirtualService per environment, matching on SNI == status.ingressHost
//     and forwarding to that environment's vcluster Service. Lives in
//     devEnvAPIGatewayNamespace (istio-system), NOT the environment's own
//     namespace: the host istiod's discoverySelectors (reconcileGateway)
//     exclude orbit.io/role=devenv namespaces from discovery entirely, so a
//     VirtualService placed there is invisible to the istiod instance that
//     actually programs the shared gateway. Cleaned up explicitly on
//     deletion (deleteDevEnvAPIRoute) rather than via namespace cascade, same
//     as the host ClusterSPIFFEIDs (deleteHostIdentity) and for the same
//     underlying reason.
//   - One ServiceEntry per environment (ensureDevEnvAPIServiceEntry),
//     registering the vcluster Service as a mesh-visible, DNS-resolved host.
//     Also necessary: discoverySelectors hide the devenv namespace's actual
//     Kubernetes Services from istiod too, not just its Istio config —
//     without this, the VirtualService's route destination is a cluster
//     istiod never configured, and every connection is accepted (SNI
//     matches) then immediately reset.
//
// Reaching this port at all requires the shared gateway's Service to expose
// it — see reconcileGateway's devEnvAPIGatewayPort wiring (gated on
// spec.SPIRE.Enabled so an in-vcluster gateway release never grows this port
// itself).
package operator

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clear-route/orbit/internal/platform/orbitapi"
)

const (
	// devEnvAPIGatewayName/Namespace name the single, shared Gateway object
	// every environment's VirtualService references. Not templated per
	// environment — there is exactly one shared TLS-passthrough listener.
	devEnvAPIGatewayName      = "devenv-api-gateway"
	devEnvAPIGatewayNamespace = "istio-system"

	// devEnvAPIGatewayPort is the shared port every environment's vcluster
	// API server is reached through from outside the host cluster, SNI-routed
	// per environment. Must match assembleKubeconfig's vclusterAPIExternalPort
	// (internal/devenvironments/service.go) — the two are independently
	// constant rather than plumbed through config because they describe the
	// same fixed protocol detail (which port speaks TLS-passthrough k8s API),
	// not an environment-specific value.
	devEnvAPIGatewayPort = orbitapi.DevEnvAPIGatewayPort
)

// devEnvAPIVirtualServiceName is templated per environment: unlike most
// devenv-owned resources, this VirtualService cannot live in the
// environment's own namespace (see ensureDevEnvAPIRoute) and so cannot rely
// on that namespace's name to disambiguate between environments.
func devEnvAPIVirtualServiceName(env *OrbitDevEnvironment) string {
	return "devenv-api-" + env.Name
}

func istioGatewayGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1beta1", Kind: "Gateway"}
}

func istioServiceEntryGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1beta1", Kind: "ServiceEntry"}
}

func istioVirtualServiceGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1beta1", Kind: "VirtualService"}
}

// ingressHost derives an environment's external hostname the same way
// reconcileReady's status patch does — factored out so ensureDevEnvAPIRoute
// can use the identical value before that patch runs.
func (r *DevEnvironmentReconciler) ingressHost(env *OrbitDevEnvironment) string {
	return fmt.Sprintf("%s.%s", env.Name, r.IngressDomain)
}

// ensureIstioResource creates or updates an Istio config object in
// devEnvAPIGatewayNamespace (istio-system) with the given spec — the shared
// create-or-replace-spec idiom every devenv ingress resource (L4 kubectl
// route and L7 app route alike) follows.
func (r *DevEnvironmentReconciler) ensureIstioResource(ctx context.Context, gvk schema.GroupVersionKind, name string, spec map[string]any) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(gvk)
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: devEnvAPIGatewayNamespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &unstructured.Unstructured{}
		desired.SetGroupVersionKind(gvk)
		desired.SetName(name)
		desired.SetNamespace(devEnvAPIGatewayNamespace)
		if setErr := unstructured.SetNestedMap(desired.Object, spec, "spec"); setErr != nil {
			return fmt.Errorf("build %s %q spec: %w", gvk.Kind, name, setErr)
		}
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create %s %q: %w", gvk.Kind, name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get %s %q: %w", gvk.Kind, name, err)
	default:
		if setErr := unstructured.SetNestedMap(existing.Object, spec, "spec"); setErr != nil {
			return fmt.Errorf("set %s %q spec: %w", gvk.Kind, name, setErr)
		}
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			return fmt.Errorf("update %s %q: %w", gvk.Kind, name, updateErr)
		}
		return nil
	}
}

// deleteIstioResource deletes an Istio config object from
// devEnvAPIGatewayNamespace, tolerating absence.
func (r *DevEnvironmentReconciler) deleteIstioResource(ctx context.Context, gvk schema.GroupVersionKind, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.SetNamespace(devEnvAPIGatewayNamespace)
	if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s %q: %w", gvk.Kind, name, err)
	}
	return nil
}

// ensureDevEnvAPIAccess ensures every piece of external vcluster API
// reachability for one environment: the shared Gateway (idempotent,
// environment-agnostic), this environment's routing VirtualService, and the
// ServiceEntry that makes its destination resolvable despite discoverySelectors
// (see ensureDevEnvAPIServiceEntry). Combined into one call so reconcileReady
// only needs a single error branch.
func (r *DevEnvironmentReconciler) ensureDevEnvAPIAccess(ctx context.Context, env *OrbitDevEnvironment) error {
	if err := r.ensureDevEnvAPIGateway(ctx); err != nil {
		return fmt.Errorf("ensure shared devenv API gateway: %w", err)
	}
	if err := r.ensureDevEnvAPIServiceEntry(ctx, env); err != nil {
		return fmt.Errorf("ensure devenv API service entry: %w", err)
	}
	if err := r.ensureDevEnvAPIRoute(ctx, env); err != nil {
		return fmt.Errorf("ensure devenv API route: %w", err)
	}
	return nil
}

// ensureDevEnvAPIGateway creates or updates the single shared Gateway that
// opens the TLS-passthrough listener every environment's vcluster API route
// attaches to. Idempotent and environment-agnostic — safe to call on every
// reconcile of every environment. This is the ONLY Gateway a dev environment
// owns; the app path (devenvironment_app_ingress.go) deliberately attaches
// to the platform's own existing Gateway instead of creating a second one —
// two Gateways with server blocks on the same host+port silently conflict.
func (r *DevEnvironmentReconciler) ensureDevEnvAPIGateway(ctx context.Context) error {
	spec := map[string]any{
		// Matches the same Istio ingress gateway workload orbit-platform's own
		// app Gateway selects (charts/orbit-platform/templates/gateway.yaml).
		"selector": map[string]any{"app": "gateway"},
		"servers": []any{
			map[string]any{
				"port": map[string]any{
					"number":   int64(devEnvAPIGatewayPort),
					"name":     "tls-devenv-api",
					"protocol": "TLS",
				},
				"tls": map[string]any{"mode": "PASSTHROUGH"},
				// Every environment's ingress host lives under IngressDomain;
				// the per-environment VirtualService narrows this down further
				// by exact SNI match.
				"hosts": []any{"*." + r.IngressDomain},
			},
		},
	}

	return r.ensureIstioResource(ctx, istioGatewayGVK(), devEnvAPIGatewayName, spec)
}

// vclusterAPIHost is the vcluster API server's cluster-DNS name, used both
// as the VirtualService's TLS route destination and the ServiceEntry's own
// registered host (see ensureDevEnvAPIRoute and ensureDevEnvAPIServiceEntry).
func vclusterAPIHost(ns string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", vclusterReleaseName, ns)
}

// ensureDevEnvAPIRoute creates or updates the per-environment VirtualService
// that routes the shared Gateway's TLS-passthrough listener to this
// environment's vcluster API server Service, matched by SNI.
//
// Lives in devEnvAPIGatewayNamespace, NOT the environment's own namespace:
// the host istiod's meshConfig carries discoverySelectors excluding
// orbit.io/role=devenv namespaces entirely (reconcileGateway's own
// discoverySelectors comment explains why — avoiding CA-root ConfigMap
// collisions with each environment's own istiod). That exclusion is
// namespace-wide, not resource-specific: a VirtualService placed in the
// environment's namespace is invisible to the HOST istiod that actually
// programs the shared gateway's listeners, so its SNI route silently never
// takes effect. Since this can't rely on namespace deletion for cleanup,
// deleteDevEnvAPIRoute removes it explicitly from handleDeletion, the same
// way deleteHostIdentity does for the (also namespace-external)
// ClusterSPIFFEIDs (pinned decision 10).
func (r *DevEnvironmentReconciler) ensureDevEnvAPIRoute(ctx context.Context, env *OrbitDevEnvironment) error {
	host := r.ingressHost(env)
	name := devEnvAPIVirtualServiceName(env)

	spec := map[string]any{
		"hosts":    []any{host},
		"gateways": []any{devEnvAPIGatewayNamespace + "/" + devEnvAPIGatewayName},
		"tls": []any{
			map[string]any{
				"match": []any{
					map[string]any{
						"port":     int64(devEnvAPIGatewayPort),
						"sniHosts": []any{host},
					},
				},
				"route": []any{
					map[string]any{
						"destination": map[string]any{
							"host": vclusterAPIHost(devEnvNamespace(env)),
							"port": map[string]any{"number": int64(vclusterAPIPort)},
						},
					},
				},
			},
		},
	}

	return r.ensureIstioResource(ctx, istioVirtualServiceGVK(), name, spec)
}

// ensureDevEnvAPIServiceEntry registers the environment's vcluster API
// Service as a mesh-visible host via a ServiceEntry, resolved by the shared
// gateway's own DNS lookups (resolution: DNS) rather than istiod's normal
// Kubernetes-Service discovery.
//
// Necessary because discoverySelectors don't merely hide devenv
// VirtualServices/Gateways from the host istiod (ensureDevEnvAPIRoute) — they
// hide the devenv namespace's Kubernetes Services too, so istiod never turns
// the vcluster Service into an Envoy cluster for the shared gateway to route
// to at all. Without this, the gateway's tcp_proxy filter would reference a
// cluster name istiod never configured, so every connection would be
// accepted (SNI matched, filter chain selected) and then immediately reset.
// A ServiceEntry is a standalone Istio config object, not derived from
// Service discovery, so it exists regardless of the namespace exclusion —
// and resolution: DNS means Envoy re-resolves the host via cluster DNS
// (CoreDNS, unaffected by discoverySelectors) at connection time instead of
// relying on istiod-pushed endpoints (pinned decision 10).
func (r *DevEnvironmentReconciler) ensureDevEnvAPIServiceEntry(ctx context.Context, env *OrbitDevEnvironment) error {
	name := devEnvAPIVirtualServiceName(env)
	host := vclusterAPIHost(devEnvNamespace(env))

	spec := map[string]any{
		"hosts": []any{host},
		// MESH_EXTERNAL, not MESH_INTERNAL: the vcluster apiserver has no
		// Istio sidecar and does not speak Istio's mTLS protocol, even though
		// it happens to run on a cluster-internal Service address.
		// MESH_INTERNAL would make Envoy treat this as a mesh workload and
		// attempt auto mTLS on top of the already-TLS passthrough bytes — the
		// backend can't participate in that handshake, so every connection
		// would be accepted at the gateway and then immediately reset.
		// MESH_EXTERNAL disables that auto-upgrade, leaving the raw TLS bytes
		// (the client's original handshake with the vcluster apiserver)
		// untouched, which is what PASSTHROUGH mode requires.
		"location":   "MESH_EXTERNAL",
		"resolution": "DNS",
		"ports": []any{
			map[string]any{
				"number":   int64(vclusterAPIPort),
				"name":     "tls",
				"protocol": "TLS",
			},
		},
	}

	return r.ensureIstioResource(ctx, istioServiceEntryGVK(), name, spec)
}

// deleteDevEnvAPIRoute removes an environment's API-routing VirtualService
// and ServiceEntry. Called from handleDeletion — both resources live outside
// the environment's own namespace (see ensureDevEnvAPIRoute and
// ensureDevEnvAPIServiceEntry), so they need explicit cleanup, like the host
// ClusterSPIFFEIDs (deleteHostIdentity).
func (r *DevEnvironmentReconciler) deleteDevEnvAPIRoute(ctx context.Context, env *OrbitDevEnvironment) error {
	name := devEnvAPIVirtualServiceName(env)
	if err := r.deleteIstioResource(ctx, istioVirtualServiceGVK(), name); err != nil {
		return fmt.Errorf("delete devenv API VirtualService: %w", err)
	}
	if err := r.deleteIstioResource(ctx, istioServiceEntryGVK(), name); err != nil {
		return fmt.Errorf("delete devenv API ServiceEntry: %w", err)
	}
	return nil
}
