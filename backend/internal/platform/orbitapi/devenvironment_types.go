// OrbitDevEnvironment CRD Go types, shared between the operator (which
// reconciles the CR) and cmd/identity's devenvironments feature (which
// creates/reads it, decision 5 of
// docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).
//
// This package exists so both consumers can depend on the same type without
// either importing the other's feature package — internal/operator is
// itself a feature package under this repo's "features never import each
// other" convention, so internal/devenvironments importing it directly
// would violate that rule. internal/operator type-aliases these definitions
// (see its own devenvironment_types.go) so its existing reconciler code is
// unaffected by the move.
//
// An OrbitDevEnvironment declares a disposable, isolated, full-stack Orbit
// installation running as a vcluster in its own host namespace
// (orbit-devenv-{name}). The operator's DevEnvironmentReconciler turns the CR
// into a namespace + vcluster Helm release, drives TTL expiry and idle-based
// sleep/wake, and signs kubeconfig CSRs against the vcluster's own client CA
// (option C3's host-provided identity model governs workload identity
// separately — this CRD only governs the environment's lifecycle).
package orbitapi

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// -----------------------------------------------------------------------------
// OrbitDevEnvironment — cluster-scoped, one per ephemeral environment
// -----------------------------------------------------------------------------

// OrbitDevEnvironment declares the desired state of one ephemeral development
// environment. The CR name is the environment ID — the operator derives the
// host namespace as "orbit-devenv-{name}".
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster;shortName=ode
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name="Awake",type=boolean,JSONPath=`.spec.awake`
// +kubebuilder:printcolumn:name="ExpiresAt",type=date,JSONPath=`.status.expiresAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type OrbitDevEnvironment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrbitDevEnvironmentSpec   `json:"spec,omitempty"`
	Status OrbitDevEnvironmentStatus `json:"status,omitempty"`
}

// OrbitDevEnvironmentSpec defines the desired state of a dev environment.
type OrbitDevEnvironmentSpec struct {
	// Owner is the principal (human identity) that provisioned this
	// environment — set once at creation and never mutated by the reconciler.
	// Every state transition is attributed to it for audit.
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// Ref selects the chart/image ref installed inside the environment.
	// Defaults to "main" (latest main build).
	// +kubebuilder:default=main
	// +optional
	Ref string `json:"ref,omitempty"`

	// TTL bounds how long the environment lives from creation before the
	// reconciler deletes it. Extendable up to TTLHardCap by patching this
	// field to a larger value — the reconciler never extends it on its own.
	// +kubebuilder:default="24h"
	// +optional
	TTL metav1.Duration `json:"ttl,omitempty"`

	// IdleTimeout is how long the environment can go without activity before
	// the reconciler puts it to sleep (scales workloads to zero, retains
	// PVCs). Independent of TTL — an idle environment still expires on
	// schedule while asleep.
	// +kubebuilder:default="2h"
	// +optional
	IdleTimeout metav1.Duration `json:"idleTimeout,omitempty"`

	// Awake is the explicit desired power state. Sleep can also be triggered
	// automatically by IdleTimeout; wake is never automatic (decision 8 — no
	// wake-on-connection) and only happens by flipping this to true.
	// +kubebuilder:default=true
	Awake bool `json:"awake"`

	// KubeconfigCSR is a PEM-encoded PKCS#10 certificate signing request
	// submitted on the developer's behalf (decision 6). The reconciler signs
	// it against the vcluster's own client CA and publishes the resulting
	// certificate in status — the private key never transits the CR.
	// +optional
	KubeconfigCSR string `json:"kubeconfigCSR,omitempty"`
}

