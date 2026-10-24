package declarative

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// Condition types every declarative resource reports.
const (
	// ConditionAccepted is true when the spec is valid, the tenant is known
	// and the key is free.
	ConditionAccepted = "Accepted"
	// ConditionResolvedRefs is true when every reference exists as a resource.
	ConditionResolvedRefs = "ResolvedRefs"
	// ConditionReady is true when the item is projected and live.
	ConditionReady = "Ready"
)

// Condition reasons.
const (
	ReasonAccepted      = "Accepted"
	ReasonInvalid       = "Invalid"
	ReasonNameConflict  = "NameConflict"
	ReasonUnknownTenant = "UnknownTenant"
	ReasonResolved      = "Resolved"
	ReasonRefNotFound   = "RefNotFound"
	ReasonProjected     = "Projected"
	ReasonPending       = "Pending"
)

// SetConditions sets the standard conditions and observedGeneration on a
// status. It sets the Type of each argument. Callers fill Status, Reason and
// Message. A condition whose status is unchanged keeps its lastTransitionTime.
func SetConditions(status *v1alpha1.Status, generation int64, accepted, resolved, ready metav1.Condition) {
	status.ObservedGeneration = generation
	accepted.Type, resolved.Type, ready.Type = ConditionAccepted, ConditionResolvedRefs, ConditionReady
	for _, c := range []metav1.Condition{accepted, resolved, ready} {
		c.ObservedGeneration = generation
		meta.SetStatusCondition(&status.Conditions, c)
	}
}

// True returns a true condition with reason and message.
func True(reason, message string) metav1.Condition {
	return metav1.Condition{Status: metav1.ConditionTrue, Reason: reason, Message: message}
}

// False returns a false condition with reason and message.
func False(reason, message string) metav1.Condition {
	return metav1.Condition{Status: metav1.ConditionFalse, Reason: reason, Message: message}
}
