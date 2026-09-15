package manifestpatch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// protectedPaths is the set of JSON paths (dotted / slash notation) that
// patches are forbidden from touching, because orbit-agents' identity and
// security invariants depend on them staying strong-typed. Overriding these
// leaves a provisioned resource outside the SPIFFE/pod-security/cross-
// resource-identity boundary the strong-typed builder guarantees.
//
// The ADR is explicit that patches layer on top; they never remove the
// invariants the builder bakes in. Rejecting at Validate time enforces that.
//
// Paths are matched as prefixes: "metadata.ownerReferences" catches both
// wholesale replacement and /metadata/ownerReferences/0/... in JSON6902.
var protectedPaths = []string{
	// Identity / cross-resource wiring — changing any of these orphans the
	// StatefulSet from its Service / SA / HPA / PDB by name.
	"metadata.name",
	"metadata.namespace",
	"metadata.ownerReferences",
	// SPIFFE labels used by SPIRE ClusterSPIFFEID templates. Removing them
	// makes the pod unattested; every downstream authz check fails closed
	// but the failure mode is a mystery to the operator.
	"metadata.labels.spiffe.io/agent",
	"metadata.labels.spiffe.io/spire-managed-identity",
	"metadata.labels.orbit.io/identity-proxy",
	"metadata.labels.orbit.io/tenant",
	"metadata.labels.orbit.io/agent-type",
	// Selectors must match pod labels; a mismatched selector produces a
	// silently un-scheduled StatefulSet.
	"spec.selector",
	"spec.serviceName",
	"spec.scaleTargetRef",
	// ServiceAccount wiring. The pod's SPIFFE ID derives from this, so
	// changing it silently drops the pod off its attested identity.
	// Both forms: StatefulSet's pod template, and a bare Pod (sandbox).
	"spec.template.spec.serviceAccountName",
	"spec.serviceAccountName",
	// Pod security context — the builder enforces restricted PSS
	// (RunAsNonRoot, RunAsUser=100, SeccompProfile=RuntimeDefault). A patch
	// that weakens these is a security regression the ADR forbids.
	"spec.template.spec.securityContext.runAsNonRoot",
	"spec.template.spec.securityContext.runAsUser",
	"spec.template.spec.securityContext.seccompProfile",
	"spec.securityContext.runAsNonRoot",
	"spec.securityContext.runAsUser",
	"spec.securityContext.seccompProfile",
}

// checkProtectedPathsSMP walks a strategic merge patch body and rejects any
// key path that matches a protected invariant.
func checkProtectedPathsSMP(body []byte) error {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		// Body is malformed as JSON; strict decode elsewhere will flag it.
		return nil
	}
	return walkForProtected(doc, "")
}

func walkForProtected(v any, path string) error {
	if hit := matchProtected(path); hit != "" {
		return fmt.Errorf("patch touches protected path %q (matches %q); see ADR 2026-09-15-manifest-patches-configmap.md", path, hit)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for k, child := range m {
		next := k
		if path != "" {
			next = path + "." + k
		}
		if err := walkForProtected(child, next); err != nil {
			return err
		}
	}
	return nil
}

// checkProtectedPathsJSON6902 rejects RFC 6902 ops whose path touches a
// protected invariant. Paths are JSON pointers ("/spec/serviceAccountName",
// "/metadata/labels/spiffe.io~1agent"); convert to the dotted form used by
// protectedPaths, unescaping "~1"->"/" and "~0"->"~" per RFC 6901 §3 so a
// label key containing a literal "/" (e.g. "spiffe.io/agent") still matches.
func checkProtectedPathsJSON6902(body []byte) error {
	var ops []struct {
		Op   string `json:"op"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &ops); err != nil {
		return nil
	}
	for _, op := range ops {
		if op.Path == "" {
			continue
		}
		segments := strings.Split(strings.TrimPrefix(op.Path, "/"), "/")
		for i, seg := range segments {
			seg = strings.ReplaceAll(seg, "~1", "/")
			seg = strings.ReplaceAll(seg, "~0", "~")
			segments[i] = seg
		}
		dotted := strings.Join(segments, ".")
		if hit := matchProtected(dotted); hit != "" {
			return fmt.Errorf("json6902 %s targets protected path %q (matches %q); see ADR 2026-09-15-manifest-patches-configmap.md", op.Op, op.Path, hit)
		}
	}
	return nil
}

// matchProtected returns the protected prefix that path falls under, or "".
func matchProtected(path string) string {
	if path == "" {
		return ""
	}
	for _, p := range protectedPaths {
		if path == p || strings.HasPrefix(path, p+".") || strings.HasPrefix(path, p+"/") {
			return p
		}
	}
	return ""
}
