package manifestpatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	jsonpatch "github.com/evanphx/json-patch/v5"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// KnownKinds is the set of Kubernetes Kinds orbit-agents provisions and will
// therefore try to apply patches against. Any patch targeting a Kind outside
// this set is rejected at Validate time.
var KnownKinds = map[string]func() runtime.Object{
	"StatefulSet":             func() runtime.Object { return &appsv1.StatefulSet{} },
	"Service":                 func() runtime.Object { return &corev1.Service{} },
	"ServiceAccount":          func() runtime.Object { return &corev1.ServiceAccount{} },
	"HorizontalPodAutoscaler": func() runtime.Object { return &autoscalingv2.HorizontalPodAutoscaler{} },
	"PodDisruptionBudget":     func() runtime.Object { return &policyv1.PodDisruptionBudget{} },
	"PersistentVolumeClaim":   func() runtime.Object { return &corev1.PersistentVolumeClaim{} },
	"Pod":                     func() runtime.Object { return &corev1.Pod{} },
}

// Validate checks every patch's shape without applying it. Errors surface
// unparseable JSON, empty bodies, unknown Kinds, malformed JSON6902 op arrays,
// malformed label AND annotation selectors (independently), and SMP bodies
// with fields that don't decode onto the target type OR that name a field the
// type does not declare (strict decoding rejects typos like `spec.nodeSeletor`)
// — BEFORE orbit-agents provisions anything.
//
// A patch targeting a Kind orbit-agents does not provision (e.g. a typo like
// "Deployement") is a hard error: silent no-ops on Kind mismatch are the
// exact silent-failure mode we killed the env-var mechanism to avoid.
//
// Validate does NOT dry-apply patches. A patch that is structurally correct
// but semantically fails on a specific runtime object (e.g. a JSON6902
// `replace` on a path that only exists after another patch adds it) is only
// caught at provision time. That is the right tradeoff — every real patch
// depends on the concrete resource shape orbit-agents builds, which is not
// available at startup.
func Validate(patches []Patch) error {
	for _, p := range patches {
		if len(p.Body) == 0 {
			return fmt.Errorf("patch %s: body is empty", p.File)
		}
		factory, ok := KnownKinds[p.Kind]
		if !ok {
			return fmt.Errorf("patch %s: unknown kind %q (must be one of %v)", p.File, p.Kind, sortedKinds())
		}
		if err := validateSelectors(p); err != nil {
			return fmt.Errorf("patch %s: %w", p.File, err)
		}
		switch p.Flavor {
		case JSONPatch:
			if _, err := jsonpatch.DecodePatch(p.Body); err != nil {
				return fmt.Errorf("patch %s: decode json6902: %w", p.File, err)
			}
			if err := checkProtectedPathsJSON6902(p.Body); err != nil {
				return fmt.Errorf("patch %s: %w", p.File, err)
			}
		case StrategicMerge:
			// Strict decode: json.Decoder with DisallowUnknownFields catches
			// field typos (`spec.nodeSeletor`, `spec.templte`) that would
			// otherwise be silently dropped and produce a no-op patch.
			dec := json.NewDecoder(bytes.NewReader(p.Body))
			dec.DisallowUnknownFields()
			obj := factory()
			if err := dec.Decode(obj); err != nil {
				return fmt.Errorf("patch %s: decode onto %s: %w", p.File, p.Kind, err)
			}
			if err := checkProtectedPathsSMP(p.Body); err != nil {
				return fmt.Errorf("patch %s: %w", p.File, err)
			}
		default:
			return fmt.Errorf("patch %s: unknown flavor %v", p.File, p.Flavor)
		}
	}
	return nil
}

// validateSelectors parses both label and annotation selectors independently
// so a malformed second selector is not hidden by an early return in Matches
// (name-check short-circuit).
func validateSelectors(p Patch) error {
	if p.LabelSelector != "" {
		if _, err := labels.Parse(p.LabelSelector); err != nil {
			return fmt.Errorf("parse labelSelector %q: %w", p.LabelSelector, err)
		}
	}
	if p.AnnotationSelector != "" {
		if _, err := labels.Parse(p.AnnotationSelector); err != nil {
			return fmt.Errorf("parse annotationSelector %q: %w", p.AnnotationSelector, err)
		}
	}
	return nil
}

func sortedKinds() []string {
	out := make([]string, 0, len(KnownKinds))
	for k := range KnownKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
