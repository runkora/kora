package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPin(t *testing.T) {
	const d = "sha256:abc"
	for _, tc := range []struct{ ref, want string }{
		{"ghcr.io/acme/go:1.2", "ghcr.io/acme/go@" + d},
		{"ghcr.io/acme/go", "ghcr.io/acme/go@" + d},
		{"localhost:5001/kora-sandbox-base:latest", "localhost:5001/kora-sandbox-base@" + d},
		{"localhost:5001/kora-sandbox-base", "localhost:5001/kora-sandbox-base@" + d},
		{"ghcr.io/acme/go:1.2@sha256:old", "ghcr.io/acme/go@" + d},
		{"ghcr.io/acme/go@sha256:old", "ghcr.io/acme/go@" + d},
	} {
		if got := Pin(tc.ref, d); got != tc.want {
			t.Errorf("Pin(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestValid(t *testing.T) {
	valid := metav1.Condition{Type: ConditionValid, Status: metav1.ConditionTrue}
	invalid := metav1.Condition{Type: ConditionValid, Status: metav1.ConditionFalse}
	for _, tc := range []struct {
		name   string
		gen    int64
		status Status
		want   bool
	}{
		{"accepted at this generation", 2, Status{Digest: "sha256:a", ObservedGeneration: 2, Conditions: []metav1.Condition{valid}}, true},
		{"rejected", 2, Status{Digest: "sha256:a", ObservedGeneration: 2, Conditions: []metav1.Condition{invalid}}, false},
		{"stale generation", 3, Status{Digest: "sha256:a", ObservedGeneration: 2, Conditions: []metav1.Condition{valid}}, false},
		{"no digest", 2, Status{ObservedGeneration: 2, Conditions: []metav1.Condition{valid}}, false},
		{"no status", 1, Status{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := SandboxImage{ObjectMeta: metav1.ObjectMeta{Generation: tc.gen}, Status: tc.status}
			if got := s.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}
