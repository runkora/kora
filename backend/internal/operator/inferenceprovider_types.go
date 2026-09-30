// InferenceProvider CRD Go types.
//
// An InferenceProvider registers one model provider with the inference
// gateway. It lives in the system namespace and names its credentials by
// Secret reference, never by value. The InferenceProviderAggregator renders
// every resource into the ConfigMap and Secret the gateway mounts.
//
// See docs/adr/2026-10-01-crd-inference-providers.md.
package operator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// InferenceProvider phases.
const (
	InferenceProviderPhaseReady   = "Ready"
	InferenceProviderPhaseInvalid = "Invalid"
)

// InferenceProvider registers one model provider with the inference gateway.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=oip
type InferenceProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InferenceProviderSpec   `json:"spec,omitempty"`
	Status InferenceProviderStatus `json:"status,omitempty"`
}

// InferenceProviderSpec is a union: Type names the one block that is set.
type InferenceProviderSpec struct {
	Type        string   `json:"type"`
	DisplayName string   `json:"displayName,omitempty"`
	Models      []string `json:"models,omitempty"`

	Bedrock   *BedrockProviderSpec   `json:"bedrock,omitempty"`
	VertexAI  *VertexAIProviderSpec  `json:"vertexai,omitempty"`
	Azure     *AzureProviderSpec     `json:"azure,omitempty"`
	OpenAI    *OpenAIProviderSpec    `json:"openai,omitempty"`
	Anthropic *AnthropicProviderSpec `json:"anthropic,omitempty"`
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

// BedrockProviderSpec configures AWS Bedrock. Without Credentials the gateway's
// workload identity applies.
type BedrockProviderSpec struct {
	Region        string              `json:"region"`
	Credentials   *BedrockCredentials `json:"credentials,omitempty"`
	AssumeRoleARN string              `json:"assumeRoleARN,omitempty"`
	ExternalID    string              `json:"externalID,omitempty"`
}

// BedrockCredentials names a Secret with keys accessKeyID, secretAccessKey and
// optionally sessionToken.
type BedrockCredentials struct {
	SecretRef SecretNameRef `json:"secretRef"`
}

// VertexAIProviderSpec configures Google Vertex AI. Without
// CredentialsSecretRef, Application Default Credentials apply.
type VertexAIProviderSpec struct {
	ProjectID            string        `json:"projectID"`
	Location             string        `json:"location"`
	AnthropicLocation    string        `json:"anthropicLocation,omitempty"`
	CredentialsSecretRef *SecretKeyRef `json:"credentialsSecretRef,omitempty"`
}

// AzureProviderSpec configures Azure AI.
type AzureProviderSpec struct {
	Endpoint          string       `json:"endpoint"`
	APIVersion        string       `json:"apiVersion"`
	AnthropicEndpoint string       `json:"anthropicEndpoint,omitempty"`
	APIKeySecretRef   SecretKeyRef `json:"apiKeySecretRef"`
}

// OpenAIProviderSpec configures any OpenAI-compatible endpoint.
type OpenAIProviderSpec struct {
	BaseURL         string        `json:"baseURL,omitempty"`
	APIKeySecretRef *SecretKeyRef `json:"apiKeySecretRef,omitempty"`
}

// AnthropicProviderSpec configures any Anthropic Messages endpoint.
type AnthropicProviderSpec struct {
	BaseURL         string       `json:"baseURL,omitempty"`
	APIKeySecretRef SecretKeyRef `json:"apiKeySecretRef"`
}

// InferenceProviderStatus is reconciliation state only. Runtime health belongs
// to the gateway.
type InferenceProviderStatus struct {
	Phase              string             `json:"phase,omitempty"`
	ProviderName       string             `json:"providerName,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastReconciledAt   *metav1.Time       `json:"lastReconciledAt,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// InferenceProviderList is a list of InferenceProvider resources.
//
// +kubebuilder:object:root=true
type InferenceProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceProvider `json:"items"`
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
func (s *InferenceProviderSpec) DeepCopyInto(out *InferenceProviderSpec) {
	*out = *s
	if s.Models != nil {
		out.Models = append([]string(nil), s.Models...)
	}
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
	if s.OpenAI != nil {
		o := *s.OpenAI
		if s.OpenAI.APIKeySecretRef != nil {
			r := *s.OpenAI.APIKeySecretRef
			o.APIKeySecretRef = &r
		}
		out.OpenAI = &o
	}
	if s.Anthropic != nil {
		a := *s.Anthropic
		out.Anthropic = &a
	}
}

// DeepCopyInto copies the status.
func (s *InferenceProviderStatus) DeepCopyInto(out *InferenceProviderStatus) {
	*out = *s
	if s.LastReconciledAt != nil {
		out.LastReconciledAt = s.LastReconciledAt.DeepCopy()
	}
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
	l.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into out.
func (l *InferenceProviderList) DeepCopyInto(out *InferenceProviderList) {
	*out = *l
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]InferenceProvider, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
