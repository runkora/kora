// Package manifestpatch applies kustomize-style Strategic Merge and JSON6902
// patches to typed Kubernetes objects at provisioning time.
//
// See docs/adr/2026-09-15-manifest-patches-configmap.md.
package manifestpatch

// Flavor is the kustomize patch flavor of a Patch.
type Flavor int

const (
	// StrategicMerge is a Kubernetes Strategic Merge Patch. Body is a JSON
	// object shaped like the target type; list-merge semantics come from the
	// target type's struct tags.
	StrategicMerge Flavor = iota

	// JSONPatch is an RFC 6902 JSON Patch. Body is a JSON array of operations.
	JSONPatch
)

func (f Flavor) String() string {
	switch f {
	case StrategicMerge:
		return "strategic-merge"
	case JSONPatch:
		return "json6902"
	}
	return "unknown"
}

// Patch is a single kustomize-style patch with its target selector.
//
// For StrategicMerge, Kind/Name are inferred from the patch body's own
// apiVersion+kind+metadata.name; an empty Name matches any resource of that
// Kind. LabelSelector and AnnotationSelector are always optional.
//
// For JSONPatch, the target is authored explicitly in a `target:` envelope
// alongside the ops array.
type Patch struct {
	File               string
	Flavor             Flavor
	Kind               string
	Name               string
	LabelSelector      string
	AnnotationSelector string
	// Body is the JSON representation of the patch. For StrategicMerge this is
	// the JSON of the partial object (with apiVersion/kind stripped so
	// StrategicMergePatch does not double-set the type). For JSONPatch it is
	// the JSON array of RFC 6902 ops.
	Body []byte
}
