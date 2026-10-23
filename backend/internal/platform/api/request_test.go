package api

import (
	"net/http"
	"testing"
)

func TestRequestBaseURL_TrustedHostAllowlist(t *testing.T) {
	t.Cleanup(func() { ConfigureTrustedHosts(nil) }) // reset global

	req := func(fwdHost string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", fwdHost)
		return r
	}

	// Unset allowlist → reflect the request host as-is (dev/tests).
	ConfigureTrustedHosts(nil)
	if got := RequestBaseURL(req("evil.com")); got != "https://evil.com" {
		t.Errorf("no allowlist: got %q, want reflected host", got)
	}

	// Configured allowlist, canonical first.
	ConfigureTrustedHosts([]string{"api.example.com", "example.com"})

	// Trusted host (incl. port) → reflected.
	if got := RequestBaseURL(req("api.example.com")); got != "https://api.example.com" {
		t.Errorf("trusted host: got %q", got)
	}
	if got := RequestBaseURL(req("api.example.com:8443")); got != "https://api.example.com:8443" {
		t.Errorf("trusted host with port: got %q", got)
	}
	if got := RequestBaseURL(req("example.com")); got != "https://example.com" {
		t.Errorf("second trusted host: got %q", got)
	}
	// Forged host → canonical fallback (never reflected).
	if got := RequestBaseURL(req("evil.com")); got != "https://api.example.com" {
		t.Errorf("forged host: got %q, want canonical fallback", got)
	}
	if got := RequestBaseURL(req("api.example.com.evil.com")); got != "https://api.example.com" {
		t.Errorf("lookalike host: got %q, want canonical fallback", got)
	}
}

func TestAppBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.local.runkora.io", "https://local.runkora.io"},
		{"https://api.example.com:8443", "https://example.com:8443"},
		{"http://api.foo", "http://foo"},
		// No api. prefix (request already on the app host) — unchanged.
		{"https://local.runkora.io", "https://local.runkora.io"},
		// "api." only as a prefix of the leading label, not mid-host.
		{"https://api.apiary.example.com", "https://apiary.example.com"},
	}
	for _, tc := range cases {
		if got := AppBaseURL(tc.in); got != tc.want {
			t.Errorf("AppBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
