// Package operator implements the Orbit Kubernetes operator. It watches
// OrbitPlatform and OrbitTenant custom resources and reconciles cluster state
// by driving the Orbit Helm charts via the Helm Go SDK.
//
// See docs/adr/0025-orbit-kubernetes-operator.md for the full design.
package operator

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version for Orbit CRDs.
var GroupVersion = schema.GroupVersion{Group: "orbit.io", Version: "v1alpha1"}

// -----------------------------------------------------------------------------
// OrbitPlatform — cluster-scoped, one per cluster
// -----------------------------------------------------------------------------

// OrbitPlatform declares the desired state of the entire Orbit platform.
// One instance per cluster, conventionally named "orbit".
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.version`
// +kubebuilder:printcolumn:name="Tenants",type=integer,JSONPath=`.status.tenantCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type OrbitPlatform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrbitPlatformSpec   `json:"spec,omitempty"`
	Status OrbitPlatformStatus `json:"status,omitempty"`
}

// OrbitPlatformSpec defines the desired state.
type OrbitPlatformSpec struct {
	// Version is the Orbit chart version to install. The operator sequences
	// upgrades across all releases when this is bumped.
	// +kubebuilder:validation:Pattern=`^[0-9]+\.[0-9]+\.[0-9]+$`
	Version string `json:"version"`

	// TrustDomain is the SPIFFE trust domain. Must be consistent across
	// SPIRE, the identity service, and all tenant workloads.
	// +kubebuilder:validation:MinLength=1
	TrustDomain string `json:"trustDomain"`

	// SPIRE controls whether the operator installs and manages SPIRE.
	// Set enabled:false if SPIRE is already installed in the cluster.
	SPIRE SPIRESpec `json:"spire,omitempty"`

	// Istio controls whether the operator installs and manages Istio.
	// Set enabled:false if Istio is already installed in the cluster.
	Istio IstioSpec `json:"istio,omitempty"`

	// Secrets references the pre-existing Secret containing platform credentials.
	// The Secret must contain: DB_PASSWORD, REDIS_PASSWORD, ORBIT_TOKEN_SIGNING_KEY.
	// The operator reads this Secret but never creates or rotates it.
	Secrets PlatformSecretsSpec `json:"secrets,omitempty"`

	// Config holds non-secret platform infrastructure configuration.
	Config PlatformConfigSpec `json:"config,omitempty"`

	// UpgradeStrategy controls how the operator handles chart version upgrades.
	UpgradeStrategy UpgradeStrategySpec `json:"upgradeStrategy,omitempty"`

	// ClusterName is the SPIRE cluster identifier. Defaults to "orbit".
	// Must be consistent across SPIRE server and agent configuration.
	// +kubebuilder:default=orbit
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// Gateway configures the Istio ingress gateway.
	Gateway GatewaySpec `json:"gateway,omitempty"`

	// ImageRegistry is an optional registry prefix applied to all Orbit platform
	// service images (orbit-identity, orbit-identity-proxy, etc.).
	// Use this for local development or air-gapped environments where images are
	// mirrored to a private registry.
	// Example: "orbit-registry:5001" -> orbit-registry:5001/orbit-identity:latest
	// Leave blank in production — the operator uses the built-in image references.
	// +optional
	ImageRegistry string `json:"imageRegistry,omitempty"`

	// PlatformValues are raw Helm values merged into the orbit-platform chart.
	// Use sparingly — prefer structured spec fields.
	// +kubebuilder:pruning:PreserveUnknownFields
	PlatformValues map[string]any `json:"platformValues,omitempty"`

	// OrbitValues are raw Helm values merged into the orbit chart.
	// Use sparingly — prefer structured spec fields.
	// +kubebuilder:pruning:PreserveUnknownFields
	OrbitValues map[string]any `json:"orbitValues,omitempty"`
}

