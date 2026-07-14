// spec.ref resolution for OrbitDevEnvironment (ADR §3, Decision B2).
//
// spec.ref accepts either an immutable tag verbatim or the symbolic "main".
// A symbolic ref resolves ONCE, at first provision, to the newest
// main-{shortsha} tag CI has published for orbit-operator (see
// .github/workflows/docker.yml) — recorded as status.resolvedRef, which
// every subsequent reconcile uses instead of re-resolving spec.ref. An
// environment is therefore pinned from birth and never silently drifts onto
// a newer main build than the one it started with.
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

// devEnvRefPattern matches an immutable spec.ref tag: the same syntax
// cmd/identity's devenvironments feature independently validates API-side
// (WP-10) — both enforcement points exist because the operator is the one
// whose install actually breaks on a garbage ref, and the API is the one
// that can reject it before a CR is even created.
var devEnvRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// mainTagPattern matches the immutable tag CI publishes for a main-branch
// build (.github/workflows/docker.yml: main-{7-char-shortsha}).
var mainTagPattern = regexp.MustCompile(`^main-[0-9a-f]{7}$`)

// RefResolver resolves the symbolic ref "main" to the newest immutable
// main-{shortsha} tag published for orbit-operator. Production wiring
// (NewCraneRefResolver) wraps a container registry client; tests inject a
// fake.
type RefResolver interface {
	ResolveMain(ctx context.Context) (tag string, err error)
}

// craneRefResolver is the production RefResolver, backed by
// github.com/google/go-containerregistry's crane package rather than a raw
// http.Client (the noRawHttpClientLiteral lint rule bans http.Client{}
// literals; crane owns its own transport). Anonymous auth via
// authn.DefaultKeychain covers ghcr.io's public images and gives the same
// node-credential parity a real cluster's kubelet pull would have, without
// any Orbit-specific auth code (Track V item 3).
type craneRefResolver struct {
	repo string
}

// NewCraneRefResolver returns a RefResolver for "{imageRegistry}/orbit-operator"
// (or bare "orbit-operator" when imageRegistry is empty, matching
// inVClusterOperatorImageRepo's own convention).
func NewCraneRefResolver(imageRegistry string) RefResolver {
	repo := "orbit-operator"
	if imageRegistry != "" {
		repo = imageRegistry + "/orbit-operator"
	}
	return &craneRefResolver{repo: repo}
}

func (c *craneRefResolver) ResolveMain(ctx context.Context) (string, error) {
	tags, err := crane.ListTags(c.repo, crane.WithContext(ctx), crane.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", fmt.Errorf("list tags for %q: %w", c.repo, err)
	}

	var candidates []string
	for _, t := range tags {
		if mainTagPattern.MatchString(t) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no main-{shortsha} tags found for %q", c.repo)
	}

	// git shas aren't chronologically sortable, so "newest" is determined by
	// the registry-reported image config's own "created" timestamp, not
	// lexical tag order.
	createdAt := make(map[string]time.Time, len(candidates))
	for _, tag := range candidates {
		cfg, configErr := crane.Config(c.repo+":"+tag, crane.WithContext(ctx), crane.WithAuthFromKeychain(authn.DefaultKeychain))
		if configErr != nil {
			// Best-effort: one tag's config being unreadable (e.g. GC'd
			// manifest) must not fail resolution of every other candidate.
			continue
		}
		var parsed struct {
			Created time.Time `json:"created"`
		}
		if jsonErr := json.Unmarshal(cfg, &parsed); jsonErr != nil {
			continue
		}
		createdAt[tag] = parsed.Created
	}

	return pickNewestMainTag(candidates, createdAt)
}

// pickNewestMainTag returns the tag from candidates with the latest
// createdAt entry. Pure and registry-free so it is unit-testable without a
// real registry. A tag missing from createdAt (its Config fetch failed) is
// skipped, never treated as "oldest" by a zero time.Time comparing least.
func pickNewestMainTag(candidates []string, createdAt map[string]time.Time) (string, error) {
	best := ""
	var bestTime time.Time
	for _, tag := range candidates {
		t, ok := createdAt[tag]
		if !ok {
			continue
		}
		if best == "" || t.After(bestTime) {
			best = tag
			bestTime = t
		}
	}
	if best == "" {
		return "", fmt.Errorf("could not determine a created time for any of %d candidate tag(s)", len(candidates))
	}
	return best, nil
}

// resolveRef ensures env.Status.ResolvedRef is set, resolving spec.ref once
// at first provision: an immutable tag is stored verbatim; the symbolic
// "main" resolves via RefResolver to the newest main-{shortsha} tag. All
// subsequent reconciles reuse ResolvedRef — this method is a no-op once it's
// already set, so an environment is pinned from birth and never silently
// drifts. An unresolvable ref (invalid syntax, or RefResolver
// failure/empty result for "main") is non-retryable: it sets phase Failed
// directly, bypassing failReconcile's consecutive-failure counter (see
// failReconcile's doc comment, which already flags this exact case), and
// reports failed=true so the caller does not ALSO invoke failReconcile.
func (r *DevEnvironmentReconciler) resolveRef(ctx context.Context, env *OrbitDevEnvironment) (ref string, failed bool, err error) {
	if env.Status.ResolvedRef != "" {
		return env.Status.ResolvedRef, false, nil
	}

	spec := env.Spec.Ref
	if spec == "" || spec == "main" {
		tag, resolveErr := r.RefResolver.ResolveMain(ctx)
		if resolveErr != nil {
			_ = r.setDevEnvPhase(ctx, env, DevEnvPhaseFailed)
			return "", true, fmt.Errorf("resolve symbolic ref %q: %w", spec, resolveErr)
		}
		spec = tag
	} else if !devEnvRefPattern.MatchString(spec) {
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
