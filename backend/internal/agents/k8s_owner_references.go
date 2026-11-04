package agents

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// koraTenantGVR is the GroupVersionResource for the cluster-scoped KoraTenant
// CR defined by the operator (backend/internal/operator/models.go). Duplicated
// here — as a three-field literal — so backend/internal/agents does not import
// backend/internal/operator (feature packages must not import each other).
var koraTenantGVR = schema.GroupVersionResource{
	Group:    "runkora.io",
	Version:  "v1alpha1",
	Resource: "koratenants",
}

const (
	koraTenantAPIVersion = "runkora.io/v1alpha1"
	koraTenantKind       = "KoraTenant"
)

// tenantOwnerRef fetches the cluster-scoped KoraTenant named tenantID and
// returns a controller OwnerReference for it. Returns the zero value with a
// nil error when the tenant cannot be reached (missing CRD, missing CR,
// permission denied) — callers must treat an empty ref as "skip ownership"
// and proceed unowned. Local development without the operator installed
// remains supported.
func (p *K8sProvisioner) tenantOwnerRef(ctx context.Context, tenantID string) metav1.OwnerReference {
	if tenantID == "" {
		return metav1.OwnerReference{}
	}
	obj, err := p.dynamicClient.Resource(koraTenantGVR).Get(ctx, tenantID, metav1.GetOptions{})
	if err != nil {
		degraded := apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || isNoKindMatchError(err)
		if degraded {
			p.logger.Warn(ctx, "KoraTenant unavailable, provisioning without owner reference",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		} else {
			p.logger.Warn(ctx, "fetch KoraTenant for owner reference failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
		return metav1.OwnerReference{}
	}
	yes := true
	return metav1.OwnerReference{
		APIVersion:         koraTenantAPIVersion,
		Kind:               koraTenantKind,
		Name:               obj.GetName(),
		UID:                obj.GetUID(),
		Controller:         &yes,
		BlockOwnerDeletion: &yes,
	}
}

// applyOwnerRef merges ref into meta.OwnerReferences, replacing any existing
// entry for the same (APIVersion, Kind, Name) tuple. No-op when ref is the
// zero value.
func applyOwnerRef(meta *metav1.ObjectMeta, ref metav1.OwnerReference) {
	if ref.Kind == "" || ref.Name == "" {
		return
	}
	meta.OwnerReferences = mergeOwnerRefs(meta.OwnerReferences, ref)
}

// mergeOwnerRefs returns refs with ref merged in — replacing any existing
// entry that matches on (APIVersion, Kind, Name), otherwise appending.
// Also used to backfill unstructured (Istio) objects on the update path via
// SetOwnerReferences(mergeOwnerRefs(GetOwnerReferences(), ref)).
func mergeOwnerRefs(refs []metav1.OwnerReference, ref metav1.OwnerReference) []metav1.OwnerReference {
	for i, existing := range refs {
		if existing.APIVersion == ref.APIVersion && existing.Kind == ref.Kind && existing.Name == ref.Name {
			refs[i] = ref
			return refs
		}
	}
	return append(refs, ref)
}

// setUnstructuredOwnerRef mirrors applyOwnerRef for the unstructured objects
// (Istio DestinationRule, AuthorizationPolicy) that the provisioner creates
// via the dynamic client.
func setUnstructuredOwnerRef(metadata map[string]any, ref metav1.OwnerReference) {
	if ref.Kind == "" || ref.Name == "" {
		return
	}
	controller := false
	if ref.Controller != nil {
		controller = *ref.Controller
	}
	block := false
	if ref.BlockOwnerDeletion != nil {
		block = *ref.BlockOwnerDeletion
	}
	entry := map[string]any{
		"apiVersion":         ref.APIVersion,
		"kind":               ref.Kind,
		"name":               ref.Name,
		"uid":                string(ref.UID),
		"controller":         controller,
		"blockOwnerDeletion": block,
	}
	// Best-effort merge against the existing slice. The type assertion drops
	// its ok flag on purpose: if metadata["ownerReferences"] is anything other
	// than []any (nil, an unexpected shape, corrupt input) we start over from
	// nil and append — the ref still lands. Per-entry assertions inside the
	// loop skip malformed entries individually rather than fail the whole
	// merge.
	existing, _ := metadata["ownerReferences"].([]any)
	for i, raw := range existing {
		e, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if e["apiVersion"] == ref.APIVersion && e["kind"] == ref.Kind && e["name"] == ref.Name {
			existing[i] = entry
			metadata["ownerReferences"] = existing
			return
		}
	}
	metadata["ownerReferences"] = append(existing, entry)
}

// statefulSetOwnerRef fetches the agent StatefulSet named slug in namespace
// and returns a non-controller OwnerReference for it. Returns the zero value
// when the StatefulSet cannot be fetched — sandbox creation must never fail
// on the ownerRef lookup alone. Non-controller (`Controller=false,
// BlockOwnerDeletion=false`) because the StatefulSet's own controller field
// belongs to the StatefulSet controller; we participate only in garbage
// collection.
func (p *K8sProvisioner) statefulSetOwnerRef(ctx context.Context, slug, namespace string) metav1.OwnerReference {
	if slug == "" {
		return metav1.OwnerReference{}
	}
	ss, err := p.client.AppsV1().StatefulSets(namespace).Get(ctx, slug, metav1.GetOptions{})
	if err != nil {
		p.logger.Warn(ctx, "fetch parent StatefulSet for sandbox owner reference failed",
			logging.Field{Key: "namespace", Value: namespace},
			logging.Field{Key: "slug", Value: slug},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return metav1.OwnerReference{}
	}
	no := false
	return metav1.OwnerReference{
		APIVersion:         "apps/v1",
		Kind:               "StatefulSet",
		Name:               ss.GetName(),
		UID:                ss.GetUID(),
		Controller:         &no,
		BlockOwnerDeletion: &no,
	}
}

// isNoKindMatchError returns true when err reports that the KoraTenant CRD is
// not installed. String matching avoids importing k8s.io/apimachinery/pkg/api/meta
// solely for a single sentinel check.
func isNoKindMatchError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "the server could not find the requested resource")
}
