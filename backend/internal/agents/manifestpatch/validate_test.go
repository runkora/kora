package manifestpatch

import (
	"strings"
	"testing"
)

func TestValidate_EmptyIsOK(t *testing.T) {
	if err := Validate(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidate_UnknownKindRejected(t *testing.T) {
	patches := []Patch{{
		File:   "010-typo.yaml",
		Flavor: StrategicMerge,
		Kind:   "Deployement", // typo
		Body:   []byte(`{"spec":{"replicas":3}}`),
	}}
	err := Validate(patches)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("expected unknown kind error, got %v", err)
	}
	if !strings.Contains(err.Error(), "010-typo.yaml") {
		t.Errorf("expected filename in error, got %v", err)
	}
}

func TestValidate_BadJSON6902OpRejected(t *testing.T) {
	patches := []Patch{{
		File:   "020-bad-op.yaml",
		Flavor: JSONPatch,
		Kind:   "StatefulSet",
		Body:   []byte(`[{"op":"nonsense","path":"/spec/replicas","value":3}]`),
	}}
	if err := Validate(patches); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestValidate_ValidPatchesPass(t *testing.T) {
	patches := []Patch{
		{File: "a.yaml", Flavor: StrategicMerge, Kind: "StatefulSet", Body: []byte(`{"spec":{"replicas":3}}`)},
		{File: "b.yaml", Flavor: StrategicMerge, Kind: "PersistentVolumeClaim", Name: "workspaces", Body: []byte(`{"spec":{"storageClassName":"gp3"}}`)},
		{File: "c.yaml", Flavor: JSONPatch, Kind: "Pod", LabelSelector: "orbit.io/role=sandbox", Body: []byte(`[{"op":"replace","path":"/spec/containers/0/resources","value":{"limits":{"cpu":"4"}}}]`)},
	}
	if err := Validate(patches); err != nil {
		t.Fatalf("expected valid patches to pass, got %v", err)
	}
}

func TestValidate_BadLabelSelectorRejected(t *testing.T) {
	patches := []Patch{{
		File:          "030-bad-selector.yaml",
		Flavor:        StrategicMerge,
		Kind:          "Pod",
		LabelSelector: "===bad===",
		Body:          []byte(`{"spec":{"nodeName":"node-1"}}`),
	}}
	if err := Validate(patches); err == nil {
		t.Fatal("expected error, got nil")
	}
}
