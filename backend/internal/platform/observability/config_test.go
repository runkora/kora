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

// The tenant collector falls back to the service's own endpoint, and
// CollectorAuthority follows the tenant value when both are set.
func TestTenantOTLPEndpoint(t *testing.T) {
	saved := *DefaultConfig
	t.Cleanup(func() { *DefaultConfig = saved })

	DefaultConfig.OTLPEndpoint = "http://own:4317"
	DefaultConfig.TenantOTLPEndpoint = ""
	if got := TenantOTLPEndpoint(); got != "http://own:4317" {
		t.Errorf("TenantOTLPEndpoint() = %q, want the service's own endpoint", got)
	}

	DefaultConfig.TenantOTLPEndpoint = "http://global:4317"
	if got := CollectorAuthority(); got != "global:4317" {
		t.Errorf("CollectorAuthority() = %q, want global:4317", got)
	}
}

func TestApplyGoSetEnv(t *testing.T) {
	env := map[string]string{"ORBIT_OTLP_ENDPOINT": "http://collector:4317", "ORBIT_SAMPLING_RATE": "0.25"}
	cfg := &ObservabilityConfig{OTLPEndpoint: "from-file"}
	if err := applyGoSetEnv(cfg, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("applyGoSetEnv() error: %v", err)
	}
	if cfg.OTLPEndpoint != "http://collector:4317" || cfg.SamplingRate == nil || *cfg.SamplingRate != 0.25 {
		t.Errorf("got endpoint %q, sampling %v", cfg.OTLPEndpoint, cfg.SamplingRate)
	}

	unset := &ObservabilityConfig{OTLPEndpoint: "from-file"}
	if err := applyGoSetEnv(unset, func(string) string { return "" }); err != nil || unset.OTLPEndpoint != "from-file" {
		t.Errorf("unset env changed the file value: %q, %v", unset.OTLPEndpoint, err)
	}

	bad := map[string]string{"ORBIT_SAMPLING_RATE": "most"}
	if err := applyGoSetEnv(&ObservabilityConfig{}, func(k string) string { return bad[k] }); err == nil {
		t.Error("a malformed ORBIT_SAMPLING_RATE must fail")
	}
}
