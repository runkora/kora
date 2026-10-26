// +kubebuilder:object:generate=true
// +groupName=runkora.io

// Package v1alpha1 holds the InferenceProvider API types. The inference
// gateway builds connectors from them and the operator registers their hosts
// with the mesh; the chart ships their CRD.
//
// See docs/adr/2026-10-01-crd-inference-providers.md.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the API group and version of InferenceProvider.
var GroupVersion = schema.GroupVersion{Group: "runkora.io", Version: "v1alpha1"}

// SecretLabel marks the Secrets a provider may reference. The gateway only
// caches Secrets carrying it.
const SecretLabel = "runkora.io/inference-provider"

// Type is a provider type. It is the prefix of every provider name.
// +kubebuilder:validation:Enum=bedrock;vertexai;azure;compatible
type Type string

// Provider types.
const (
	TypeBedrock    Type = "bedrock"
	TypeVertexAI   Type = "vertexai"
	TypeAzure      Type = "azure"
	TypeCompatible Type = "compatible"
)

// API is an inference API a compatible endpoint serves.
// +kubebuilder:validation:Enum=chatCompletions;embeddings;responses;messages;systemone
type API string

// APIs a compatible endpoint can declare.
const (
	APIChatCompletions API = "chatCompletions"
	APIEmbeddings      API = "embeddings"
	APIResponses       API = "responses"
	APIMessages        API = "messages"
	// APISystemOne is TypeSafe's System One decision API, served at
	// <baseURL>/systemone.
	APISystemOne API = "systemone"
)

// Phases on InferenceProvider status.
const (
	PhaseReady   = "Ready"
	PhaseInvalid = "Invalid"
)

// InferenceProvider registers one model provider with the inference gateway,
// which watches it. Apply it in the kora system namespace. The provider
// registers as <type>:<metadata.name>. Credentials are Secrets in the same
// namespace, labeled runkora.io/inference-provider=true, and named here, never
// inlined.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=kip
// +kubebuilder:printcolumn:name=Type,type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name=Provider,type=string,JSONPath=`.status.providerName`
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 63",message="metadata.name must be a DNS label; it becomes the provider name suffix."
type InferenceProvider struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	Spec Spec `json:"spec"`
	// +optional
	Status Status `json:"status"`
}

// ProviderName is the gateway provider name this resource registers.
func (p *InferenceProvider) ProviderName() string {
	return string(p.Spec.Type) + ":" + p.Name
}

// Spec is a union: Type names the one block that is set.
//
// +kubebuilder:validation:XValidation:rule="(self.type == 'bedrock') == has(self.bedrock)",message="set the bedrock block if and only if type is bedrock."
// +kubebuilder:validation:XValidation:rule="(self.type == 'vertexai') == has(self.vertexai)",message="set the vertexai block if and only if type is vertexai."
// +kubebuilder:validation:XValidation:rule="(self.type == 'azure') == has(self.azure)",message="set the azure block if and only if type is azure."
// +kubebuilder:validation:XValidation:rule="(self.type == 'compatible') == has(self.compatible)",message="set the compatible block if and only if type is compatible."
type Spec struct {
	// Type is the provider type. It selects the one config block that must be set.
	Type Type `json:"type"`
	// DisplayName is the human-readable name shown in the dashboard.
	// +kubebuilder:validation:MaxLength=128
	DisplayName string `json:"displayName,omitempty"`
	// Models are the upstream model ids to offer. Empty offers every discovered
	// model. When the upstream has no model list, this is the list.
	// +kubebuilder:validation:items:MinLength=1
	Models []string `json:"models,omitempty"`

	// Bedrock is AWS Bedrock. Without credentials the gateway's workload
	// identity applies (IRSA, EKS Pod Identity).
	Bedrock *Bedrock `json:"bedrock,omitempty"`
	// VertexAI is Google Vertex AI. Without credentialsSecretRef, Application
	// Default Credentials apply (GKE Workload Identity).
	VertexAI *VertexAI `json:"vertexai,omitempty"`
	// Azure is Azure AI.
	Azure *Azure `json:"azure,omitempty"`
	// Compatible is an endpoint that speaks the OpenAI or Anthropic APIs, such
	// as OpenAI, Anthropic, OpenRouter or vLLM. apis lists what it serves; those
	// pass through natively.
	Compatible *Compatible `json:"compatible,omitempty"`
}

