package preflight

import (
	"testing"
	"time"
)

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

// TestLoop_RetriesSoonAfterFailure pins the startup behaviour: a failed run is
// retried after RetryInterval (never longer than Interval), a passing run waits
// the full Interval.
func TestLoop_RetriesSoonAfterFailure(t *testing.T) {
	l := &Loop{Interval: 5 * time.Minute}
	if got := l.nextDelay(false); got != defaultRetryInterval {
		t.Fatalf("failure delay = %v, want %v", got, defaultRetryInterval)
	}
	if got := l.nextDelay(true); got != 5*time.Minute {
		t.Fatalf("success delay = %v, want interval", got)
	}
	short := &Loop{Interval: 5 * time.Second, RetryInterval: time.Minute}
	if got := short.nextDelay(false); got != 5*time.Second {
		t.Fatalf("retry must be capped at the interval, got %v", got)
	}
}

func TestClaimsAllManagedPods_MatchExpressions(t *testing.T) {
	spec := map[string]any{
		"podSelector": map[string]any{
			"matchExpressions": []any{map[string]any{"key": "spiffe.io/spire-managed-identity", "operator": "Exists"}},
		},
	}
	if !claimsAllManagedPods(spec) {
		t.Fatal("a matchExpressions catch-all must be detected")
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
