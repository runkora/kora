package smConfig

import (
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
)

func TestApplyEnvOverrides_Landlock(t *testing.T) {
	for _, tc := range []struct {
		yaml, env, want string
		wantErr         bool
	}{
		{yaml: "enforce", env: "", want: "enforce"},
		{yaml: "enforce", env: "bestEffort", want: "bestEffort"},
		{yaml: "", env: "", want: ""},
		{yaml: "enforce", env: "off", wantErr: true},
		{yaml: "loose", env: "", wantErr: true},
	} {
		cfg := &Config{}
		cfg.Runner.Landlock = tc.yaml
		err := applyEnvOverrides(cfg, func(k string) string {
			if k == "KORA_LANDLOCK" {
				return tc.env
			}
			return ""
		})
		if (err != nil) != tc.wantErr || (err == nil && cfg.Runner.Landlock != tc.want) {
			t.Errorf("yaml %q env %q: Landlock = %q, err = %v; want %q, error %v", tc.yaml, tc.env, cfg.Runner.Landlock, err, tc.want, tc.wantErr)
		}
	}
}

func TestApplyEnvOverrides_GatewayURLs(t *testing.T) {
	cfg := &Config{}
	cfg.InferenceGateway.URL = "http://yaml-inference"
	cfg.ToolGateway.URL = "http://yaml-tool"
	env := map[string]string{
		"KORA_INFERENCE_GATEWAY_URL": "http://kora-inference-gateway.kora-prod.svc.cluster.local",
		"KORA_TOOL_GATEWAY_URL":      "http://kora-tool-gateway.kora-prod.svc.cluster.local",
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

// The pressure monitor checks every 250 ms at 0.90 of memory.max unless configured otherwise.
func TestBaseDefaults_MemoryPressure(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/session-manager", "production")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CheckInterval(); got != 250*time.Millisecond {
		t.Errorf("CheckInterval = %s, want 250ms", got)
	}
	if cfg.Memory.PressureLimit != 0.90 {
		t.Errorf("pressureLimit = %v, want 0.90", cfg.Memory.PressureLimit)
	}
}

func TestApplyEnvOverrides_PressureLimit(t *testing.T) {
	for env, want := range map[string]float64{"": 0.9, "0.85": 0.85, "0.99": 0.99} {
		cfg := &Config{}
		cfg.Memory.PressureLimit = 0.9
		if err := applyEnvOverrides(cfg, func(k string) string {
			if k == "KORA_PRESSURE_LIMIT" {
				return env
			}
			return ""
		}); err != nil || cfg.Memory.PressureLimit != want {
			t.Errorf("env %q: pressureLimit = %v, err = %v; want %v", env, cfg.Memory.PressureLimit, err, want)
		}
	}
	for _, env := range []string{"0", "1", "1.5", "-0.2", "NaN", "high"} {
		cfg := &Config{}
		if err := applyEnvOverrides(cfg, func(k string) string {
			if k == "KORA_PRESSURE_LIMIT" {
				return env
			}
			return ""
		}); err == nil {
			t.Errorf("env %q: want an error", env)
		}
	}
}

func TestValidate_CheckInterval(t *testing.T) {
	for ms, valid := range map[int]bool{1: true, 250: true, 0: false, -1: false} {
		cfg := &Config{}
		cfg.Memory.CheckIntervalMs = ms
		if err := validate(cfg); (err == nil) != valid {
			t.Errorf("checkIntervalMs %d: err = %v, want valid %v", ms, err, valid)
		}
	}
}