// SPIRESpec controls SPIRE installation.
type SPIRESpec struct {
	// Enabled controls whether the operator installs SPIRE. Default: true.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// IstioSpec controls Istio installation.
type IstioSpec struct {
	// Enabled controls whether the operator installs Istio. Default: true.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// GatewaySpec configures the Istio ingress gateway.
type GatewaySpec struct {
	// ServiceType is the Kubernetes service type for the gateway.
	// Use "NodePort" on local clusters without a cloud LoadBalancer controller (e.g. k3d).
	// Defaults to "LoadBalancer".
	// +kubebuilder:default=LoadBalancer
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	ServiceType string `json:"serviceType,omitempty"`
}

// PlatformSecretsSpec references the Secret containing platform credentials.
type PlatformSecretsSpec struct {
	// PlatformSecretRef references the Secret with DB_PASSWORD, REDIS_PASSWORD,
	// and ORBIT_TOKEN_SIGNING_KEY. Defaults to namespace orbit-system.
	PlatformSecretRef SecretRef `json:"platformSecretRef"`
}

// SecretRef is a reference to a Kubernetes Secret.
type SecretRef struct {
	// Name is the Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace defaults to orbit-system if omitted.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// PlatformConfigSpec holds non-secret infrastructure configuration.
type PlatformConfigSpec struct {
	// DB holds PostgreSQL connection parameters (no credentials).
	DB DBConfig `json:"db,omitempty"`
	// Redis holds Redis connection parameters (no credentials).
	Redis RedisConfig `json:"redis,omitempty"`
}

// DBConfig holds non-secret PostgreSQL connection parameters.
type DBConfig struct {
	Host string `json:"host,omitempty"`
	Name string `json:"name,omitempty"`
	Port int    `json:"port,omitempty"`
	User string `json:"user,omitempty"`
}

// RedisConfig holds non-secret Redis connection parameters.
type RedisConfig struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

// UpgradeStrategySpec controls how chart version upgrades are performed.
type UpgradeStrategySpec struct {
	// Mode is Explicit (default) or Automatic.
	// Explicit: only upgrade when spec.version is changed.
	// Automatic: operator upgrades on new patch versions from the registry.
	// +kubebuilder:default=Explicit
	// +kubebuilder:validation:Enum=Explicit;Automatic
	Mode string `json:"mode,omitempty"`
}

// OrbitPlatformStatus records observed state.
type OrbitPlatformStatus struct {
	// Phase summarizes overall platform health.
	// +kubebuilder:validation:Enum=Pending;Installing;Upgrading;Ready;Degraded;Failed
	Phase string `json:"phase,omitempty"`

	// Version is the currently installed Orbit chart version.
	Version string `json:"version,omitempty"`

	// TenantCount is the number of active OrbitTenant CRs.
	TenantCount int `json:"tenantCount,omitempty"`

	// LastReconciledAt is the timestamp of the last successful reconciliation.
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`

	// Conditions lists detailed status conditions.
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Platform status condition types.
const (
	ConditionPlatformReady = "PlatformReady"
	ConditionSPIREReady    = "SPIREReady"
	ConditionIstioReady    = "IstioReady"
	ConditionIdentityReady = "IdentityServiceReady"
)

// Platform phase values.
const (
	PhasePending    = "Pending"
	PhaseInstalling = "Installing"
	PhaseUpgrading  = "Upgrading"
	PhaseReady      = "Ready"
	PhaseDegraded   = "Degraded"
	PhaseFailed     = "Failed"
)

// OrbitPlatformList is a list of OrbitPlatform resources.
// +kubebuilder:object:root=true
type OrbitPlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OrbitPlatform `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (p *OrbitPlatform) DeepCopyObject() runtime.Object {
	if p == nil {
		return nil
	}
	out := new(OrbitPlatform)
	p.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (p *OrbitPlatform) DeepCopyInto(out *OrbitPlatform) {
	*out = *p
	out.TypeMeta = p.TypeMeta
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.Spec.DeepCopyInto(&out.Spec)
	p.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (p *OrbitPlatform) DeepCopy() *OrbitPlatform {
	if p == nil {
		return nil
	}
	out := new(OrbitPlatform)
	p.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object for OrbitPlatformList.
func (l *OrbitPlatformList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(OrbitPlatformList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *OrbitPlatformList) DeepCopyInto(out *OrbitPlatformList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]OrbitPlatform, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyInto for OrbitPlatformSpec.
func (s *OrbitPlatformSpec) DeepCopyInto(out *OrbitPlatformSpec) {
	*out = *s
	out.SPIRE = s.SPIRE
	out.Istio = s.Istio
	out.Secrets = s.Secrets
	out.Config = s.Config
	out.UpgradeStrategy = s.UpgradeStrategy
	if s.PlatformValues != nil {
		out.PlatformValues = make(map[string]any, len(s.PlatformValues))
		maps.Copy(out.PlatformValues, s.PlatformValues)
	}
	if s.OrbitValues != nil {
		out.OrbitValues = make(map[string]any, len(s.OrbitValues))
		maps.Copy(out.OrbitValues, s.OrbitValues)
	}
}

// DeepCopyInto for OrbitPlatformStatus.
func (s *OrbitPlatformStatus) DeepCopyInto(out *OrbitPlatformStatus) {
	*out = *s
	if s.LastReconciledAt != nil {
		t := *s.LastReconciledAt
		out.LastReconciledAt = &t
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}

// -----------------------------------------------------------------------------
// OrbitTenant — cluster-scoped, one per tenant
// -----------------------------------------------------------------------------

// OrbitTenant declares a tenant's existence, OIDC configuration, and namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type OrbitTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrbitTenantSpec   `json:"spec,omitempty"`
	Status OrbitTenantStatus `json:"status,omitempty"`
}

// OrbitTenantSpec defines the desired state of a tenant.
type OrbitTenantSpec struct {
	// DisplayName is the human-readable tenant name surfaced in the identity
	// service tenant registry.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`

	// Active controls whether the tenant is active. When false, the operator
	// sets state:suspended in the registry without deleting cluster resources.
	// +kubebuilder:default=true
	Active bool `json:"active"`

	// Namespace is the Kubernetes namespace for this tenant's workloads.
	// If omitted, the operator derives it as "orbit-tenant-{tenantID}".
	// The operator creates the namespace with the correct labels:
	//   orbit.io/role: tenant
	//   istio-injection: enabled
	//   spiffe.io/spire-managed: "true"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// OIDC configures the identity provider for this tenant.
	OIDC TenantOIDCSpec `json:"oidc"`

	// DeleteNamespaceOnRemoval controls whether the operator deletes the
	// tenant namespace when the OrbitTenant CR is deleted.
	// Default: false (namespace is retained for safety).
	// +kubebuilder:default=false
	DeleteNamespaceOnRemoval bool `json:"deleteNamespaceOnRemoval,omitempty"`
}

// TenantOIDCSpec defines OIDC configuration for a tenant.
type TenantOIDCSpec struct {
	// IssuerURL is the OIDC issuer URL. The operator validates it at reconcile
	// time by fetching /.well-known/openid-configuration.
	// +kubebuilder:validation:Pattern=`^https?://`
	IssuerURL string `json:"issuerURL"`

	// ClientID is the OIDC client ID registered with the identity provider.
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientID"`

	// ClientSecretRef references a pre-existing Secret with key OIDC_CLIENT_SECRET.
	// Omit for public clients (device flow with no client secret).
	// The Secret can be created by ESO, Vault Agent, SealedSecret, or manually.
	// +optional
	ClientSecretRef *SecretRef `json:"clientSecretRef,omitempty"`

	// Scopes are the OAuth2 scopes to request. Defaults to [openid, profile, email].
	// +kubebuilder:default={openid,profile,email}
	Scopes []string `json:"scopes,omitempty"`
}

// OrbitTenantStatus records observed tenant state.
type OrbitTenantStatus struct {
	// Phase summarizes tenant lifecycle state.
	// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Degraded;Suspended
	Phase string `json:"phase,omitempty"`

	// Namespace is the provisioned tenant namespace name.
	Namespace string `json:"namespace,omitempty"`

	// LastReconciledAt is the timestamp of the last successful reconciliation.
	LastReconciledAt *metav1.Time `json:"lastReconciledAt,omitempty"`

	// Conditions lists detailed status conditions.
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Tenant status condition types.
const (
	ConditionNamespaceReady = "NamespaceReady"
	ConditionRegistryUpdated = "RegistryUpdated"
)

// Tenant phase values.
const (
	TenantPhasePending      = "Pending"
	TenantPhaseProvisioning = "Provisioning"
	TenantPhaseReady        = "Ready"
	TenantPhaseDegraded     = "Degraded"
	TenantPhaseSuspended    = "Suspended"
)

// OrbitTenantList is a list of OrbitTenant resources.
// +kubebuilder:object:root=true
type OrbitTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OrbitTenant `json:"items"`
}

// DeepCopyObject implements runtime.Object.
func (t *OrbitTenant) DeepCopyObject() runtime.Object {
	if t == nil {
		return nil
	}
	out := new(OrbitTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (t *OrbitTenant) DeepCopyInto(out *OrbitTenant) {
	*out = *t
	out.TypeMeta = t.TypeMeta
	t.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	t.Spec.DeepCopyInto(&out.Spec)
	t.Status.DeepCopyInto(&out.Status)
}

// DeepCopy returns a deep copy.
func (t *OrbitTenant) DeepCopy() *OrbitTenant {
	if t == nil {
		return nil
	}
	out := new(OrbitTenant)
	t.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object for OrbitTenantList.
func (l *OrbitTenantList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(OrbitTenantList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *OrbitTenantList) DeepCopyInto(out *OrbitTenantList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]OrbitTenant, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopyInto for OrbitTenantSpec.
func (s *OrbitTenantSpec) DeepCopyInto(out *OrbitTenantSpec) {
	*out = *s
	out.OIDC = s.OIDC
	if s.OIDC.ClientSecretRef != nil {
		ref := *s.OIDC.ClientSecretRef
		out.OIDC.ClientSecretRef = &ref
	}
	if s.OIDC.Scopes != nil {
		out.OIDC.Scopes = make([]string, len(s.OIDC.Scopes))
		copy(out.OIDC.Scopes, s.OIDC.Scopes)
	}
}

// DeepCopyInto for OrbitTenantStatus.
func (s *OrbitTenantStatus) DeepCopyInto(out *OrbitTenantStatus) {
	*out = *s
	if s.LastReconciledAt != nil {
		t := *s.LastReconciledAt
		out.LastReconciledAt = &t
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}
