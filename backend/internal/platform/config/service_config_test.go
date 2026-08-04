package config

import (
	"testing"
	"time"
)

// policyBlockConfig mirrors how a service embeds PolicyRegistrationConfig: a
// policy block with its own keys plus the squashed shared cadence.
type policyBlockConfig struct {
	Policy struct {
		BundleEndpoint           string `yaml:"bundleEndpoint"`
		PolicyRegistrationConfig `yaml:",squash"`
	} `yaml:"policy"`
}

// TestReregistrationIntervalDefaultsWhenUnset covers the case every service is
// in: the key is absent from base.yaml, so the shared default has to supply the
// cadence. A zero here would panic time.NewTicker inside KeepRegistered.
func TestReregistrationIntervalDefaultsWhenUnset(t *testing.T) {
	for _, secs := range []int{0, -1} {
		c := PolicyRegistrationConfig{ReregistrationIntervalSecs: secs}
		if got := c.ReregistrationInterval(); got != defaultReregistrationInterval {
			t.Errorf("secs=%d: interval %s, want the default %s", secs, got, defaultReregistrationInterval)
		}
	}

	c := PolicyRegistrationConfig{ReregistrationIntervalSecs: 90}
	if got := c.ReregistrationInterval(); got != 90*time.Second {
		t.Errorf("explicit override: interval %s, want 90s", got)
	}
}

// TestPolicyRegistrationConfigSquashesIntoServiceBlock pins the embedding the
// five service configs rely on: an override written at
// policy.reregistrationIntervalSecs must reach the shared struct, and the
// service's own policy keys must keep decoding alongside it.
func TestPolicyRegistrationConfigSquashesIntoServiceBlock(t *testing.T) {
	const module = "cmd/example-service"
	root := makeTempProject(t, module, "base", `
policy:
  bundleEndpoint: "http://policy.invalid/bundles"
  reregistrationIntervalSecs: 120
`)
	chdirTo(t, root)

	cfg, err := LoadService[policyBlockConfig](module, "production")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Policy.BundleEndpoint != "http://policy.invalid/bundles" {
		t.Errorf("bundleEndpoint = %q", cfg.Policy.BundleEndpoint)
	}
	if got := cfg.Policy.ReregistrationInterval(); got != 2*time.Minute {
		t.Errorf("interval = %s, want 2m: the squashed key did not decode", got)
	}
}

// TestPolicyRegistrationConfigAbsentKeyLoads is the direct check on removing
// reregistrationIntervalSecs from every base.yaml: LoadService decodes with
// UnmarshalExact, which rejects unknown keys but must tolerate absent ones.
func TestPolicyRegistrationConfigAbsentKeyLoads(t *testing.T) {
	const module = "cmd/example-service"
	root := makeTempProject(t, module, "base", `
policy:
  bundleEndpoint: "http://policy.invalid/bundles"
`)
	chdirTo(t, root)

	cfg, err := LoadService[policyBlockConfig](module, "production")
	if err != nil {
		t.Fatalf("load with the key absent: %v", err)
	}
	if got := cfg.Policy.ReregistrationInterval(); got != defaultReregistrationInterval {
		t.Errorf("interval = %s, want the default %s", got, defaultReregistrationInterval)
	}
}
