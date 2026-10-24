package operator

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dv1 "github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// Condition types every declarative resource reports.
const (
	// condAccepted is true when the spec is valid, the tenant is known
	// and the key is free.
	condAccepted = "Accepted"
	// condResolvedRefs is true when every reference to another declared
	// kind is Ready and every other reference exists.
	condResolvedRefs = "ResolvedRefs"
	// condReady is true when the item is projected and live.
	condReady = "Ready"
)

// Condition reasons.
const (
	reasonAccepted      = "Accepted"
	reasonInvalid       = "Invalid"
	reasonNameConflict  = "NameConflict"
	reasonUnknownTenant = "UnknownTenant"
	reasonResolved      = "Resolved"
	reasonRefNotFound   = "RefNotFound"
	reasonProjected     = "Projected"
	reasonPending       = "Pending"
)

// setConditions sets the standard conditions and observedGeneration on a
// status. It sets the Type of each argument. Callers fill Status, Reason and
// Message. A condition whose status is unchanged keeps its lastTransitionTime.
func setConditions(status *dv1.Status, generation int64, accepted, resolved, ready metav1.Condition) {
	status.ObservedGeneration = generation
	accepted.Type, resolved.Type, ready.Type = condAccepted, condResolvedRefs, condReady
	for _, c := range []metav1.Condition{accepted, resolved, ready} {
		c.ObservedGeneration = generation
		meta.SetStatusCondition(&status.Conditions, c)
	}
}

// condTrue returns a true condition with reason and message.
func condTrue(reason, message string) metav1.Condition {
	return metav1.Condition{Status: metav1.ConditionTrue, Reason: reason, Message: message}
}

// condFalse returns a false condition with reason and message.
func condFalse(reason, message string) metav1.Condition {
	return metav1.Condition{Status: metav1.ConditionFalse, Reason: reason, Message: message}
}
