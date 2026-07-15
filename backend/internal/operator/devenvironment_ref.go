// spec.ref resolution for OrbitDevEnvironment (ADR §3, Decision B2).
//
// spec.ref accepts either an immutable tag verbatim or the symbolic "main".
// A symbolic ref (or an empty spec.ref, the CRD's own default) resolves to
// the literal, floating tag "main" — CI (.github/workflows/docker.yml)
// republishes that tag on every push to the main branch, and every
// in-vcluster image is pulled with pullPolicy: Always for it
// (inVClusterOperatorValues, inVClusterServiceImageTags), so a dev
// environment always runs current main rather than whatever main happened
// to be at first provision. There is deliberately no version pinning here:
// pin an explicit immutable tag in spec.ref instead if that's what you want.
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
// the literal floating tag "main". All subsequent reconciles reuse
// ResolvedRef — this method is a no-op once it's already set. An invalid
// ref (fails devEnvRefPattern) is non-retryable: it sets phase Failed
// directly, bypassing failReconcile's consecutive-failure counter (see
// failReconcile's doc comment, which already flags this exact case), and
// reports failed=true so the caller does not ALSO invoke failReconcile.
func (r *DevEnvironmentReconciler) resolveRef(ctx context.Context, env *OrbitDevEnvironment) (ref string, failed bool, err error) {
	if env.Status.ResolvedRef != "" {
		return env.Status.ResolvedRef, false, nil
	}

	spec := env.Spec.Ref
	switch {
	case spec == "" || spec == "main":
		spec = "main"
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
