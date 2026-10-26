package declarative

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

func TestSetConditions(t *testing.T) {
	var s v1alpha1.Status
	SetConditions(&s, 3, True(ReasonAccepted, ""), False(ReasonRefNotFound, `Agent "a" not found`), False(ReasonPending, ""))
	if s.ObservedGeneration != 3 || len(s.Conditions) != 3 {
		t.Fatalf("status = %+v", s)
	}
	for i, want := range []string{ConditionAccepted, ConditionResolvedRefs, ConditionReady} {
		c := s.Conditions[i]
		if c.Type != want || c.ObservedGeneration != 3 || c.LastTransitionTime.IsZero() {
			t.Errorf("condition %d = %+v, want type %s", i, c, want)
		}
	}
	resolved := meta.FindStatusCondition(s.Conditions, ConditionResolvedRefs)
	if resolved.Status != metav1.ConditionFalse || resolved.Reason != ReasonRefNotFound || resolved.Message != `Agent "a" not found` {
		t.Errorf("ResolvedRefs = %+v", resolved)
	}

	past := metav1.NewTime(time.Unix(1, 0))
	for i := range s.Conditions {
		s.Conditions[i].LastTransitionTime = past
	}
	SetConditions(&s, 4, True(ReasonAccepted, ""), True(ReasonResolved, ""), False(ReasonPending, ""))
	if s.ObservedGeneration != 4 {
		t.Errorf("observedGeneration = %d", s.ObservedGeneration)
	}
	if c := meta.FindStatusCondition(s.Conditions, ConditionAccepted); !c.LastTransitionTime.Equal(&past) || c.ObservedGeneration != 4 {
		t.Errorf("an unchanged condition keeps its transition time: %+v", c)
	}
	if c := meta.FindStatusCondition(s.Conditions, ConditionResolvedRefs); c.LastTransitionTime.Equal(&past) || c.Status != metav1.ConditionTrue {
		t.Errorf("a changed condition moves its transition time: %+v", c)
	}
}
