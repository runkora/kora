package manifestpatch

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/yaml"
)

// Load reads every *.yaml/*.yml file in dir, in filename order. Each file is
// one kustomize `patches:` entry. A missing directory means no patches.
func Load(dir string) ([]Patch, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read patch dir %s: %w", dir, err)
	}

	var patches []Patch
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read patch %s: %w", e.Name(), err)
		}
		var entry types.Patch
		if err := yaml.UnmarshalStrict(body, &entry); err != nil {
			return nil, fmt.Errorf("patch %s: %w", e.Name(), err)
		}
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("patch %s: %w", e.Name(), err)
		}
		patches = append(patches, Patch{File: e.Name(), Entry: entry})
	}
	return patches, nil
}

// validateEntry rejects entries the apply path cannot handle. Patches run
// against one resource at a time, so every entry needs a target Kind.
func validateEntry(e types.Patch) error {
	if e.Path != "" {
		return fmt.Errorf("path is not supported, inline the patch under patch")
	}
	if strings.TrimSpace(e.Patch) == "" {
		return fmt.Errorf("patch is empty")
	}
	if e.Target == nil || e.Target.Kind == "" {
		return fmt.Errorf("target.kind is required")
	}
	if _, ok := apiVersions[e.Target.Kind]; !ok {
		return fmt.Errorf("unknown target kind %q, must be one of %v", e.Target.Kind, slices.Sorted(maps.Keys(apiVersions)))
	}
	if _, err := labels.Parse(e.Target.LabelSelector); err != nil {
		return fmt.Errorf("target.labelSelector: %w", err)
	}
	if _, err := labels.Parse(e.Target.AnnotationSelector); err != nil {
		return fmt.Errorf("target.annotationSelector: %w", err)
	}
	return nil
}
