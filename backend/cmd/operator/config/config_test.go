package operatorConfig

import (
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

func TestBaseDefaults(t *testing.T) {
	cfg, err := config.LoadService[Config]("cmd/operator", "production")
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	in := cfg.Install
	if in.SystemNamespace != "orbit-system" || in.PodSecurity.Enforce != "restricted" || in.Injection.BundlePollingMaxDelay != 60 {
		t.Errorf("install defaults = %+v", in)
	}
	if in.TrustDomain != "" {
		t.Errorf("trustDomain = %q, want empty so the chart must render it", in.TrustDomain)
	}
}

func TestPreflightInterval_HasAFloor(t *testing.T) {
	for secs, want := range map[int]time.Duration{0: 10 * time.Second, 5: 10 * time.Second, 300: 5 * time.Minute} {
		if got := (Preflight{IntervalSeconds: secs}).Interval(); got != want {
			t.Errorf("Interval(%d) = %s, want %s", secs, got, want)
		}
	}
}
