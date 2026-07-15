// spec.ref resolution for OrbitDevEnvironment (ADR §3, Decision B2).
//
// spec.ref accepts either an immutable tag verbatim or the symbolic "main".
// A symbolic ref (or an empty spec.ref, the CRD's own default) resolves to
// SelfImageTag — the tag THIS operator's own container is running at
// (cmd/operator/config's DevEnvSelfImageTag, threaded in unconditionally by
// charts/orbit-operator/templates/deployment.yaml via the same helper the
// container's own image line uses, so it can never drift out of sync with
// what's actually deployed). A dev environment therefore always matches the
// currently deployed Orbit version exactly — no registry lookup, no
// separately published or floating CI tag, no pinning to a point-in-time
// build that can go stale.
package operator

import (
	"context"
	"fmt"
	"regexp"
)

// devEnvRefPattern matches an immutable spec.ref tag: the same syntax
// cmd/identity's devenvironments feature independently validates API-side
// (WP-10) — both enforcement points exist because the operator is the one
// whose install actually breaks on a garbage ref, and the API is the one
// that can reject it before a CR is even created.
var devEnvRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// resolveRef ensures env.Status.ResolvedRef is set: an immutable tag is
// stored verbatim; the symbolic "main" (or an empty spec.ref) resolves to
// r.SelfImageTag. All subsequent reconciles reuse ResolvedRef — this method
// is a no-op once it's already set. An invalid ref (fails devEnvRefPattern)
// is non-retryable: it sets phase Failed directly, bypassing
// failReconcile's consecutive-failure counter (see failReconcile's doc
// comment, which already flags this exact case), and reports failed=true
// so the caller does not ALSO invoke failReconcile.
func (r *DevEnvironmentReconciler) resolveRef(ctx context.Context, env *OrbitDevEnvironment) (ref string, failed bool, err error) {
	if env.Status.ResolvedRef != "" {
		return env.Status.ResolvedRef, false, nil
	}

	spec := env.Spec.Ref
	switch {
	case spec == "" || spec == "main":
		spec = r.SelfImageTag
	case !devEnvRefPattern.MatchString(spec):
		_ = r.setDevEnvPhase(ctx, env, DevEnvPhaseFailed)
		return "", true, fmt.Errorf("spec.ref %q does not match the immutable-tag pattern", spec)
	}

	if err := r.patchStatusFields(ctx, env, func(s *OrbitDevEnvironmentStatus) {
		s.ResolvedRef = spec
	}); err != nil {
		return "", false, err
	}
	return spec, false, nil
}
