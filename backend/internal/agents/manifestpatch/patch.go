// Package manifestpatch applies operator-authored kustomize patches to the
// Kubernetes objects orbit-agents provisions, using the kustomize engine.
//
// See docs/adr/2026-09-15-manifest-patches-configmap.md.
package manifestpatch

import "sigs.k8s.io/kustomize/api/types"

// Patch is one kustomize `patches:` entry, read from a file in the patch
// directory. File names the source in errors.
type Patch struct {
	File  string
	Entry types.Patch
}

// apiVersions maps each Kind orbit-agents provisions to its apiVersion.
// A patch can only target these Kinds.
var apiVersions = map[string]string{
	"StatefulSet":             "apps/v1",
	"Service":                 "v1",
	"ServiceAccount":          "v1",
	"HorizontalPodAutoscaler": "autoscaling/v2",
	"PodDisruptionBudget":     "policy/v1",
	"PersistentVolumeClaim":   "v1",
	"Pod":                     "v1",
}
