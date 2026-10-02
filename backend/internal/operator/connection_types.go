// Connection CRD Go types for the credential broker (Amendment 3 — GitOps).
//
// A Connection is a tenant-namespaced custom resource that declares the
// credential-source identity of one external service the egress gateway
// brokers (GitHub App, an OAuth2 app, an API-key SaaS). It carries the
// adapter type, the upstream base host, a reference to a Kubernetes Secret
// (a name, never a value), and adapter-specific config. The referenced
// Secret's material is materialized by the deployer's secret pipeline; the
// raw value never enters kora's application layer.
//
// The operator's ConnectionAggregator reads these CRs (and their referenced
// Secrets) and renders the orbit-system config the egress gateway mounts —
// mirroring the per-tenant CA aggregator. The gateway, a feature package,
// never imports this CRD type; it reads the mounted ConfigMap/Secret instead.
//
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md,
// "Amendment 3 — 2026-06-12".
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// -----------------------------------------------------------------------------
// Connection — namespaced, one per external service per tenant
// -----------------------------------------------------------------------------

// Connection declares the credential-source identity of one external service
// the egress gateway brokers for a tenant. It lives in the tenant namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=oconn
// +kubebuilder:printcolumn:name="Adapter",type=string,JSONPath=`.spec.adapter`
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.baseHost`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Connection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ConnectionSpec `json:"spec,omitempty"`
}

// ConnectionSpec carries credential-source identity only. Per Amendment 3 the
// grantable action vocabulary, the agent grant/PDP authorization, and the
// repos/permissions the token can reach are owned by other authorities and are
// deliberately not restated here.
type ConnectionSpec struct {
	// Adapter selects the credential mechanism (e.g. "github_app",
	// "kubernetes"). Validated at resolve time by the gateway's adapter
	// registry rather than by the CRD schema, so adding an adapter does not
	// require a CRD bump.
	// +kubebuilder:validation:MinLength=1
	Adapter string `json:"adapter"`

	// BaseHost is the upstream the gateway terminates and re-originates, e.g.
	// "github.com". Optional for adapters that do not key on a single host.
	// +optional
	BaseHost string `json:"baseHost,omitempty"`

	// SecretRef is the name of a Kubernetes Secret in this namespace holding
	// the connection's secret material (e.g. the GitHub App private key). A
	// name, never a value — the material is materialized by the deployer's
	// secret pipeline.
	// +kubebuilder:validation:MinLength=1
	SecretRef string `json:"secretRef"`

	// SecretKey is which data key inside the referenced Secret holds the
	// material. Optional — when empty and the Secret has exactly one key, that
	// key is used.
	// +optional
	SecretKey string `json:"secretKey,omitempty"`

	// CACert is the PEM certificate authority the gateway verifies this
	// upstream against, for an upstream not signed by a public root. It
	// belongs to the connection rather than to any adapter: the gateway owns
	// the outbound TLS leg, and no adapter dials the upstream.
	// +optional
	CACert string `json:"caCert,omitempty"`

	// Config is the connection's adapter-specific configuration, passed
	// through to the gateway verbatim. For github_app the adapter parses
	// app_id (string) and installation_id (number); snake_case is required.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config runtime.RawExtension `json:"config,omitempty"`
}

// ConnectionList is a list of Connection resources.
// +kubebuilder:object:root=true
type ConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Connection `json:"items"`
}

// -----------------------------------------------------------------------------
// DeepCopy (hand-written, mirroring OrbitTenant)
// -----------------------------------------------------------------------------

// DeepCopyObject implements runtime.Object.
func (c *Connection) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	out := new(Connection)
	c.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (c *Connection) DeepCopyInto(out *Connection) {
	*out = *c
	out.TypeMeta = c.TypeMeta
	c.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	c.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy returns a deep copy.
func (c *Connection) DeepCopy() *Connection {
	if c == nil {
		return nil
	}
	out := new(Connection)
	c.DeepCopyInto(out)
	return out
}

// DeepCopyInto for ConnectionSpec. runtime.RawExtension carries its own
// DeepCopyInto.
func (s *ConnectionSpec) DeepCopyInto(out *ConnectionSpec) {
	*out = *s
	s.Config.DeepCopyInto(&out.Config)
}

// DeepCopyObject implements runtime.Object for ConnectionList.
func (l *ConnectionList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(ConnectionList)
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *ConnectionList) DeepCopyInto(out *ConnectionList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]Connection, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
