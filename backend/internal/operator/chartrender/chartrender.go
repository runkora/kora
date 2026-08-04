// Package chartrender renders a chart's templates via `helm template` against
// fixed values and returns the decoded manifests, for asserting on rendered
// YAML in tests without a live Kubernetes/Helm backend. Devenv-mode-specific
// use only — not a general chart-testing framework (see
// docs/adr/2026-08-04-devenv-mode-platform-support.md, Decision 3).
package chartrender

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// releaseName is fixed across every render — no test asserts on it, and a
// stable name keeps rendered output (and any error messages) reproducible.
const releaseName = "devenv-test"

// Render runs `helm template` against chartPath with the given values
// (marshaled to a temp values.yaml) and returns one decoded object per
// "---" document, keyed by "<kind>/<name>" for lookup in assertions.
//
// Requires helm on PATH (already a make setup dependency); fails the test
// immediately, with the reason, if it is absent rather than skipping —
// devenv-mode chart regressions must fail make check, not go quietly
// unverified.
func Render(t *testing.T, chartPath string, values map[string]any) map[string]*unstructured.Unstructured {
	t.Helper()

	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm not found on PATH — required to render chart templates in tests (see make setup): %v", err)
	}

	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		t.Fatalf("marshal values to YAML: %v", err)
	}

	valuesPath := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(valuesPath, valuesYAML, 0o600); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}

	// #nosec G204 -- chartPath/valuesPath are test-authored constants, not
	// external input.
	cmd := exec.CommandContext(context.Background(), "helm", "template", releaseName, chartPath, "-f", valuesPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template %s: %v\n%s", chartPath, err, stderr.String())
	}

	return parseManifests(t, stdout.String())
}

// parseManifests splits a multi-document YAML stream on "---" and decodes
// each non-empty document, keyed by "<kind>/<name>".
func parseManifests(t *testing.T, rendered string) map[string]*unstructured.Unstructured {
	t.Helper()

	objects := make(map[string]*unstructured.Unstructured)
	for doc := range strings.SplitSeq(rendered, "\n---\n") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), &obj.Object); err != nil {
			t.Fatalf("decode rendered manifest: %v\n%s", err, doc)
		}
		if len(obj.Object) == 0 {
			continue
		}

		key := obj.GetKind() + "/" + obj.GetName()
		objects[key] = obj
	}
	return objects
}
