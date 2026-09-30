// Package inferenceproviders is the file contract between the operator, which
// renders InferenceProvider resources, and the inference gateway, which loads
// them. It lives under platform because the two binaries must agree on it
// without importing each other.
//
// See docs/adr/2026-10-01-crd-inference-providers.md.
package inferenceproviders

import (
	"fmt"
	"regexp"
)

const (
	// ConfigMapName holds providers.json in the system namespace.
	ConfigMapName = "orbit-inference-providers"
	// ConfigMapKey is the providers.json data key.
	ConfigMapKey = "providers.json"
	// SecretName holds the credential values the entries reference.
	SecretName = "orbit-inference-provider-secrets"
)

// Type is a provider type. It is the prefix of every provider name.
type Type string

// Provider types a resource can declare.
const (
	TypeBedrock   Type = "bedrock"
	TypeVertexAI  Type = "vertexai"
	TypeAzure     Type = "azure"
	TypeOpenAI    Type = "openai"
	TypeAnthropic Type = "anthropic"
)

// Types lists every provider type in a stable order.
var Types = []Type{TypeBedrock, TypeVertexAI, TypeAzure, TypeOpenAI, TypeAnthropic}

// Valid reports whether t is a known provider type.
func (t Type) Valid() bool {
	for _, known := range Types {
		if t == known {
			return true
		}
	}
	return false
}

// Default upstream addresses for the generic types.
const (
	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
)

// resourceNameRe is the DNS label rule the CRD enforces on metadata.name.
var resourceNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidResourceName reports whether name can become a provider name suffix.
func ValidResourceName(name string) bool {
	return len(name) <= 63 && resourceNameRe.MatchString(name)
}

// ProviderName is the gateway provider name a resource registers.
func ProviderName(t Type, resource string) string {
	return string(t) + ":" + resource
}

// SecretKey is the aggregate Secret key holding one credential field of one
// resource. Resource names contain no dots; the key is unambiguous.
func SecretKey(resource, field string) string {
	return resource + "." + field
}

// Credential field names used in SecretKey.
const (
	FieldAPIKey          = "apiKey"
	FieldAccessKeyID     = "accessKeyID"
	FieldSecretAccessKey = "secretAccessKey"
	FieldSessionToken    = "sessionToken"
	FieldCredentialsJSON = "credentials"
)

// Entry is one provider in providers.json. Credential fields name keys in the
// aggregate Secret; they never carry values.
type Entry struct {
	Name        string     `json:"name"`
	Resource    string     `json:"resource"`
	Type        Type       `json:"type"`
	DisplayName string     `json:"display_name,omitempty"`
	Models      []string   `json:"models,omitempty"`
	Bedrock     *Bedrock   `json:"bedrock,omitempty"`
	VertexAI    *VertexAI  `json:"vertexai,omitempty"`
	Azure       *Azure     `json:"azure,omitempty"`
	OpenAI      *OpenAI    `json:"openai,omitempty"`
	Anthropic   *Anthropic `json:"anthropic,omitempty"`
}

// Bedrock configures an AWS Bedrock provider. Empty key fields mean the
// gateway's workload identity.
type Bedrock struct {
	Region             string `json:"region"`
	AssumeRoleARN      string `json:"assume_role_arn,omitempty"`
	ExternalID         string `json:"external_id,omitempty"`
	AccessKeyIDKey     string `json:"access_key_id_key,omitempty"`
	SecretAccessKeyKey string `json:"secret_access_key_key,omitempty"`
	SessionTokenKey    string `json:"session_token_key,omitempty"`
}

// VertexAI configures a Google Vertex AI provider. An empty CredentialsKey
// means Application Default Credentials.
type VertexAI struct {
	ProjectID         string `json:"project_id"`
	Location          string `json:"location"`
	AnthropicLocation string `json:"anthropic_location,omitempty"`
	CredentialsKey    string `json:"credentials_key,omitempty"`
}

// Azure configures an Azure AI provider.
type Azure struct {
	Endpoint          string `json:"endpoint"`
	APIVersion        string `json:"api_version"`
	AnthropicEndpoint string `json:"anthropic_endpoint,omitempty"`
	APIKeyKey         string `json:"api_key_key"`
}

// OpenAI configures an OpenAI-compatible provider. An empty APIKeyKey sends no
// key, for an unauthenticated in-cluster server.
type OpenAI struct {
	BaseURL   string `json:"base_url"`
	APIKeyKey string `json:"api_key_key,omitempty"`
	CACert    string `json:"ca_cert,omitempty"`
}

// Anthropic configures an Anthropic Messages provider.
type Anthropic struct {
	BaseURL   string `json:"base_url"`
	APIKeyKey string `json:"api_key_key"`
	CACert    string `json:"ca_cert,omitempty"`
}

// Validate checks that the entry names a known type and carries exactly the
// block for that type.
func (e Entry) Validate() error {
	if !e.Type.Valid() {
		return fmt.Errorf("unknown provider type %q", e.Type)
	}
	blocks := map[Type]bool{
		TypeBedrock:   e.Bedrock != nil,
		TypeVertexAI:  e.VertexAI != nil,
		TypeAzure:     e.Azure != nil,
		TypeOpenAI:    e.OpenAI != nil,
		TypeAnthropic: e.Anthropic != nil,
	}
	for t, set := range blocks {
		if set != (t == e.Type) {
			return fmt.Errorf("provider %q of type %q must set only the %q block", e.Name, e.Type, e.Type)
		}
	}
	return nil
}
