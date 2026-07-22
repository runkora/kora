// Host-side routing to each environment's browser-facing Orbit UI/API — the
// path-based app ingress.
//
// Every environment's app is served from ONE shared hostname (the host part
// of AppExternalURL) with the environment ID as a path prefix:
//
//	{AppExternalURL}/{env-id}  →  that environment's own in-vcluster
//	                              ingress gateway (synced onto the host as a
//	                              plain Service in the env's namespace)
//
// One hostname needs one DNS record and one TLS certificate, versus the
// wildcard DNS + wildcard certificate a subdomain-per-environment scheme
// would demand on real infrastructure. Unlike the kubectl API route
// (devenvironment_ingress.go), which is L4 TLS passthrough matched on SNI, a
// path prefix only exists inside the HTTP request line — so the shared
// gateway terminates TLS here and routes at L7.
//
// The per-environment prefix cannot be baked into the environment's own
// frontend build: every environment installs the SAME image, and Next.js's
// basePath is a build-time setting — so the frontend itself has to be
// prefix-aware at runtime (reading X-Forwarded-Prefix and threading it
// through every internally-constructed URL — AppLink, usePrefixedRouter,
// api-routes.ts, the OIDC redirect_uri) rather than relying on any
// path-rewriting trick here. A prefixed request (/{env-id}, /{env-id}/*) is
// routed by path: the prefix is stripped (the environment serves "/"
// unprefixed, identical bits everywhere) and recorded in X-Forwarded-Prefix
// so the frontend can restore it on every URL it builds afterward.
//
// This once had a routing-cookie fallback for requests the frontend couldn't
// yet prefix itself (assets, client-side navigation, the OIDC callback) —
// removed once the frontend became fully prefix-aware and staging traffic
// confirmed the fallback route was never hit (pinned decision 14: an
// unscoped routing cookie is a hijack vector, kept only as long as something
// genuinely still depended on it).
//
// All app routes for all environments live in ONE aggregated VirtualService,
// not one per environment: Istio's merge order across multiple
// VirtualServices bound to the same host is undefined, so a per-environment
// split could evaluate routes in an order this package does not control. A
// single VirtualService makes the precedence deterministic: every
// environment's Dex routes precede its own app-prefix route (pinned decision
// 12). Rebuilt from the full OrbitDevEnvironment list on every reconcile
// (same pattern as PlatformReconciler.tenantNamespaces); safe because the
// controller runs single-threaded reconciles.
package operator

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/clear-route/orbit/internal/platform/orbitapi"
)

const (
	// hostOrbitGatewayName is the platform's OWN shared Gateway
	// (charts/orbit-platform/templates/gateway.yaml, in systemNamespace),
	// which already carries AppExternalURL's host among its servers — it is
	// the same Gateway the platform frontend's own VirtualService attaches
	// to. The aggregated devenv app VirtualService attaches here too, rather
	// than to a Gateway of its own: two Gateway resources that select the
	// same ingress-gateway workload with server blocks on the same host and
	// port silently conflict (Istio binds one, drops the other), so a
	// devenv-only Gateway sharing orbit-gateway's host never actually served
	// traffic (pinned decision 11).
	hostOrbitGatewayName = "orbit-gateway"

	// legacyDevEnvAppGatewayName is the now-unused Gateway name from before
	// the fix above. Kept only so ensureDevEnvAppAccess can delete any
	// leftover copy on clusters that reconciled with the old code (pinned
	// decision 11).
	legacyDevEnvAppGatewayName = "devenv-app-gateway"

	// devEnvAppVirtualServiceName names the ONE aggregated VirtualService
	// carrying every environment's app routes (see the package comment for
	// why they cannot be split per environment).
	devEnvAppVirtualServiceName = "devenv-app-routes"

	// devEnvAppGatewayPort is the environment gateway's plain-HTTP service
	// port on the synced Service (the in-vcluster orbit-platform install runs
	// with gateway TLS disabled — TLS for real infrastructure terminates once
	// at the SHARED gateway, never per environment).
	devEnvAppGatewayPort = 80
)

