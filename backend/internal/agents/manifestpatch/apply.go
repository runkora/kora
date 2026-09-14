package manifestpatch

import (
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// Apply applies each of the given patches to obj in order, returning the
// resulting object. Patches are dispatched by flavor: StrategicMerge uses
// k8s strategic merge (schema from obj's struct tags); JSONPatch uses RFC
// 6902.
//
// A zero-length patches slice returns obj unchanged.
func Apply[T any](obj T, patches []Patch) (T, error) {
	if len(patches) == 0 {
		return obj, nil
	}
	origJSON, err := json.Marshal(obj)
	if err != nil {
		return obj, fmt.Errorf("marshal object: %w", err)
	}
	for _, p := range patches {
		switch p.Flavor {
		case StrategicMerge:
			// StrategicMergePatch needs a value of the target type so it can
			// read the patchMergeKey/patchStrategy struct tags. obj itself
			// works here — the tags are on the type.
			merged, err := strategicpatch.StrategicMergePatch(origJSON, p.Body, obj)
			if err != nil {
				return obj, fmt.Errorf("apply strategic merge patch %s: %w", p.File, err)
			}
			origJSON = merged
		case JSONPatch:
			decoded, err := jsonpatch.DecodePatch(p.Body)
			if err != nil {
				return obj, fmt.Errorf("decode json6902 patch %s: %w", p.File, err)
			}
			opts := jsonpatch.NewApplyOptions()
			opts.AllowMissingPathOnRemove = true
			opts.EnsurePathExistsOnAdd = true
			applied, err := decoded.ApplyWithOptions(origJSON, opts)
			if err != nil {
				return obj, fmt.Errorf("apply json6902 patch %s: %w", p.File, err)
			}
			origJSON = applied
		default:
			return obj, fmt.Errorf("apply patch %s: unknown flavor %v", p.File, p.Flavor)
		}
	}
	var out T
	if err := json.Unmarshal(origJSON, &out); err != nil {
		return obj, fmt.Errorf("unmarshal patched object: %w", err)
	}
	return out, nil
}

// ApplyToResource is a convenience that filters patches by (kind, name,
// labels, annotations) then applies. It is the shape every provisioner call
// site wants.
func ApplyToResource[T any](obj T, patches []Patch, kind, name string, resourceLabels, resourceAnnotations map[string]string) (T, error) {
	filtered, err := FilterForResource(patches, kind, name, resourceLabels, resourceAnnotations)
	if err != nil {
		return obj, err
	}
	return Apply(obj, filtered)
}
