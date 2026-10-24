package operator

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dv1 "github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

func TestSetConditions(t *testing.T) {
	var s dv1.Status
	setConditions(&s, 3, condTrue(reasonAccepted, ""), condFalse(reasonRefNotFound, `Agent "a" not found`), condFalse(reasonPending, ""))
	if s.ObservedGeneration != 3 || len(s.Conditions) != 3 {
		t.Fatalf("status = %+v", s)
	}
	for i, want := range []string{condAccepted, condResolvedRefs, condReady} {
		c := s.Conditions[i]
		if c.Type != want || c.ObservedGeneration != 3 || c.LastTransitionTime.IsZero() {
			t.Errorf("condition %d = %+v, want type %s", i, c, want)
		}
	}
	resolved := meta.FindStatusCondition(s.Conditions, condResolvedRefs)
	if resolved.Status != metav1.ConditionFalse || resolved.Reason != reasonRefNotFound || resolved.Message != `Agent "a" not found` {
		t.Errorf("ResolvedRefs = %+v", resolved)
	}

	past := metav1.NewTime(time.Unix(1, 0))
	for i := range s.Conditions {
		s.Conditions[i].LastTransitionTime = past
	}
	setConditions(&s, 4, condTrue(reasonAccepted, ""), condTrue(reasonResolved, ""), condFalse(reasonPending, ""))
	if s.ObservedGeneration != 4 {
		t.Errorf("observedGeneration = %d", s.ObservedGeneration)
	}
	if c := meta.FindStatusCondition(s.Conditions, condAccepted); !c.LastTransitionTime.Equal(&past) || c.ObservedGeneration != 4 {
		t.Errorf("an unchanged condition keeps its transition time: %+v", c)
	}
	if c := meta.FindStatusCondition(s.Conditions, condResolvedRefs); c.LastTransitionTime.Equal(&past) || c.Status != metav1.ConditionTrue {
		t.Errorf("a changed condition moves its transition time: %+v", c)
	}
}
