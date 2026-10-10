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
var GroupVersion = schema.GroupVersion{Group: "orbit.io", Version: "v1alpha1"}

// SecretLabel marks the Secrets a provider may reference. The gateway only
// caches Secrets carrying it.
const SecretLabel = "orbit.io/inference-provider"

// Type is a provider type. It is the prefix of every provider name.
type Type string

// Provider types.
const (
	TypeBedrock    Type = "bedrock"
	TypeVertexAI   Type = "vertexai"
	TypeAzure      Type = "azure"
	TypeCompatible Type = "compatible"
)

// API is an inference API a compatible endpoint serves.
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

// InferenceProvider registers one model provider with the inference gateway.
type InferenceProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   Spec   `json:"spec"`
	Status Status `json:"status"`
}

// ProviderName is the gateway provider name this resource registers.
func (p *InferenceProvider) ProviderName() string {
	return string(p.Spec.Type) + ":" + p.Name
}

// Spec is a union: Type names the one block that is set.
type Spec struct {
	Type        Type     `json:"type"`
	DisplayName string   `json:"displayName,omitempty"`
	Models      []string `json:"models,omitempty"`

	Bedrock    *Bedrock    `json:"bedrock,omitempty"`
	VertexAI   *VertexAI   `json:"vertexai,omitempty"`
	Azure      *Azure      `json:"azure,omitempty"`
	Compatible *Compatible `json:"compatible,omitempty"`
}

// SecretKeyRef names one key of a Secret in the resource's namespace.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// SecretNameRef names a Secret in the resource's namespace.
type SecretNameRef struct {
	Name string `json:"name"`
}

// Bedrock configures AWS Bedrock. Without Credentials the gateway's workload
// identity applies.
type Bedrock struct {
	Region          string              `json:"region"`
	Credentials     *BedrockCredentials `json:"credentials,omitempty"`
	AssumeRoleARN   string              `json:"assumeRoleARN,omitempty"`
	ExternalID      string              `json:"externalID,omitempty"`
	RoleSessionName string              `json:"roleSessionName,omitempty"`
}

// BedrockCredentials names a Secret with keys accessKeyID, secretAccessKey
// and optionally sessionToken.
type BedrockCredentials struct {
	SecretRef SecretNameRef `json:"secretRef"`
}

// VertexAI configures Google Vertex AI. Without CredentialsSecretRef,
// Application Default Credentials apply.
type VertexAI struct {
	ProjectID            string        `json:"projectID"`
	Location             string        `json:"location"`
	AnthropicLocation    string        `json:"anthropicLocation,omitempty"`
	CredentialsSecretRef *SecretKeyRef `json:"credentialsSecretRef,omitempty"`
}

// Azure configures Azure AI.
type Azure struct {
	Endpoint          string       `json:"endpoint"`
	APIVersion        string       `json:"apiVersion"`
	AnthropicEndpoint string       `json:"anthropicEndpoint,omitempty"`
	APIKeySecretRef   SecretKeyRef `json:"apiKeySecretRef"`
}

// Compatible configures an endpoint that speaks the OpenAI or Anthropic APIs,
// such as OpenAI, Anthropic, OpenRouter or vLLM. APIs lists what it serves.
type Compatible struct {
	BaseURL         string        `json:"baseURL"`
	APIs            []API         `json:"apis"`
	APIKeySecretRef *SecretKeyRef `json:"apiKeySecretRef,omitempty"`
}

// Status is written by the inference gateway.
type Status struct {
	Phase              string             `json:"phase,omitempty"`
	ProviderName       string             `json:"providerName,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// InferenceProviderList is a list of InferenceProvider resources.
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

// DeepCopyObject implements runtime.Object.
func (p *InferenceProvider) DeepCopyObject() runtime.Object {
	if p == nil {
		return nil
	}
	return p.DeepCopy()
}

// DeepCopy returns a deep copy.
func (p *InferenceProvider) DeepCopy() *InferenceProvider {
	if p == nil {
		return nil
	}
	out := new(InferenceProvider)
	p.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (p *InferenceProvider) DeepCopyInto(out *InferenceProvider) {
	*out = *p
	p.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	p.Spec.DeepCopyInto(&out.Spec)
	p.Status.DeepCopyInto(&out.Status)
}

// DeepCopyInto copies the spec, including every pointer block.
func (s *Spec) DeepCopyInto(out *Spec) {
	*out = *s
	out.Models = append([]string(nil), s.Models...)
	if s.Bedrock != nil {
		b := *s.Bedrock
		if s.Bedrock.Credentials != nil {
			c := *s.Bedrock.Credentials
			b.Credentials = &c
		}
		out.Bedrock = &b
	}
	if s.VertexAI != nil {
		v := *s.VertexAI
		if s.VertexAI.CredentialsSecretRef != nil {
			r := *s.VertexAI.CredentialsSecretRef
			v.CredentialsSecretRef = &r
		}
		out.VertexAI = &v
	}
	if s.Azure != nil {
		a := *s.Azure
		out.Azure = &a
	}
	if s.Compatible != nil {
		c := *s.Compatible
		c.APIs = append([]API(nil), s.Compatible.APIs...)
		if s.Compatible.APIKeySecretRef != nil {
			r := *s.Compatible.APIKeySecretRef
			c.APIKeySecretRef = &r
		}
		out.Compatible = &c
	}
}

// DeepCopyInto copies the status.
func (s *Status) DeepCopyInto(out *Status) {
	*out = *s
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		for i := range s.Conditions {
			s.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}

// DeepCopyObject implements runtime.Object for InferenceProviderList.
func (l *InferenceProviderList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(InferenceProviderList)
	*out = *l
	l.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]InferenceProvider, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
	return out
}
