package config

import (
	"os"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/observability"
)

// Config is the root configuration for the inference-gateway service.
// YAML is loaded from cmd/inference-gateway/config/{APP_ENV}.yaml.
// All secrets are read from environment variables (never from YAML).
type Config struct {
	App           config.AppConfig    `yaml:"app"`
	Server        config.ServerConfig `yaml:"server"`
	Observability observability.Config `yaml:"observability"`

	// Token holds settings for parsing inbound platform JWTs.
	Token TokenConfig `yaml:"token"`

	// Providers holds provider-specific configuration (credentials from env).
	Providers ProvidersConfig `yaml:"providers"`

	// Pricing holds cost data configuration.
	Pricing PricingConfig `yaml:"pricing"`
}

// TokenConfig configures inbound JWT verification.
//
// The inference gateway verifies platform tokens using the identity service's
// public JWKS endpoint — no private key is distributed to this service.
// Keys are fetched lazily and cached; go-oidc/v3 handles automatic rotation.
type TokenConfig struct {
	// JWKSURL is the full URL of the identity service JWKS endpoint.
	// In-cluster default:
	//   http://orbit-identity.orbit-system.svc.cluster.local/api/v1/identity/.well-known/jwks.json
	// Override via ORBIT_TOKEN_JWKS_URL for local development.
	JWKSURL string `yaml:"jwksUrl"`

	// Issuer is the expected "iss" claim value in platform tokens.
	// Defaults to "orbit" (the value set by the identity service).
	Issuer string `yaml:"issuer"`

	// Audience is the expected "aud" claim value.
	// Defaults to "orbit-platform".
	Audience string `yaml:"audience"`
}

// ProvidersConfig holds per-provider enablement flags.
// Credentials are always read from environment variables (yaml:"-").
type ProvidersConfig struct {
	Bedrock  BedrockConfig  `yaml:"bedrock"`
	VertexAI VertexAIConfig `yaml:"vertexAI"`
	Azure    AzureConfig    `yaml:"azure"`
}

// BedrockConfig configures the AWS Bedrock provider.
type BedrockConfig struct {
	Enabled bool   `yaml:"enabled"`
	Region  string `yaml:"region"`

	// Credentials loaded from environment — never stored in YAML.
	AccessKeyID     string `yaml:"-"`
	SecretAccessKey string `yaml:"-"`
	SessionToken    string `yaml:"-"` // optional, for temporary credentials
}

// VertexAIConfig configures the Google Vertex AI provider.
type VertexAIConfig struct {
	Enabled   bool   `yaml:"enabled"`
	ProjectID string `yaml:"projectId"`
	Location  string `yaml:"location"` // e.g. "us-central1"

	// CredentialsJSON is the GCP service account JSON content.
	// Loaded from VERTEX_CREDENTIALS_JSON env var.
	CredentialsJSON string `yaml:"-"`
}

// AzureConfig configures the Azure AI Inference provider.
type AzureConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Endpoint string `yaml:"endpoint"` // e.g. "https://my-hub.openai.azure.com"

	// APIKey loaded from AZURE_AI_API_KEY env var.
	APIKey string `yaml:"-"`
}

// PricingConfig configures the pricing cache.
type PricingConfig struct {
	// RefreshIntervalSecs is how often the pricing cache is refreshed from the
	// provider APIs. Defaults to 3600 (1 hour).
	RefreshIntervalSecs int `yaml:"refreshIntervalSecs"`

	// PricingFilePath is an optional path to a local JSON pricing override file.
	// When set the cache bootstraps from this file, useful in air-gapped environments.
	PricingFilePath string `yaml:"pricingFilePath"`
}

// Current is the loaded configuration singleton.
var Current *Config

func init() {
	Current = config.MustLoadService[Config]("cmd/inference-gateway", config.Environment())
	loadSecrets(Current)
}

// loadSecrets reads provider credentials from environment variables into the
// config struct. This keeps secrets out of YAML and out of version control.
func loadSecrets(c *Config) {
	env := func(key string) string { return os.Getenv(key) }

	// Allow the JWKS URL to be overridden via env — useful for local Tier-1
	// development where the identity service runs on localhost rather than
	// in-cluster.
	if jwksURL := env("ORBIT_TOKEN_JWKS_URL"); jwksURL != "" {
		c.Token.JWKSURL = jwksURL
	}

	c.Providers.Bedrock.AccessKeyID = env("AWS_ACCESS_KEY_ID")
	c.Providers.Bedrock.SecretAccessKey = env("AWS_SECRET_ACCESS_KEY")
	c.Providers.Bedrock.SessionToken = env("AWS_SESSION_TOKEN")
	c.Providers.VertexAI.CredentialsJSON = env("VERTEX_CREDENTIALS_JSON")
	c.Providers.Azure.APIKey = env("AZURE_AI_API_KEY")
}
