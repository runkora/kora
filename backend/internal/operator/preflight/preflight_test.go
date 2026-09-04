package preflight

import "testing"

func TestVersionSupported(t *testing.T) {
	r := &Runner{Config: Config{SupportedIstioMinors: []string{"1.29", "1.28"}}}
	for version, want := range map[string]bool{"1.29.1": true, "1.28.4": true, "1.30.0": false, "garbage": false} {
		if ok, _ := r.versionSupported(version); ok != want {
			t.Errorf("%s: supported=%v want %v", version, ok, want)
		}
	}
}

func TestClaimsAllManagedPods(t *testing.T) {
	selectsAll := map[string]any{"podSelector": map[string]any{"matchLabels": map[string]any{"spiffe.io/spire-managed-identity": "true"}}}
	if !claimsAllManagedPods(selectsAll) {
		t.Error("default registration should be flagged")
	}
	withFallback := map[string]any{"fallback": true, "podSelector": selectsAll["podSelector"]}
	if claimsAllManagedPods(withFallback) {
		t.Error("fallback registration is fine")
	}
	scoped := map[string]any{"namespaceSelector": map[string]any{}, "podSelector": selectsAll["podSelector"]}
	if claimsAllManagedPods(scoped) {
		t.Error("namespace-scoped registration is fine")
	}
}

func TestResult_OKAndFailures(t *testing.T) {
	r := Result{Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: false, Message: "nope"}}}
	if r.OK() {
		t.Error("should fail")
	}
	if r.Failures() != "b: nope" {
		t.Errorf("failures = %q", r.Failures())
	}
	if (Result{}).OK() {
		t.Error("empty result must not pass")
	}
}