// OrbitDevEnvironmentStatus records observed state.
type OrbitDevEnvironmentStatus struct {
	// Phase summarizes the environment's lifecycle state.
	// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Sleeping;Expired;Failed
	Phase string `json:"phase,omitempty"`

	// Namespace is the host namespace backing this environment
	// ("orbit-devenv-{name}").
	Namespace string `json:"namespace,omitempty"`

	// IngressHost ("{name}.{ingressDomain}") is the per-environment
	// SNI/certificate-SAN value of the kubectl API-passthrough route (the
	// shared gateway's TLS-passthrough listener, decision 6) — and, since the
	// path-based-app-ingress amendment, nothing else. It never needs to
	// resolve in DNS; browser-facing access is AppURL below.
	IngressHost string `json:"ingressHost,omitempty"`

	// AppURL is the environment's browser-facing entry point
	// ("{appExternalURL}/{name}"): one shared hostname for every
	// environment, path-scoped per environment by the shared gateway's L7
	// app listener (the ADR's path-based-app-ingress amendment).
	AppURL string `json:"appURL,omitempty"`

	// ExpiresAt is when the reconciler will delete the environment:
	// min(creationTimestamp + spec.ttl, creationTimestamp + TTLHardCap).
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// LastActivityAt is the most recent platform-visible activity signal
	// (currently: environment creation, each explicit wake, and each
	// kubeconfig CSR fulfillment) used to derive idleness for automatic sleep.
	LastActivityAt *metav1.Time `json:"lastActivityAt,omitempty"`

	// LastReconciledAt is the timestamp of the last successful reconciliation.
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`

	// KubeconfigCertificate is the PEM-encoded client certificate the
	// reconciler signed for the most recent spec.kubeconfigCSR. Public
	// material only — never the private key.
	KubeconfigCertificate string `json:"kubeconfigCertificate,omitempty"`

	// KubeconfigCABundle is the vcluster's client CA certificate, so a
	// consumer can assemble a complete kubeconfig without a second round trip.
	KubeconfigCABundle string `json:"kubeconfigCABundle,omitempty"`

	// KubeconfigCSRFingerprint is CSRFingerprint(spec.KubeconfigCSR) as of the
	// most recent signing. The reconciler re-signs whenever this no longer
	// matches the current spec.KubeconfigCSR, rather than treating any
	// non-empty KubeconfigCertificate as fulfilled forever — the identity
	// service's RBAC has no access to the status subresource, so it cannot
	// clear KubeconfigCertificate itself to request a re-sign.
	KubeconfigCSRFingerprint string `json:"kubeconfigCSRFingerprint,omitempty"`

	// Conditions lists detailed status conditions.
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DevEnvironment status condition types.
const (
	ConditionDevEnvNamespaceReady  = "NamespaceReady"
	ConditionDevEnvVClusterReady   = "VClusterReady"
	ConditionDevEnvKubeconfigCSR   = "KubeconfigCSRFulfilled"
	ConditionDevEnvPlatformApplied = "InVClusterPlatformApplied"
	// ConditionDevEnvPlatformReady distinguishes "the OrbitPlatform/OrbitTenant
	// CRs were submitted into the vcluster" (ConditionDevEnvPlatformApplied)
	// from "the nested orbit-operator has actually finished the real release
	// sequence" — status.phase must not report Ready until this is True.
	ConditionDevEnvPlatformReady = "InVClusterPlatformReady"
)

// DevEnvironment phase values.
const (
	DevEnvPhasePending      = "Pending"
	DevEnvPhaseProvisioning = "Provisioning"
	DevEnvPhaseReady        = "Ready"
	DevEnvPhaseSleeping     = "Sleeping"
	DevEnvPhaseExpired      = "Expired"
	DevEnvPhaseFailed       = "Failed"
)

// OrbitDevEnvironmentList is a list of OrbitDevEnvironment resources.
// +kubebuilder:object:root=true
type OrbitDevEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OrbitDevEnvironment `json:"items"`
}

// -----------------------------------------------------------------------------
// DeepCopy (hand-written, mirroring OrbitPlatform/OrbitTenant in internal/operator)
// -----------------------------------------------------------------------------

// DeepCopyObject implements runtime.Object.
func (e *OrbitDevEnvironment) DeepCopyObject() runtime.Object {
	if e == nil {
		return nil
	}
	out := new(OrbitDevEnvironment)
	e.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (e *OrbitDevEnvironment) DeepCopyInto(out *OrbitDevEnvironment) {
	*out = *e
	out.TypeMeta = e.TypeMeta
	e.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	e.Spec.DeepCopyInto(&out.Spec)
	e.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (e *OrbitDevEnvironment) DeepCopy() *OrbitDevEnvironment {
	if e == nil {
		return nil
	}
	out := new(OrbitDevEnvironment)
	e.DeepCopyInto(out)
	return out
}

// DeepCopyInto for OrbitDevEnvironmentSpec.
func (s *OrbitDevEnvironmentSpec) DeepCopyInto(out *OrbitDevEnvironmentSpec) {
	*out = *s
	out.TTL = s.TTL
	out.IdleTimeout = s.IdleTimeout
}

// DeepCopyInto for OrbitDevEnvironmentStatus.
func (s *OrbitDevEnvironmentStatus) DeepCopyInto(out *OrbitDevEnvironmentStatus) {
	*out = *s
	if s.ExpiresAt != nil {
		t := *s.ExpiresAt
		out.ExpiresAt = &t
	}
	if s.LastActivityAt != nil {
		t := *s.LastActivityAt
		out.LastActivityAt = &t
	}
	if s.LastReconciledAt != nil {
		t := *s.LastReconciledAt
		out.LastReconciledAt = &t
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}

// DeepCopyObject implements runtime.Object for OrbitDevEnvironmentList.
func (l *OrbitDevEnvironmentList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(OrbitDevEnvironmentList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *OrbitDevEnvironmentList) DeepCopyInto(out *OrbitDevEnvironmentList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]OrbitDevEnvironment, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
