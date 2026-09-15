package manifestpatch

import (
	"strings"
	"testing"
)

func TestValidate_ProtectedPathSMPRejected(t *testing.T) {
	// Touching metadata.namespace via SMP orphans the resource from its
	// StatefulSet/Service/SA/HPA/PDB peers named after the same slug.
	patches := []Patch{{
		File:   "010-bad.yaml",
		Flavor: StrategicMerge,
		Kind:   "StatefulSet",
		Body:   []byte(`{"metadata":{"namespace":"other-ns"}}`),
	}}
	err := Validate(patches)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("expected protected-path error, got %v", err)
	}
}

func TestValidate_ProtectedPathJSON6902Rejected(t *testing.T) {
	// Overwriting serviceAccountName silently drops the pod off its
	// attested SPIFFE identity.
	patches := []Patch{{
		File:   "020-bad.yaml",
		Flavor: JSONPatch,
		Kind:   "Pod",
		Body:   []byte(`[{"op":"replace","path":"/spec/serviceAccountName","value":"attacker"}]`),
	}}
	err := Validate(patches)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("expected protected-path error, got %v", err)
	}
}

func TestValidate_ProtectedPathSecurityContextRejected(t *testing.T) {
	patches := []Patch{{
		File:   "030-weak-security.yaml",
		Flavor: StrategicMerge,
		Kind:   "Pod",
		Body:   []byte(`{"spec":{"securityContext":{"runAsNonRoot":false}}}`),
	}}
	err := Validate(patches)
	if err == nil {
		t.Fatal("expected error rejecting runAsNonRoot=false patch, got nil")
	}
}

func TestValidate_NonProtectedPathAllowed(t *testing.T) {
	// Storage class, resources, node selectors, image pull secrets — the
	// operator-facing knobs the chart translates today — must NOT trip
	// protected-path checks.
	patches := []Patch{
		{File: "a.yaml", Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Name: "workspaces", Body: []byte(`{"spec":{"storageClassName":"gp3"}}`)},
		{File: "b.yaml", Flavor: StrategicMerge, Kind: "Pod", Body: []byte(`{"spec":{"nodeSelector":{"disk":"ssd"}}}`)},
		{File: "c.yaml", Flavor: StrategicMerge, Kind: "ServiceAccount", Body: []byte(`{"metadata":{"annotations":{"eks.amazonaws.com/role-arn":"arn:aws:iam::123:role/foo"}}}`)},
		{File: "d.yaml", Flavor: JSONPatch, Kind: "Pod", Body: []byte(`[{"op":"replace","path":"/spec/containers/0/resources","value":{"limits":{"cpu":"4"}}}]`)},
	}
	if err := Validate(patches); err != nil {
		t.Fatalf("expected chart-typical patches to pass, got %v", err)
	}
}
