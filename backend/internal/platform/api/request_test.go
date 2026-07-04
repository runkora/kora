package api

import "testing"

func TestAppBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.orbit.local.clearroute.io", "https://orbit.local.clearroute.io"},
		{"https://api.orbit.example.com:8443", "https://orbit.example.com:8443"},
		{"http://api.foo", "http://foo"},
		// No api. prefix (request already on the app host) — unchanged.
		{"https://orbit.local.clearroute.io", "https://orbit.local.clearroute.io"},
		// "api." only as a prefix of the leading label, not mid-host.
		{"https://api.apiary.example.com", "https://apiary.example.com"},
	}
	for _, tc := range cases {
		if got := AppBaseURL(tc.in); got != tc.want {
			t.Errorf("AppBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
