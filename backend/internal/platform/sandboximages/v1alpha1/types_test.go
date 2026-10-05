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
		if got := pin(tc.ref, d); got != tc.want {
			t.Errorf("pin(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestValid(t *testing.T) {
	const ref = "ghcr.io/acme/go:1"
	valid := metav1.Condition{Type: ConditionValid, Status: metav1.ConditionTrue}
	rejected := metav1.Condition{Type: ConditionValid, Status: metav1.ConditionFalse, Reason: ReasonWrongUser}
	unread := metav1.Condition{Type: ConditionValid, Status: metav1.ConditionFalse, Reason: "RegistryError"}
	for _, tc := range []struct {
		name         string
		spec         string
		status       Status
		wantValid    bool
		wantRejected bool
	}{
		{"accepted", ref, Status{Image: ref, Digest: "sha256:a", Conditions: []metav1.Condition{valid}}, true, false},
		{"accepted at an older generation", ref, Status{Image: ref, Digest: "sha256:a", ObservedGeneration: 1, Conditions: []metav1.Condition{valid}}, true, false},
		{"spec names another image", "ghcr.io/acme/go:2", Status{Image: ref, Digest: "sha256:a", Conditions: []metav1.Condition{valid}}, false, false},
		{"rejected", ref, Status{Image: ref, Conditions: []metav1.Condition{rejected}}, false, true},
		{"rejection of an older image", "ghcr.io/acme/go:2", Status{Image: ref, Conditions: []metav1.Condition{rejected}}, false, false},
		{"not read yet", ref, Status{Conditions: []metav1.Condition{unread}}, false, false},
		{"no status", ref, Status{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := SandboxImage{ObjectMeta: metav1.ObjectMeta{Generation: 2}, Spec: Spec{Image: tc.spec}, Status: tc.status}
			if got := s.Valid(); got != tc.wantValid {
				t.Errorf("Valid() = %v, want %v", got, tc.wantValid)
			}
			if got := s.Rejected(); got != tc.wantRejected {
				t.Errorf("Rejected() = %v, want %v", got, tc.wantRejected)
			}
		})
	}
}

func TestPinnedRefUsesTheValidatedImage(t *testing.T) {
	s := SandboxImage{Spec: Spec{Image: "ghcr.io/acme/go:2"}, Status: Status{Image: "ghcr.io/acme/go:1", Digest: "sha256:a"}}
	if got := s.PinnedRef(); got != "ghcr.io/acme/go@sha256:a" {
		t.Errorf("PinnedRef() = %q", got)
	}
}
