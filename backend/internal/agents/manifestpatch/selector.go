package manifestpatch

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

// Matches reports whether p targets a resource with the given kind, name,
// labels, and annotations.
//
// Kind must match exactly. Empty Name on the Patch acts as a wildcard.
// LabelSelector and AnnotationSelector, when non-empty, are parsed as
// Kubernetes label selectors and must match the resource's own labels and
// annotations respectively.
func (p Patch) Matches(kind, name string, resourceLabels, resourceAnnotations map[string]string) (bool, error) {
	if p.Kind != kind {
		return false, nil
	}
	if p.Name != "" && p.Name != name {
		return false, nil
	}
	if p.LabelSelector != "" {
		sel, err := labels.Parse(p.LabelSelector)
		if err != nil {
			return false, fmt.Errorf("parse labelSelector %q: %w", p.LabelSelector, err)
		}
		if !sel.Matches(labels.Set(resourceLabels)) {
			return false, nil
		}
	}
	if p.AnnotationSelector != "" {
		sel, err := labels.Parse(p.AnnotationSelector)
		if err != nil {
			return false, fmt.Errorf("parse annotationSelector %q: %w", p.AnnotationSelector, err)
		}
		if !sel.Matches(labels.Set(resourceAnnotations)) {
			return false, nil
		}
	}
	return true, nil
}

// FilterForResource returns the ordered subset of patches whose selector
// matches the given kind, name, labels, and annotations.
func FilterForResource(patches []Patch, kind, name string, resourceLabels, resourceAnnotations map[string]string) ([]Patch, error) {
	var out []Patch
	for _, p := range patches {
		ok, err := p.Matches(kind, name, resourceLabels, resourceAnnotations)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}