// SecretKeyRef names one key of a Secret in the resource's namespace.
type SecretKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// SecretNameRef names a Secret in the resource's namespace.
type SecretNameRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Bedrock configures AWS Bedrock. Without Credentials the gateway's workload
// identity applies.
type Bedrock struct {
	// +kubebuilder:validation:MinLength=1
	Region      string              `json:"region"`
	Credentials *BedrockCredentials `json:"credentials,omitempty"`
	// AssumeRoleARN is a role the gateway assumes on top of its base credentials.
	// +kubebuilder:validation:Pattern=`^arn:aws[a-z-]*:iam::[0-9]{12}:role/.+$`
	AssumeRoleARN string `json:"assumeRoleARN,omitempty"`
	ExternalID    string `json:"externalID,omitempty"`
	// RoleSessionName names the assumed sessions in CloudTrail. Defaults to
	// kora-inference-gateway.
	// +kubebuilder:validation:Pattern=`^[\w+=,.@-]{2,64}$`
	RoleSessionName string `json:"roleSessionName,omitempty"`
}

// BedrockCredentials names a Secret with keys accessKeyID, secretAccessKey
// and optionally sessionToken.
type BedrockCredentials struct {
	SecretRef SecretNameRef `json:"secretRef"`
}

// VertexAI configures Google Vertex AI. Without CredentialsSecretRef,
// Application Default Credentials apply.
type VertexAI struct {
	// +kubebuilder:validation:MinLength=1
	ProjectID string `json:"projectID"`
	// +kubebuilder:validation:MinLength=1
	Location string `json:"location"`
	// AnthropicLocation is the region for Claude models when it differs from location.
	AnthropicLocation string `json:"anthropicLocation,omitempty"`
	// CredentialsSecretRef is a Secret key holding a service account JSON key.
	CredentialsSecretRef *SecretKeyRef `json:"credentialsSecretRef,omitempty"`
}

// Azure configures Azure AI.
type Azure struct {
	// +kubebuilder:validation:Pattern=`^https?://.+`
	Endpoint string `json:"endpoint"`
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`
	// +kubebuilder:validation:Pattern=`^https?://.+`
	AnthropicEndpoint string       `json:"anthropicEndpoint,omitempty"`
	APIKeySecretRef   SecretKeyRef `json:"apiKeySecretRef"`
}

// Compatible configures an endpoint that speaks the OpenAI or Anthropic APIs,
// such as OpenAI, Anthropic, OpenRouter or vLLM. APIs lists what it serves.
//
// +kubebuilder:validation:XValidation:rule="!('messages' in self.apis) || self.baseURL.endsWith('/v1') || self.baseURL.endsWith('/v1/')",message="an endpoint serving messages needs a baseURL ending in /v1."
// +kubebuilder:validation:XValidation:rule="!has(self.decisionModels) || 'systemone' in self.apis",message="decisionModels needs systemone in apis."
type Compatible struct {
	// BaseURL is the prefix of /chat/completions and /messages, e.g.
	// https://openrouter.ai/api/v1. http and https are allowed.
	// +kubebuilder:validation:Pattern=`^https?://.+`
	BaseURL string `json:"baseURL"`
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	APIs []API `json:"apis"`
	// APIKeySecretRef is a key of a Secret labeled runkora.io/inference-provider=true.
	APIKeySecretRef *SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// DecisionModels names models served on POST /systemone when the endpoint
	// publishes no metadata that says so. Requires systemone in APIs.
	// +kubebuilder:validation:items:MinLength=1
	DecisionModels []string `json:"decisionModels,omitempty"`
}

// Status is written by the inference gateway.
type Status struct {
	// +kubebuilder:validation:Enum=Ready;Invalid
	Phase              string             `json:"phase,omitempty"`
	ProviderName       string             `json:"providerName,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// InferenceProviderList is a list of InferenceProvider resources.
//
// +kubebuilder:object:root=true
type InferenceProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []InferenceProvider `json:"items"`
}

// AddToScheme registers the types.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &InferenceProvider{}, &InferenceProviderList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
