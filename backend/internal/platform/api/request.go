package api

import (
	"net/http"
	"net/url"
	"strings"
)

// RequestBaseURL returns the public scheme://host the client used to reach this
// service, honoring the ingress's X-Forwarded-* headers (Istio terminates TLS
// and forwards the original host). It lets services build absolute URLs —
// OAuth/OIDC discovery metadata, redirect URIs — from the request rather than
// hardcoding a host, so the same binary is correct at whatever host it is exposed.
//
// SECURITY: the returned host is client-influenceable (Host / X-Forwarded-Host).
// Responses that embed it MUST be Cache-Control: no-store (never shared-cached),
// or a poisoned cached copy could redirect other clients. When these endpoints
// become publicly reachable, the resolved host must additionally be validated
// against a configured allowlist of trusted public hostnames — the ingress does
// not itself guarantee X-Forwarded-Host is trustworthy.
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
	return scheme + "://" + host
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
