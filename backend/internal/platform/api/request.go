package api

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

// trustedHostConfig is the immutable allowlist of public hostnames that
// host-derived discovery metadata may reflect, plus the canonical fallback host.
type trustedHostConfig struct {
	hosts     map[string]bool
	canonical string
}

// trustedHostCfg is set once at startup (nil = validation disabled). Read
// lock-free on every RequestBaseURL call.
var trustedHostCfg atomic.Pointer[trustedHostConfig]

// ConfigureTrustedHosts sets the allowlist of public hostnames that host-derived
// discovery metadata (OAuth PRM/AS metadata, the 401 challenge) may reflect
// (ADR 2026-07-04). The first entry is the canonical host used as a fallback when
// a request's derived host is not in the allowlist — so a forged X-Forwarded-Host
// is never reflected into metadata. An empty list disables validation (local dev
// and tests reflect the request host as-is).
func ConfigureTrustedHosts(hosts []string) {
	if len(hosts) == 0 {
		trustedHostCfg.Store(nil)
		return
	}
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[h] = true
	}
	trustedHostCfg.Store(&trustedHostConfig{hosts: m, canonical: hosts[0]})
}

// ConfigureTrustedHostsFromEnv configures the allowlist from ORBIT_PUBLIC_HOSTS
// (comma-separated hostnames, canonical first). No-op when unset. Call once at
// service startup, before serving.
func ConfigureTrustedHostsFromEnv() {
	raw := os.Getenv("ORBIT_PUBLIC_HOSTS")
	if raw == "" {
		return
	}
	var hosts []string
	for h := range strings.SplitSeq(raw, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	ConfigureTrustedHosts(hosts)
}

// enforceTrustedHost returns host unchanged if its hostname is in the configured
// allowlist (or if no allowlist is configured); otherwise it returns the canonical
// host, so a client-supplied (forged) host is never reflected into metadata.
func enforceTrustedHost(host string) string {
	cfg := trustedHostCfg.Load()
	if cfg == nil {
		return host
	}
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	if cfg.hosts[hostname] {
		return host
	}
	return cfg.canonical
}

// RequestBaseURL returns the public scheme://host the client used to reach this
// service, honoring the ingress's X-Forwarded-* headers (Istio terminates TLS
// and forwards the original host). It lets services build absolute URLs —
// OAuth/OIDC discovery metadata, redirect URIs — from the request rather than
// hardcoding a host, so the same binary is correct at whatever host it is exposed.
//
// SECURITY: the resolved host is client-influenceable (Host / X-Forwarded-Host).
// Responses that embed it MUST be Cache-Control: no-store (never shared-cached),
// or a poisoned cached copy could redirect other clients. Per-request injection
// is closed by the trusted-host allowlist (ConfigureTrustedHosts / ADR 2026-07-04):
// once configured, a host outside the allowlist is replaced with the canonical
// host, so a forged X-Forwarded-Host is never reflected into metadata.
func RequestBaseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + enforceTrustedHost(host)
}

// AppBaseURL derives the app/frontend origin from an API-host origin by stripping
// the leading "api." label — the north-south exposure convention where
// api.<domain> is the unified backend API host and <domain> is the app host
// (ADR 2026-07-04). A host with no "api." prefix (e.g. a request that already
// arrived on the app host) is returned unchanged. Used to point OAuth discovery
// metadata's authorization_endpoint at the browser-facing consent page, which
// lives on the app host rather than the API host that serves the metadata.
func AppBaseURL(apiBase string) string {
	u, err := url.Parse(apiBase)
	if err != nil {
		return apiBase
	}
	u.Host = strings.TrimPrefix(u.Host, "api.")
	return u.String()
}
