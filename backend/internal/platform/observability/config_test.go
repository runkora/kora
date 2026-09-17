package observability

import "testing"

// otlpTarget is the compatibility contract with the OpenTelemetry SDKs: the
// scheme decides the transport, and an endpoint without one is rejected rather
// than guessed at. Guessing either fails the handshake against a plaintext
// collector or ships telemetry in the clear to a TLS one.
func TestOTLPTarget(t *testing.T) {
	cases := map[string]struct {
		endpoint     string
		wantAddr     string
		wantInsecure bool
		wantErr      bool
	}{
		"http is plaintext":       {"http://collector:4317", "collector:4317", true, false},
		"https is TLS":            {"https://collector:4317", "collector:4317", false, false},
		"trailing slash trimmed":  {"http://collector:55680/", "collector:55680", true, false},
		"in-cluster service":      {"http://otel.observability.svc:4317", "otel.observability.svc:4317", true, false},
		"no scheme rejected":      {"collector:4317", "", false, true},
		"empty rejected":          {"", "", false, true},
		"unknown scheme rejected": {"grpc://collector:4317", "", false, true},
		"bare scheme rejected":    {"http://", "", false, true},
		"bare scheme with slash":  {"https://", "", false, true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			addr, insecure, err := otlpTarget(c.endpoint)
			if c.wantErr {
				if err == nil {
					t.Fatalf("otlpTarget(%q) = %q, want an error", c.endpoint, addr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if addr != c.wantAddr {
				t.Errorf("addr = %q, want %q", addr, c.wantAddr)
			}
			if insecure != c.wantInsecure {
				t.Errorf("insecure = %v, want %v", insecure, c.wantInsecure)
			}
		})
	}
}
