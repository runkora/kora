// Host-provided workload identity for OrbitDevEnvironment (decision 3 /
// option C3 of the prototype spike).
//
// SPIRE cannot run inside a vcluster — its agent attests via the host
// kubelet, where synced pods appear under translated names (the ADR's Option
// C1 rejection). But every pod a vcluster syncs onto a host node is a real
// host pod, so the HOST's own SPIRE agents can attest it directly, with no
// proxy layer required. This file creates the ClusterSPIFFEID the Phase 1
// spike (scripts/cluster/spike-identity-wire.sh) proved works end-to-end: a
// single, namespace-scoped grant covering every pod synced into an
// environment's flattened host namespace, minting path-scoped SVIDs under
// spiffe://{trustDomain}/devenv/{env-id}/{pod-name} — stripped back to a bare
// principal by principalFromSpiffeID
// (backend/internal/identity/principal_extractor.go).
//
// This must exist before any in-vcluster workload that requests a SPIFFE SVID
// via the CSI socket starts: the Istio ingress gateway (and any
// SPIRE-cert-provider sidecar) hangs indefinitely waiting on a SPIRE
// registration entry that never arrives otherwise — the same ordering hazard
// PlatformReconciler.reconcileReleases documents for the host's own
// orbit-platform-before-gateway sequencing.
package operator

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// spireClassName matches the SPIRE controller manager's configured className
// (controller-manager-config.yaml, field: className) — the same value the
// orbit-platform chart's own ClusterSPIFFEID templates
// (charts/orbit-platform/templates/clusterSpiffeIDs.yaml) use.
const spireClassName = "spire-system-spire"

func clusterSPIFFEIDGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "spire.spiffe.io", Version: "v1alpha1", Kind: "ClusterSPIFFEID"}
}

// devEnvClusterSPIFFEIDName names the catch-all host ClusterSPIFFEID
// granting identity to pods an environment's vcluster syncs onto the host.
func devEnvClusterSPIFFEIDName(env *OrbitDevEnvironment) string {
	return "devenv-" + env.Name
}

// devEnvComponentClusterSPIFFEIDName names the component-scoped host
// ClusterSPIFFEID minting STABLE per-service paths.
func devEnvComponentClusterSPIFFEIDName(env *OrbitDevEnvironment) string {
	return "devenv-" + env.Name + "-system"
}

// devEnvClusterSPIFFEIDSpecs returns the host ClusterSPIFFEID specs for an
// environment, keyed by object name. Two disjoint grants (a pod can only
// ever match one, so SPIRE never issues competing registrations):
//
//   - Pods carrying the spiffe.io/component label — the orbit chart's own
//     system services, whose labels the vcluster syncer propagates verbatim
//     (Phase 1 spike, question 2) — get the STABLE path
//     devenv/{env}/system/{component}, mirroring the host's own
//     orbit-system-component convention. Stable paths are what the chart's
//     DestinationRules pin as SANs (platform.spiffe.devEnvID) and what
//     principalFromSpiffeID strips back to a system/{component} principal.
//   - Everything else (the Istio gateway, probe pods, tenant workloads) gets
//     the per-pod-name path devenv/{env}/{pod} — attribution without a
//     stable-identity claim.
func devEnvClusterSPIFFEIDSpecs(env *OrbitDevEnvironment, trustDomain string) map[string]map[string]any {
	namespaceSelector := map[string]any{
		"matchLabels": map[string]any{
			"orbit.io/devenv": env.Name,
		},
	}
	return map[string]map[string]any{
		devEnvComponentClusterSPIFFEIDName(env): {
			"className": spireClassName,
			"spiffeIDTemplate": fmt.Sprintf(
				`spiffe://%s/devenv/%s/system/{{ index .PodMeta.Labels "spiffe.io/component" }}`,
				trustDomain, env.Name),
			"namespaceSelector": namespaceSelector,
			"podSelector": map[string]any{
				"matchExpressions": []any{
					map[string]any{"key": "spiffe.io/component", "operator": "Exists"},
				},
			},
		},
		devEnvClusterSPIFFEIDName(env): {
			"className":         spireClassName,
			"spiffeIDTemplate":  fmt.Sprintf("spiffe://%s/devenv/%s/{{ .PodMeta.Name }}", trustDomain, env.Name),
			"namespaceSelector": namespaceSelector,
			"podSelector": map[string]any{
				"matchExpressions": []any{
					map[string]any{"key": "spiffe.io/component", "operator": "DoesNotExist"},
				},
			},
		},
	}
}

// ensureHostIdentity creates or updates the host-side ClusterSPIFFEIDs for an
// environment's namespace. Unstructured rather than a typed client: the
// operator has no Go SDK dependency on spire.spiffe.io/v1alpha1 today (the
// host's own ClusterSPIFFEIDs are Helm-templated YAML, never constructed in
// Go), and adding one for a single resource type is not worth the new
// dependency.
func (r *DevEnvironmentReconciler) ensureHostIdentity(ctx context.Context, env *OrbitDevEnvironment) error {
	trustDomain, err := r.hostTrustDomain(ctx)
	if err != nil {
		return fmt.Errorf("determine host trust domain: %w", err)
	}

	for name, spec := range devEnvClusterSPIFFEIDSpecs(env, trustDomain) {
		if err := r.ensureClusterSPIFFEID(ctx, name, spec); err != nil {
			return err
		}
	}
	return nil
}

