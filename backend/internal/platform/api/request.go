package api

import "net/http"

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
