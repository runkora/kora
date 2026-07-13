// Host-side routing to each environment's browser-facing Orbit UI/API —
// the path-based app ingress of the ADR's "Amendment (2026-07-10): Path-Based
// App Ingress for Real Infrastructure"
// (docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
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
// gateway terminates TLS here (or serves plain HTTP on local k3d) and routes
// at L7.
//
// The per-environment prefix cannot be baked into the environment's own
// frontend build: decision 4 installs the SAME image in every environment,
// and Next.js's basePath is a build-time setting — so the environment's
// frontend emits UNPREFIXED asset/navigation URLs (/_next/..., /dashboard)
// that carry no environment ID for the gateway to route on. The amendment
// offers two ways out; the one implemented here is the routing cookie:
//
//   - A prefixed request (/{env-id}, /{env-id}/*) is routed by path: the
//     prefix is stripped (the environment serves "/" unprefixed, identical
//     bits everywhere), X-Forwarded-Prefix records what was stripped, and the
//     response sets a host-scoped routing cookie naming the environment.
//   - Every unprefixed request (assets, client-side navigation, BFF fetches,
//     the OIDC callback) is routed by that cookie instead.
//
// The alternative (X-Forwarded-Prefix + Next.js middleware) cannot actually
// work: middleware rewrites incoming requests, not the asset/link URLs the
// server render embeds in outgoing HTML/RSC payloads, so unprefixed asset
// requests would still reach the gateway with no environment ID anywhere.
//
// Known, accepted limitations of the cookie scheme (documented in the ADR):
// the browser-visible URL goes unprefixed after client-side navigation, so
// deep links must re-enter through /{env-id}; and one browser profile talks
// to one environment at a time — visiting another environment's prefix
// repoints the cookie (two environments need two browser profiles).
//
// All app routes for all environments live in ONE aggregated VirtualService,
// not one per environment: Istio's merge order across multiple
// VirtualServices bound to the same host is undefined, so a per-environment
// split could evaluate environment B's cookie-fallback route before
// environment A's path-prefix route — a stale cookie would then shadow an
// explicit /{env-id} entry and route it to the wrong environment. A single
// VirtualService makes the precedence deterministic: every environment's
// path routes first, every cookie fallback after. Rebuilt from the full
// OrbitDevEnvironment list on every reconcile (same pattern as
// PlatformReconciler.tenantNamespaces); safe because the controller runs
// single-threaded reconciles.
package operator

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
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
	// traffic — the root cause of the app-ingress 404s this replaced.
	hostOrbitGatewayName = "orbit-gateway"

	// legacyDevEnvAppGatewayName is the now-unused Gateway name from before
	// the fix above. Kept only so ensureDevEnvAppAccess can delete any
	// leftover copy on clusters that reconciled with the old code.
	legacyDevEnvAppGatewayName = "devenv-app-gateway"

	// devEnvAppVirtualServiceName names the ONE aggregated VirtualService
	// carrying every environment's app routes (see the package comment for
	// why they cannot be split per environment).
	devEnvAppVirtualServiceName = "devenv-app-routes"

	// devEnvRouteCookieName is the host-scoped cookie the gateway sets on
	// prefixed requests and routes unprefixed requests by. Set and consumed
	// entirely at the gateway — no Orbit service ever reads it.
	devEnvRouteCookieName = "orbit-devenv-route"

	// devEnvAppGatewayPort is the environment gateway's plain-HTTP service
	// port on the synced Service (the in-vcluster orbit-platform install runs
	// with gateway TLS disabled — TLS for real infrastructure terminates once
	// at the SHARED gateway, never per environment).
	devEnvAppGatewayPort = 80

	// devEnvRouteCookieMaxAgeSeconds bounds how long a stale routing cookie
	// can misdirect the shared host's own (non-devenv) traffic. The cookie's
	// match on devEnvAppCookieRoute has no uri constraint — it applies to
	// every path on the shared host, including a plain visit to the real
	// Orbit app — so an unbounded (session-only) cookie left the main app
	// hijacked into a stale environment for as long as the browser session
	// lasted, with no expiry and no indication anything was wrong. Every
	// prefixed request re-sets this cookie (devEnvAppPrefixRoutes), so an
	// actively-used environment's cookie never actually expires underfoot;
	// only an abandoned one does. This is a stopgap, not the fix: the real
	// fix is the frontend becoming prefix-aware so the cookie-fallback route
	// is no longer needed for general navigation at all (see the ADR's
	// 2026-07-13 amendment).
	devEnvRouteCookieMaxAgeSeconds = 900
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
// segment. This is the identifier split the amendment calls for —
// status.ingressHost stays the kubectl route's SNI value and nothing else.
func (r *DevEnvironmentReconciler) appURL(env *OrbitDevEnvironment) string {
	return strings.TrimSuffix(r.AppExternalURL, "/") + "/" + env.Name
}

