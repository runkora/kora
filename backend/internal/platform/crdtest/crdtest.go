// Package crdtest loads the CRDs the kora-crds chart ships and validates
// objects against their schemas, for tests.
//
// The schema is walked as plain data. Running the API server's validators and
// CEL would pull k8s.io/apiserver and cel-go into this module, so CEL rules are
// not executed.
package crdtest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// Node is a node of a CRD, as decoded from YAML.
type Node = map[string]any

// CRD loads charts/kora-crds/templates/<file>. Includes of the chart's helper
// templates are expanded. Other template lines carry only labels and are
// dropped.
func CRD(t *testing.T, file string) Node {
	t.Helper()
	dir := templatesDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	helpers, err := os.ReadFile(filepath.Join(dir, "_helpers.tpl"))
	if err != nil {
		t.Fatal(err)
	}
	defines := parseDefines(string(helpers))
	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if m := includeRe.FindStringSubmatch(line); m != nil {
			if body, ok := defines[m[1]]; ok {
				indent, _ := strconv.Atoi(m[2])
				for l := range strings.SplitSeq(body, "\n") {
					kept = append(kept, strings.Repeat(" ", indent)+l)
				}
				continue
			}
		}
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

var (
	includeRe = regexp.MustCompile(`\{\{-? include "([^"]+)" \. \| nindent (\d+) \}\}`)
	defineRe  = regexp.MustCompile(`(?s)\{\{- define "([^"]+)" -\}\}\n(.*?)\n\{\{- end \}\}`)
)

func parseDefines(helpers string) map[string]string {
	out := map[string]string{}
	for _, m := range defineRe.FindAllStringSubmatch(helpers, -1) {
		// A define with template actions, such as the labels, carries no schema.
		if !strings.Contains(m[2], "{{") {
			out[m[1]] = m[2]
		}
	}
	return out
}

// templatesDir finds the chart's templates directory above the working directory.
func templatesDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, "charts", "kora-crds", "templates")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("charts/kora-crds/templates not found above the working directory")
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

// Validate checks value against the OpenAPI keywords the kora CRDs use. It
// returns one message per violation. Unknown fields are reported except under
// metadata and x-kubernetes-preserve-unknown-fields.
func Validate(schema Node, value any, path string) []string {
	want := map[string]string{"object": "an object", "array": "an array", "string": "a string", "boolean": "a boolean"}
	var got string
	var errs []string
	switch v := value.(type) {
	case map[string]any:
		got, errs = "object", validateObject(schema, v, path)
	case []any:
		got, errs = "array", validateArray(schema, v, path)
	case string:
		got, errs = "string", validateString(schema, v, path)
	case float64:
		if schema["type"] != "integer" && schema["type"] != "number" {
			return []string{fmt.Sprintf("%s: got a number, want %v", path, schema["type"])}
		}
		return validateNumber(schema, v, path)
	case bool:
		got = "boolean"
	default:
		return []string{fmt.Sprintf("%s: unexpected %T, want %v", path, value, schema["type"])}
	}
	if schema["type"] != got {
		errs = append([]string{fmt.Sprintf("%s: got %s, want %v", path, want[got], schema["type"])}, errs...)
	}
	return errs
}

func validateObject(schema Node, v map[string]any, path string) []string {
	var errs []string
	required, _ := schema["required"].([]any)
	for _, r := range required {
		if _, ok := v[r.(string)]; !ok {
			errs = append(errs, fmt.Sprintf("%s: %s is required", path, r))
		}
	}
	for k, item := range v {
		switch {
		case Prop(schema, k) != nil:
			errs = append(errs, Validate(Prop(schema, k), item, path+"."+k)...)
		case Child(schema, "additionalProperties") != nil:
			errs = append(errs, Validate(Child(schema, "additionalProperties"), item, path+"."+k)...)
		case schema["x-kubernetes-preserve-unknown-fields"] == true || path == ".metadata":
		default:
			errs = append(errs, fmt.Sprintf("%s: unknown field %s", path, k))
		}
	}
	return errs
}

func validateArray(schema Node, v []any, path string) []string {
	var errs []string
	if n, ok := schema["minItems"].(float64); ok && float64(len(v)) < n {
		errs = append(errs, fmt.Sprintf("%s: needs at least %v items", path, n))
	}
	if n, ok := schema["maxItems"].(float64); ok && float64(len(v)) > n {
		errs = append(errs, fmt.Sprintf("%s: allows at most %v items", path, n))
	}
	for i, item := range v {
		errs = append(errs, Validate(Child(schema, "items"), item, fmt.Sprintf("%s[%d]", path, i))...)
	}
	return errs
}

func validateString(schema Node, v, path string) []string {
	var errs []string
	if enum, ok := schema["enum"].([]any); ok && !slices.Contains(enum, any(v)) {
		errs = append(errs, fmt.Sprintf("%s: %q is not one of %v", path, v, enum))
	}
	if n, ok := schema["minLength"].(float64); ok && float64(len(v)) < n {
		errs = append(errs, fmt.Sprintf("%s: shorter than %v", path, n))
	}
	if n, ok := schema["maxLength"].(float64); ok && float64(len(v)) > n {
		errs = append(errs, fmt.Sprintf("%s: longer than %v", path, n))
	}
	if pat, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pat).MatchString(v) {
		errs = append(errs, fmt.Sprintf("%s: %q does not match %s", path, v, pat))
	}
	return errs
}

func validateNumber(schema Node, v float64, path string) []string {
	var errs []string
	if n, ok := schema["minimum"].(float64); ok && (v < n || (v == n && schema["exclusiveMinimum"] == true)) {
		errs = append(errs, fmt.Sprintf("%s: %v is below the minimum %v", path, v, n))
	}
	if n, ok := schema["maximum"].(float64); ok && v > n {
		errs = append(errs, fmt.Sprintf("%s: %v is above the maximum %v", path, v, n))
	}
	return errs
}
