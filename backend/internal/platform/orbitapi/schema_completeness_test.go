package orbitapi

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestStatusSchemaCompleteness guards pinned decision 21: a structural CRD
// schema PRUNES any field not declared in it, so a status field the Go type
// writes but the chart schema omits is silently dropped by the API server —
// the operator can set it every reconcile and it will never persist. This
// reflects over OrbitDevEnvironmentStatus's JSON tags and asserts each one
// is declared under status.properties in the chart CRD.
func TestStatusSchemaCompleteness(t *testing.T) {
	schema := loadStatusSchemaProperties(t)

	statusType := reflect.TypeFor[OrbitDevEnvironmentStatus]()
	for field := range statusType.Fields() {
		field := field
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("field %s has no usable json tag", field.Name)
		}
		name := strings.Split(tag, ",")[0]

		if _, ok := schema[name]; !ok {
			t.Errorf("status field %q (Go field %s) is missing from status.properties in "+
				"charts/orbit-operator/templates/crd-orbitdevenvironment.yaml — it will be "+
				"silently pruned by the API server", name, field.Name)
		}
	}
}

// crdDocument mirrors just enough of the CRD chart's static structure
// (everything from "spec:" onward — the only part of the file that is not
// itself Helm template syntax) to reach status.properties.
type crdDocument struct {
	Spec struct {
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties struct {
						Status struct {
							Properties map[string]any `json:"properties"`
						} `json:"status"`
					} `json:"properties"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

func loadStatusSchemaProperties(t *testing.T) map[string]any {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path via runtime.Caller")
	}
	// thisFile: backend/internal/platform/orbitapi/schema_completeness_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	crdPath := filepath.Join(repoRoot, "charts", "orbit-operator", "templates", "crd-orbitdevenvironment.yaml")

	raw, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read %s: %v", crdPath, err)
	}

	// The file's metadata.labels block uses Helm template syntax
	// ({{- include ... }}) that is not valid standalone YAML. Everything
	// from the top-level "spec:" key onward is static, template-free YAML,
	// so parse from there.
	const marker = "\nspec:\n"
	idx := strings.Index(string(raw), marker)
	if idx == -1 {
		t.Fatalf("could not find top-level %q in %s", "spec:", crdPath)
	}
	doc := raw[idx+1:]

	var parsed crdDocument
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("parse %s: %v", crdPath, err)
	}
	if len(parsed.Spec.Versions) == 0 {
		t.Fatalf("%s: spec.versions is empty", crdPath)
	}

	props := parsed.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Status.Properties
	if len(props) == 0 {
		t.Fatalf("%s: status.properties is empty", crdPath)
	}
	return props
}
