package api

import "net/http"

// RequestBaseURL returns the public scheme://host the client used to reach this
// service, honoring the ingress's X-Forwarded-* headers (Istio terminates TLS
// and forwards the original host). It lets services build absolute URLs —
// OAuth/OIDC discovery metadata, redirect URIs — from the request rather than
// hardcoding a host, so the same binary is correct at whatever host it is exposed.
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
