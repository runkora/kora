// OAuthClient CRD Go types for the identity service's OAuth authorization
// server (GitOps hosted-app registration).
//
// An OAuthClient is a tenant-namespaced custom resource that pre-registers one
// public OAuth client (a hosted web application, e.g. a CRM that logs users in
// through kora) with the identity service. It carries the client_id, an
// optional display name, and the exact set of permitted redirect URIs. Unlike
// dynamically-registered clients (RFC 7591 DCR, which are restricted to
// loopback redirect URIs per RFC 8252), an OAuthClient is admin-reviewed and
// may declare non-loopback HTTPS redirect URIs.
//
// All OAuth clients are public — PKCE is the proof of possession — so, unlike
// Connection, an OAuthClient carries NO secret reference.
//
// The operator's OAuthClientAggregator reads these CRs and renders the
// kora-system ConfigMap the identity service mounts (oauth-clients.json),
// mirroring the ConnectionAggregator. The identity service, a feature package,
// never imports this CRD type; it reads the mounted ConfigMap instead.
//
// See docs/adr/2026-07-28-oauth2-web-redirect-clients.md.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// OAuthClient — namespaced, one per pre-registered hosted-app client per tenant
// -----------------------------------------------------------------------------

// OAuthClient pre-registers a public OAuth client (a hosted web app) with the
// identity authorization server. It lives in the tenant namespace.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=oauthc
// +kubebuilder:printcolumn:name="ClientID",type=string,JSONPath=`.spec.clientId`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.clientName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:object:generate=true
type OAuthClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec OAuthClientSpec `json:"spec,omitempty"`
}

// OAuthClientSpec declares the public-client identity the authorization server
// enforces at the authorize endpoint. There is no secret and no grant/scope
// vocabulary here: authorization of the resulting human token is owned by the
// policy service, exactly as with Connection under Amendment 3.
//
// +kubebuilder:object:generate=true
type OAuthClientSpec struct {
	// ClientID is the stable, human-readable client identifier the app presents
	// at the authorize and token endpoints (e.g. "example-app"). It MUST be
	// unique across the whole authorization server — the identity service looks
	// clients up by client_id alone, not scoped by tenant.
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientId"`

	// ClientName is the display name shown on the consent screen. Optional.
	// +optional
	ClientName string `json:"clientName,omitempty"`

	// RedirectURIs is the exact list of permitted redirect URIs for this client.
	// The authorize endpoint requires an exact string match against this list
	// (no prefix or wildcard matching), which is the security gate that makes
	// non-loopback redirect URIs safe. At least one is required.
	// +kubebuilder:validation:MinItems=1
	RedirectURIs []string `json:"redirectUris"`
}

// OAuthClientList is a list of OAuthClient resources.
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
type OAuthClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OAuthClient `json:"items"`
}
