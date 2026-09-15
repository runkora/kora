package manifestpatch

import (
	"encoding/json"
	"fmt"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"
)

// Apply runs every patch targeting kind over obj, one at a time in file order,
// so a later patch sees what an earlier one changed. Each result must keep
// the protected fields as built and decode strictly onto T, which rejects
// field typos kustomize would otherwise pass through.
func Apply[T any](obj T, kind string, patches []Patch) (T, error) {
	var built, current []byte
	result := obj
	for _, p := range patches {
		if p.Entry.Target.Kind != kind {
			continue
		}
		if built == nil {
			var err error
			if built, err = toResource(obj, kind); err != nil {
				return obj, err
			}
			current = built
		}
		out, err := kustomize(current, p.Entry)
		if err != nil {
			return obj, fmt.Errorf("patch %s: %w", p.File, err)
		}
		if err := checkProtected(built, out); err != nil {
			return obj, fmt.Errorf("patch %s: %w", p.File, err)
		}
		var next T
		if err := yaml.UnmarshalStrict(out, &next); err != nil {
			return obj, fmt.Errorf("patch %s: %w", p.File, err)
		}
		current, result = out, next
	}
	return result, nil
}

// toResource renders obj as YAML with apiVersion and kind set, which kustomize
// needs to identify the resource. The typed builders leave TypeMeta empty.
func toResource(obj any, kind string) ([]byte, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", kind, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", kind, err)
	}
	m["apiVersion"] = apiVersions[kind]
	m["kind"] = kind
	return yaml.Marshal(m)
}

// kustomize builds a one-resource kustomization with a single patch in an
// in-memory filesystem and returns the patched resource.
func kustomize(resource []byte, entry types.Patch) ([]byte, error) {
	k, err := yaml.Marshal(types.Kustomization{
		Resources: []string{"resource.yaml"},
		Patches:   []types.Patch{entry},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal kustomization: %w", err)
	}
	fs := filesys.MakeFsInMemory()
	if err = fs.WriteFile("/resource.yaml", resource); err != nil {
		return nil, fmt.Errorf("write resource: %w", err)
	}
	if err = fs.WriteFile("/kustomization.yaml", k); err != nil {
		return nil, fmt.Errorf("write kustomization: %w", err)
	}
	resMap, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(fs, "/")
	if err != nil {
		return nil, fmt.Errorf("kustomize: %w", err)
	}
	if resMap.Size() != 1 {
		return nil, fmt.Errorf("patch must leave exactly one resource, got %d", resMap.Size())
	}
	return resMap.Resources()[0].AsYAML()
}