// appHostname is the shared hostname all environments' app traffic is served
// on — the host part of AppExternalURL, without any port.
func (r *DevEnvironmentReconciler) appHostname() (string, error) {
	u, err := url.Parse(r.AppExternalURL)
	if err != nil {
		return "", fmt.Errorf("parse AppExternalURL %q: %w", r.AppExternalURL, err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("AppExternalURL %q has no hostname", r.AppExternalURL)
	}
	return u.Hostname(), nil
}

// appURL is the environment's browser-facing entry point, published as
// status.appURL: the shared base URL plus the environment ID as a path
// segment. status.ingressHost stays the kubectl route's SNI value and
// nothing else.
func (r *DevEnvironmentReconciler) appURL(env *OrbitDevEnvironment) string {
	return strings.TrimSuffix(r.AppExternalURL, "/") + "/" + env.Name
}

// appAuthCallbackURL is the browser-facing OIDC redirect URI registered on
// this environment's Dex client (devenvironment_fixtures.go): the shared
// app-ingress host, path-prefixed with this environment's own ID
// ({AppExternalURL}/{env-id}/auth/callback), exactly like appDexIssuerURL
// below. The frontend builds the matching redirect_uri client-side by
// reading its own current path prefix (getClientDevEnvPrefix,
// frontend/src/lib/oidc-client.ts) — both sides must agree, or Dex rejects
// the token exchange with a redirect_uri mismatch. This used to be
// unprefixed, host-scoped, and routed back to the right environment by the
// (now-removed) routing cookie; the frontend becoming prefix-aware made
// that unnecessary — the callback now travels through the same general
// /{env-id}/* prefix route as everything else the environment serves.
func (r *DevEnvironmentReconciler) appAuthCallbackURL(env *OrbitDevEnvironment) string {
	return strings.TrimSuffix(r.AppExternalURL, "/") + "/" + env.Name + "/auth/callback"
}

// appDexIssuerURL is the browser- (and in-vcluster-identity-service-)
// reachable OIDC issuer for one environment's fixture Dex:
// {AppExternalURL}/{env-id}/dex. Every caller of Dex (the browser completing
// signinRedirect, and the in-vcluster identity service's own discovery/JWKS
// fetch) always has the environment ID in hand, so the path prefix alone is
// enough. Threaded into both
// the fixture Dex's own "issuer:" config (devenvironment_fixtures.go) and the
// seeded OrbitTenant's OIDC issuer (devenvironment_platform.go) — they must
// agree exactly, or OIDC discovery's issuer check fails.
//
// Dex's issuer used to be the in-vcluster DNS name
// (http://orbit-dex.orbit-devenv-fixtures:5556/dex), unreachable from a
// laptop browser. AppExternalURL's host is already resolvable from inside
// the host cluster (and, by the ordinary DNS-forwarding chain, from inside a
// vcluster's own pods too), so routing Dex through the same shared
// app-ingress host that already serves the rest of the environment's UI
// makes the issuer universally reachable without a second hostname or
// certificate.
func (r *DevEnvironmentReconciler) appDexIssuerURL(env *OrbitDevEnvironment) string {
	return strings.TrimSuffix(r.AppExternalURL, "/") + "/" + env.Name + "/dex"
}

// devEnvAppBackendHost is the host-side cluster-DNS name of an environment's
// own ingress gateway: the in-vcluster "gateway" Service in istio-system,
// synced onto the host under vcluster's translated naming (the same
// {name}-x-{namespace}-x-{release} convention devenvironment_identity.go
// relies on for the mirrored CA-root ConfigMap).
func devEnvAppBackendHost(ns string) string {
	return fmt.Sprintf("gateway-x-istio-system-x-%s.%s.svc.cluster.local", vclusterReleaseName, ns)
}

// devEnvDexBackendHost is the host-side cluster-DNS name of an environment's
// fixture Dex: the in-vcluster "orbit-dex" Service in
// inVClusterFixturesNamespace, synced onto the host under the same
// vcluster-translated naming as devEnvAppBackendHost.
func devEnvDexBackendHost(ns string) string {
	return fmt.Sprintf("%s-x-%s-x-%s.%s.svc.cluster.local", inVClusterDexName, inVClusterFixturesNamespace, vclusterReleaseName, ns)
}

// ensureDevEnvAppAccess ensures this environment's backend ServiceEntry and
// the aggregated app-routing VirtualService are attached to the platform's
// own orbit-gateway. Mirrors ensureDevEnvAPIAccess's single-error-branch
// shape (that L4 path legitimately owns its own Gateway — a TLS-passthrough
// listener the platform Gateway has no equivalent server for — so it is not
// subject to the same conflict).
func (r *DevEnvironmentReconciler) ensureDevEnvAppAccess(ctx context.Context, env *OrbitDevEnvironment) error {
	host, err := r.appHostname()
	if err != nil {
		return err
	}
	if err := r.deleteLegacyDevEnvAppGateway(ctx); err != nil {
		return fmt.Errorf("delete legacy devenv app gateway: %w", err)
	}
	if err := r.ensureDevEnvAppServiceEntry(ctx, env); err != nil {
		return fmt.Errorf("ensure devenv app service entry: %w", err)
	}
	if err := r.reconcileDevEnvAppRoutes(ctx, host, ""); err != nil {
		return fmt.Errorf("reconcile devenv app routes: %w", err)
	}
	return nil
}

// deleteLegacyDevEnvAppGateway removes the devenv-only Gateway created by
// the pre-fix code, if a cluster still has one lying around from before
// devenv app routes moved onto orbit-gateway. Tolerates absence, so it is
// safe to call on every reconcile (pinned decision 11).
func (r *DevEnvironmentReconciler) deleteLegacyDevEnvAppGateway(ctx context.Context) error {
	return r.deleteIstioResource(ctx, istioGatewayGVK(), legacyDevEnvAppGatewayName)
}

// ensureDevEnvAppServiceEntry registers the environment's synced gateway and
// Dex Services as mesh-visible hosts. Needed for exactly the same reason as
// the kubectl route's ServiceEntry (see ensureDevEnvAPIServiceEntry): the
// host istiod's discoverySelectors hide devenv namespaces' Services
// entirely, so without this the aggregated VirtualService would route to a
// cluster istiod never configured. MESH_EXTERNAL likewise: both Services'
// istio-proxy (or lack thereof, for Dex) belongs to the ENVIRONMENT's mesh,
// not the host's, so host-side auto-mTLS could never handshake with them —
// both hops are plain HTTP inside the host-fenced environment namespace. One
// ServiceEntry covers both hosts (Istio applies the full ports list to every
// listed host, which is harmless here — neither Service answers the other's
// port) rather than two, since both live in the same per-environment
// namespace and share this object's cleanup lifecycle.
func (r *DevEnvironmentReconciler) ensureDevEnvAppServiceEntry(ctx context.Context, env *OrbitDevEnvironment) error {
	ns := devEnvNamespace(env)
	spec := map[string]any{
		"hosts":      []any{devEnvAppBackendHost(ns), devEnvDexBackendHost(ns)},
		"location":   "MESH_EXTERNAL",
		"resolution": "DNS",
		"ports": []any{
			map[string]any{
				"number":   int64(devEnvAppGatewayPort),
				"name":     "http",
				"protocol": "HTTP",
			},
			map[string]any{
				"number":   int64(inVClusterDexPort),
				"name":     "http-dex",
				"protocol": "HTTP",
			},
		},
	}
	return r.ensureIstioResource(ctx, istioServiceEntryGVK(), devEnvAppVirtualServiceNamePerEnv(env), spec)
}

// devEnvAppVirtualServiceNamePerEnv names the per-environment app
// ServiceEntry ("devenv-app-{env}"). Only the ServiceEntry is per-environment
// on the app path — the VirtualService is aggregated (see package comment) —
// but it shares the naming convention of the kubectl route's per-environment
// objects so cleanup tooling can find both families the same way.
func devEnvAppVirtualServiceNamePerEnv(env *OrbitDevEnvironment) string {
	return "devenv-app-" + env.Name
}

// reconcileDevEnvAppRoutes rebuilds the aggregated app VirtualService from
// the current OrbitDevEnvironment list. exclude names an environment to
// leave out — the deletion path passes the environment being deleted, since
// its CR still exists (finalizer) while its routes must already go.
func (r *DevEnvironmentReconciler) reconcileDevEnvAppRoutes(ctx context.Context, host, exclude string) error {
	envs := &OrbitDevEnvironmentList{}
	if err := r.List(ctx, envs); err != nil {
		return fmt.Errorf("list OrbitDevEnvironments: %w", err)
	}

	names := make([]string, 0, len(envs.Items))
	for i := range envs.Items {
		e := &envs.Items[i]
		if e.Name == exclude || !e.DeletionTimestamp.IsZero() {
			continue
		}
		names = append(names, e.Name)
	}
	sort.Strings(names)

	if len(names) == 0 {
		return r.deleteIstioResource(ctx, istioVirtualServiceGVK(), devEnvAppVirtualServiceName)
	}

	// Dex routes before this environment's own app prefix route: a request
	// under /{env-id}/dex/* would otherwise be captured first by the app
	// prefix route's broader /{env-id}/ match and forwarded, wrongly, to the
	// environment's own frontend gateway instead of Dex (pinned decision 12).
	var routes []any
	for _, name := range names {
		routes = append(routes, devEnvDexRoutes(name)...)
		routes = append(routes, devEnvAppPrefixRoutes(name)...)
	}

	spec := map[string]any{
		"hosts": []any{host},
		// orbit-system/orbit-gateway, NOT a devenv-owned Gateway — see
		// hostOrbitGatewayName. This VirtualService itself still lives in
		// devEnvAPIGatewayNamespace (ensureIstioResource is hardcoded to
		// that namespace); Istio VirtualServices may reference a Gateway in
		// another namespace, and orbit-gateway sets no exportTo restricting
		// that.
		"gateways": []any{systemNamespace + "/" + hostOrbitGatewayName},
		"http":     routes,
	}
	return r.ensureIstioResource(ctx, istioVirtualServiceGVK(), devEnvAppVirtualServiceName, spec)
}

// devEnvAppPrefixRoutes builds one environment's path-routed entry points:
// exact /{env-id} and prefix /{env-id}/, both stripped to "/" before reaching
// the environment (identical bits — the environment serves unprefixed),
// recording the stripped prefix in X-Forwarded-Prefix so the environment's
// own (prefix-aware) frontend can restore it on every URL it builds.
func devEnvAppPrefixRoutes(name string) []any {
	headers := map[string]any{
		"request": map[string]any{
			"set": map[string]any{"X-Forwarded-Prefix": "/" + name},
		},
	}
	exact := map[string]any{
		"name":    "devenv-" + name + "-entry",
		"match":   []any{map[string]any{"uri": map[string]any{"exact": "/" + name}}},
		"rewrite": map[string]any{"uri": "/"},
		"headers": headers,
		"route":   devEnvAppDestination(name),
	}
	prefix := map[string]any{
		"name":    "devenv-" + name + "-prefix",
		"match":   []any{map[string]any{"uri": map[string]any{"prefix": "/" + name + "/"}}},
		"rewrite": map[string]any{"uri": "/"},
		"headers": headers,
		"route":   devEnvAppDestination(name),
	}
	return []any{exact, prefix}
}

func devEnvAppDestination(name string) []any {
	ns := orbitapi.DevEnvNamespacePrefix + name
	return []any{
		map[string]any{
			"destination": map[string]any{
				"host": devEnvAppBackendHost(ns),
				"port": map[string]any{"number": int64(devEnvAppGatewayPort)},
			},
		},
	}
}

// devEnvDexRoutes builds one environment's externally-routable Dex entry
// points: exact /{env-id}/dex and prefix /{env-id}/dex/. No rewrite at all
// (unlike devEnvAppPrefixRoutes, which strips its prefix to "/"): Dex's own
// issuer is the FULL /{env-id}/dex URL (appDexIssuerURL), and Dex mounts
// every one of its own routes (.well-known/openid-configuration, auth,
// token, keys, ...) under its issuer's path, not under a bare "/dex" — a
// request rewritten to "/dex/..." 404s against Dex, while the untouched
// "/{env-id}/dex/..." path succeeds. No X-Forwarded-Prefix header either:
// every caller of these routes (the browser's signinRedirect, the
// in-vcluster identity service's discovery/JWKS fetch) always has the
// environment ID in hand and builds the full /{env-id}/dex/... URL directly.
func devEnvDexRoutes(name string) []any {
	exact := map[string]any{
		"name":  "devenv-" + name + "-dex-entry",
		"match": []any{map[string]any{"uri": map[string]any{"exact": "/" + name + "/dex"}}},
		"route": devEnvDexDestination(name),
	}
	prefix := map[string]any{
		"name":  "devenv-" + name + "-dex-prefix",
		"match": []any{map[string]any{"uri": map[string]any{"prefix": "/" + name + "/dex/"}}},
		"route": devEnvDexDestination(name),
	}
	return []any{exact, prefix}
}

func devEnvDexDestination(name string) []any {
	ns := orbitapi.DevEnvNamespacePrefix + name
	return []any{
		map[string]any{
			"destination": map[string]any{
				"host": devEnvDexBackendHost(ns),
				"port": map[string]any{"number": int64(inVClusterDexPort)},
			},
		},
	}
}

// deleteDevEnvAppRoute removes an environment's app-ingress footprint on
// deletion: its ServiceEntry, and its routes out of the aggregated
// VirtualService (which, like the kubectl route's objects, lives outside the
// environment's namespace and gets no cleanup from the namespace cascade).
// The shared Gateway stays — it is environment-agnostic, like its L4 sibling.
func (r *DevEnvironmentReconciler) deleteDevEnvAppRoute(ctx context.Context, env *OrbitDevEnvironment) error {
	host, err := r.appHostname()
	if err != nil {
		return err
	}
	if err := r.reconcileDevEnvAppRoutes(ctx, host, env.Name); err != nil {
		return err
	}
	if err := r.deleteIstioResource(ctx, istioServiceEntryGVK(), devEnvAppVirtualServiceNamePerEnv(env)); err != nil {
		return fmt.Errorf("delete devenv app ServiceEntry: %w", err)
	}
	return nil
}