func (r *DevEnvironmentReconciler) ensureClusterSPIFFEID(ctx context.Context, name string, spec map[string]any) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(clusterSPIFFEIDGVK())
	err := r.Get(ctx, client.ObjectKey{Name: name}, existing)
	switch {
	case apierrors.IsNotFound(err):
		desired := &unstructured.Unstructured{}
		desired.SetGroupVersionKind(clusterSPIFFEIDGVK())
		desired.SetName(name)
		if setErr := unstructured.SetNestedMap(desired.Object, spec, "spec"); setErr != nil {
			return fmt.Errorf("build ClusterSPIFFEID %q spec: %w", name, setErr)
		}
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create ClusterSPIFFEID %q: %w", name, createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get ClusterSPIFFEID %q: %w", name, err)
	default:
		if setErr := unstructured.SetNestedMap(existing.Object, spec, "spec"); setErr != nil {
			return fmt.Errorf("set ClusterSPIFFEID %q spec: %w", name, setErr)
		}
		if updateErr := r.Update(ctx, existing); updateErr != nil {
			return fmt.Errorf("update ClusterSPIFFEID %q: %w", name, updateErr)
		}
		return nil
	}
}

// deleteHostIdentity removes an environment's host ClusterSPIFFEIDs during
// cleanup. Idempotent — called from handleDeletion alongside the vcluster
// Helm uninstall and namespace delete.
func (r *DevEnvironmentReconciler) deleteHostIdentity(ctx context.Context, env *OrbitDevEnvironment) error {
	for _, name := range []string{devEnvClusterSPIFFEIDName(env), devEnvComponentClusterSPIFFEIDName(env)} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(clusterSPIFFEIDGVK())
		obj.SetName(name)
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete ClusterSPIFFEID %q: %w", name, err)
		}
	}
	return nil
}

// istioCARootCertConfigMapName is the fixed name Istio's own istiod
// namespace-controller uses for the CA-root ConfigMap it publishes into
// every namespace it discovers — Istio's sidecar/gateway injection templates
// always mount it under this exact literal name (see the "istiod-ca-cert"
// volume in charts/orbit-operator's injection templates and any istiod
// chart's own webhook config), never a computed one.
const istioCARootCertConfigMapName = "istio-ca-root-cert"

// mirrorIstioCARootCert copies the in-vcluster istiod's own CA-root
// ConfigMap into a plain-named copy in the same flattened host namespace.
//
// Every pod istiod's sidecar-injection webhook touches mounts a volume
// literally named "istio-ca-root-cert" — a reference baked into the pod spec
// inside the vcluster's own API server, before the syncer ever sees it. The
// vcluster syncer does sync the ConfigMap itself down to the host, but (per
// its usual non-core-resource convention) under a translated name
// ("istio-ca-root-cert-x-istio-system-x-vcluster"), and it does not rewrite
// the pod's already-admitted volume reference to match — so every
// SPIRE/Istio-injected pod in a devenvironment hangs on a FailedMount for a
// ConfigMap that, under its expected literal name, never exists.
//
// Before decision 1's discoverySelectors exclusion (see reconcileIstiod),
// this went unnoticed: the HOST's own istiod (with no namespace scoping)
// independently published its OWN "istio-ca-root-cert" under the literal
// name into every namespace it could see, including devenv ones — silently
// satisfying the mount, but with the HOST's root, not the devenv's own
// istiod's, breaking XDS certificate validation. Now that the host is
// correctly excluded, nothing publishes the literal name at all, so this
// reconciler must — with the *correct* (devenv's own) root instead.
//
// Best-effort and non-fatal: the source ConfigMap only exists once the
// in-vcluster istiod has started and completed its own namespace-controller
// pass, which can be later than other parts of reconcileReady.
func (r *DevEnvironmentReconciler) mirrorIstioCARootCert(ctx context.Context, env *OrbitDevEnvironment) error {
	ns := devEnvNamespace(env)
	sourceName := fmt.Sprintf("%s-x-istio-system-x-%s", istioCARootCertConfigMapName, vclusterReleaseName)

	source := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: sourceName, Namespace: ns}, source); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get synced istiod CA root configmap %q: %w", sourceName, err)
	}

	dest := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Name: istioCARootCertConfigMapName, Namespace: ns}, dest)
	switch {
	case apierrors.IsNotFound(err):
		desired := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: istioCARootCertConfigMapName, Namespace: ns},
			Data:       source.Data,
		}
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create mirrored istiod CA root configmap: %w", createErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("get mirrored istiod CA root configmap: %w", err)
	default:
		if maps.Equal(dest.Data, source.Data) {
			return nil
		}
		dest.Data = source.Data
		if updateErr := r.Update(ctx, dest); updateErr != nil {
			return fmt.Errorf("update mirrored istiod CA root configmap: %w", updateErr)
		}
		return nil
	}
}
