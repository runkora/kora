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

func TestValidate_ProtectedSandboxIdentityLabelSMPRejected(t *testing.T) {
	// buildSandboxPod (k8s.go) sets orbit.io/role, orbit.io/run-id, and
	// orbit.io/agent-slug for pod-webhook selection and SPIFFE attestation.
	// A patch removing or changing any of them silently disables sandbox
	// injection or misattests the sandbox's identity.
	cases := []struct {
		name string
		body string
	}{
		{"orbit.io/role changed", `{"metadata":{"labels":{"orbit.io/role":"not-sandbox"}}}`},
		{"orbit.io/run-id changed", `{"metadata":{"labels":{"orbit.io/run-id":"forged-run"}}}`},
		{"orbit.io/agent-slug changed", `{"metadata":{"labels":{"orbit.io/agent-slug":"forged-slug"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patches := []Patch{{File: "bad.yaml", Flavor: StrategicMerge, Kind: "Pod", Body: []byte(tc.body)}}
			err := Validate(patches)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "protected") {
				t.Errorf("expected protected-path error, got %v", err)
			}
		})
	}
}

func TestValidate_ProtectedSandboxIdentityLabelJSON6902Rejected(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"remove orbit.io/role", `[{"op":"remove","path":"/metadata/labels/orbit.io~1role"}]`},
		{"remove orbit.io/run-id", `[{"op":"remove","path":"/metadata/labels/orbit.io~1run-id"}]`},
		{"remove orbit.io/agent-slug", `[{"op":"remove","path":"/metadata/labels/orbit.io~1agent-slug"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patches := []Patch{{File: "bad.yaml", Flavor: JSONPatch, Kind: "Pod", Body: []byte(tc.body)}}
			err := Validate(patches)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "protected") {
				t.Errorf("expected protected-path error, got %v", err)
			}
		})
	}
}

func TestValidate_NonProtectedPathAllowed(t *testing.T) {
	// Storage class, resources, node selectors, image pull secrets, and
	// service account annotations must NOT trip protected-path checks.
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
