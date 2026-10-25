// Package crdtest loads the CRDs the kora-crds chart ships and validates
// objects against their schemas, for tests.
//
// CEL rules are not executed. Running them would pull k8s.io/apiserver and
// cel-go into this module.
package crdtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
	"k8s.io/kube-openapi/pkg/validation/validate"
	"sigs.k8s.io/yaml"
)

// Node is a node of a CRD, as decoded from YAML.
type Node = map[string]any

// CRD loads charts/kora-crds/<file>: a controller-gen file under generated/ or
// a hand-written template under templates/. Template lines carry only labels
// and are dropped.
func CRD(t *testing.T, file string) Node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(chartDir(t), file))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.Contains(line, "{{") {
			kept = append(kept, line)
		}
	}
	var crd Node
	if err := yaml.Unmarshal([]byte(strings.Join(kept, "\n")), &crd); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return crd
}

// Schema returns the openAPIV3Schema of a CRD with exactly one version.
func Schema(t *testing.T, file string) Node {
	t.Helper()
	versions, _ := Child(CRD(t, file), "spec")["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("%s: want one version, got %d", file, len(versions))
	}
	schema := Child(Child(versions[0].(Node), "schema"), "openAPIV3Schema")
	if schema == nil {
		t.Fatalf("%s: no openAPIV3Schema", file)
	}
	return schema
}

// chartDir finds charts/kora-crds above the working directory.
func chartDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "charts", "kora-crds")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("charts/kora-crds not found above the working directory")
		}
		dir = parent
	}
}

// Child returns the map under key, or nil.
func Child(n Node, key string) Node {
	c, _ := n[key].(Node)
	return c
}

// Prop returns the schema of a property of an object schema.
func Prop(n Node, name string) Node { return Child(Child(n, "properties"), name) }

// Validate checks value against an openAPIV3Schema with the OpenAPI
// validator. Unknown fields are not reported.
func Validate(t *testing.T, schema Node, value any) error {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var s spec.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return validate.AgainstSchema(&s, value, strfmt.Default)
}