// appAuthCallbackURL is the browser-facing OIDC redirect URI registered on
// every environment's Dex client (devenvironment_fixtures.go). Host-scoped
// and unprefixed — the frontend builds its redirect_uri from
// window.location.origin, and the routing cookie (not a path prefix) carries
// the environment identity on the callback request.
func (r *DevEnvironmentReconciler) appAuthCallbackURL() string {
	return strings.TrimSuffix(r.AppExternalURL, "/") + "/auth/callback"
}

// appDexIssuerURL is the browser- (and in-vcluster-identity-service-)
// reachable OIDC issuer for one environment's fixture Dex:
// {AppExternalURL}/{env-id}/dex. Unlike the app's own routes, this one is
// NOT the cookie-routed unprefixed scheme — every caller of Dex (the browser
// completing signinRedirect, and the in-vcluster identity service's own
// discovery/JWKS fetch) always has the environment ID in hand, so the path
// prefix alone is enough and no routing cookie is needed. Threaded into both
// the fixture Dex's own "issuer:" config (devenvironment_fixtures.go) and the
// seeded OrbitTenant's OIDC issuer (devenvironment_platform.go) — they must
// agree exactly, or OIDC discovery's issuer check fails.
//
// This resolves the previously-tracked gap (docs/adr/2026-07-07-ephemeral-
// development-environments-via-vcluster.md, "Dex redirect URI" amendment
// note): Dex's issuer used to be the in-vcluster DNS name
// (http://orbit-dex.orbit-devenv-fixtures:5556/dex), unreachable from a
// laptop browser. AppExternalURL's host is already resolvable from inside
// the host cluster (and, by the ordinary DNS-forwarding chain, from inside a
// vcluster's own pods too — see scripts/cluster/dev-cluster-up.sh's CoreDNS
// patch), so routing Dex through the same shared app-ingress host that
// already serves the rest of the environment's UI makes the issuer
// universally reachable without a second hostname or certificate.
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
// safe to call on every reconcile.
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

	// Dex routes before this environment's own app prefix routes (a request
	// under /{env-id}/dex/* would otherwise be captured first by the app
	// prefix route's broader /{env-id}/ match and forwarded, wrongly, to the
	// environment's own frontend gateway instead of Dex) — then, across all
	// environments, cookie fallbacks last so a stale routing cookie can never
	// shadow an explicit /{env-id} entry.
	var routes []any
	for _, name := range names {
		routes = append(routes, devEnvDexRoutes(name)...)
		routes = append(routes, devEnvAppPrefixRoutes(name)...)
	}
	for _, name := range names {
		routes = append(routes, devEnvAppCookieRoute(name))
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
// recording the stripped prefix in X-Forwarded-Prefix and pinning the routing
// cookie for the unprefixed requests that follow.
func devEnvAppPrefixRoutes(name string) []any {
	headers := map[string]any{
		"request": map[string]any{
			"set": map[string]any{"X-Forwarded-Prefix": "/" + name},
		},
		"response": map[string]any{
			// add, not set: the environment's own Set-Cookie headers (the BFF
			// session) must survive alongside the routing cookie.
			"add": map[string]any{
				"Set-Cookie": fmt.Sprintf(
					"%s=%s; Path=/; Max-Age=%d; HttpOnly; SameSite=Lax",
					devEnvRouteCookieName, name, devEnvRouteCookieMaxAgeSeconds,
				),
			},
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

// devEnvAppCookieRoute builds one environment's fallback route: any request
// carrying this environment's routing cookie, forwarded as-is (unprefixed
// asset/navigation/API paths are exactly what the environment serves).
func devEnvAppCookieRoute(name string) map[string]any {
	return map[string]any{
		"name": "devenv-" + name + "-cookie",
		"match": []any{
			map[string]any{
				"headers": map[string]any{
					"cookie": map[string]any{
						// Envoy regex matches are full-string: the cookie may
						// sit anywhere in the header, but its VALUE must end
						// at a ";" or the end of the header — {env-id} being a
						// prefix of another environment's ID can never match.
						"regex": fmt.Sprintf(`(.*;\s*)?%s=%s(\s*;.*)?`, devEnvRouteCookieName, name),
					},
				},
			},
		},
		"route": devEnvAppDestination(name),
	}
}

func devEnvAppDestination(name string) []any {
	ns := "orbit-devenv-" + name
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
// token, keys, ...) under its issuer's path, not under a bare "/dex" —
// confirmed live, a request rewritten to "/dex/..." 404s against Dex,
// while the untouched "/{env-id}/dex/..." path succeeds. No
// X-Forwarded-Prefix/routing-cookie headers either: every caller of these
// routes (the browser's signinRedirect, the in-vcluster identity service's
// discovery/JWKS fetch) always has the environment ID in hand and builds
// the full /{env-id}/dex/... URL directly, unlike the app's own unprefixed
// asset/navigation requests that need the cookie fallback.
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
	ns := "orbit-devenv-" + name
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
