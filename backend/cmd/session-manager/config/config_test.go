package smConfig

import "testing"

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
			env := map[string]string{"ORBIT_NODE_MAX_OLD_SPACE_MB": tt.value}
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
		"ORBIT_INFERENCE_GATEWAY_URL": "http://orbit-inference-gateway.orbit-prod.svc.cluster.local",
		"ORBIT_TOOL_GATEWAY_URL":      "http://orbit-tool-gateway.orbit-prod.svc.cluster.local",
	}
	if err := applyEnvOverrides(cfg, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.InferenceGateway.URL, env["ORBIT_INFERENCE_GATEWAY_URL"]; got != want {
		t.Errorf("InferenceGateway.URL = %q, want %q", got, want)
	}
	if got, want := cfg.ToolGateway.URL, env["ORBIT_TOOL_GATEWAY_URL"]; got != want {
		t.Errorf("ToolGateway.URL = %q, want %q", got, want)
	}
}
