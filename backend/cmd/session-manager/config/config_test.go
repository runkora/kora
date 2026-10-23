package smConfig

import (
	"testing"
	"time"
)

func TestApplyEnvOverrides_NodeMaxOldSpaceMB(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "unset keeps the YAML value", value: "", want: 7},
		{name: "set", value: "512", want: 512},
		{name: "zero disables the cap", value: "0", want: 0},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not a number", value: "512Mi", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Runner.NodeMaxOldSpaceMB = 7
			env := map[string]string{"KORA_NODE_MAX_OLD_SPACE_MB": tt.value}
			err := applyEnvOverrides(cfg, func(k string) string { return env[k] })
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Runner.NodeMaxOldSpaceMB != tt.want {
				t.Errorf("NodeMaxOldSpaceMB = %d, want %d", cfg.Runner.NodeMaxOldSpaceMB, tt.want)
			}
		})
	}
}

func TestApplyEnvOverrides_GatewayURLs(t *testing.T) {
	cfg := &Config{}
	cfg.InferenceGateway.URL = "http://yaml-inference"
	cfg.ToolGateway.URL = "http://yaml-tool"
	env := map[string]string{
		"KORA_INFERENCE_GATEWAY_URL": "http://orbit-inference-gateway.orbit-prod.svc.cluster.local",
		"KORA_TOOL_GATEWAY_URL":      "http://orbit-tool-gateway.orbit-prod.svc.cluster.local",
	}
	if err := applyEnvOverrides(cfg, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.InferenceGateway.URL, env["KORA_INFERENCE_GATEWAY_URL"]; got != want {
		t.Errorf("InferenceGateway.URL = %q, want %q", got, want)
	}
	if got, want := cfg.ToolGateway.URL, env["KORA_TOOL_GATEWAY_URL"]; got != want {
		t.Errorf("ToolGateway.URL = %q, want %q", got, want)
	}
}

func TestDrainTimeout(t *testing.T) {
	for _, tc := range []struct {
		grace   string
		want    time.Duration
		limited bool
	}{{"", 0, false}, {"3600", 3540 * time.Second, true}, {"30", 0, true}} {
		cfg := &Config{}
		cfg.Server.ShutdownTimeoutSecs = 30
		if err := applyEnvOverrides(cfg, func(k string) string {
			if k == "KORA_TERMINATION_GRACE_PERIOD_SECS" {
				return tc.grace
			}
			return ""
		}); err != nil {
			t.Fatal(err)
		}
		if got, limited := cfg.DrainTimeout(); got != tc.want || limited != tc.limited {
			t.Errorf("grace %q: drain timeout = %s limited %v, want %s %v", tc.grace, got, limited, tc.want, tc.limited)
		}
	}
}
