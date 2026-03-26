package vertexai

import (
	"context"
	"fmt"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicvertex "github.com/anthropics/anthropic-sdk-go/vertex"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds the Vertex AI connector configuration.
// Populated exclusively from environment variables via LoadConfig — never from YAML.
type Config struct {
	ProjectID string
	Location  string
}

// LoadConfig reads Vertex AI configuration from environment variables.
// Returns nil when VERTEX_ENABLED is not "true", indicating the connector
// should not be registered. Returns an error if enabled but misconfigured.
//
// Environment variables:
//
//	VERTEX_ENABLED     — set to "true" to enable
//	VERTEX_PROJECT_ID  — GCP project ID (required when enabled)
//	VERTEX_LOCATION    — GCP region, e.g. "us-central1" (required when enabled)
//
// Auth uses Application Default Credentials (ADC) exclusively.
// On GKE use Workload Identity; locally set GOOGLE_APPLICATION_CREDENTIALS.
func LoadConfig() (*Config, error) {
	if os.Getenv("VERTEX_ENABLED") != "true" {
		return nil, nil
	}

	projectID := os.Getenv("VERTEX_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("vertexai: VERTEX_PROJECT_ID is required when VERTEX_ENABLED=true")
	}

	location := os.Getenv("VERTEX_LOCATION")
	if location == "" {
		return nil, fmt.Errorf("vertexai: VERTEX_LOCATION is required when VERTEX_ENABLED=true")
	}

	return &Config{
		ProjectID: projectID,
		Location:  location,
	}, nil
}

// Factory implements inferencegw.ConnectorFactory for Google Cloud Vertex AI.
type Factory struct{}

// Name returns the provider identifier.
func (Factory) Name() inferencegw.ProviderName { return inferencegw.ProviderVertexAI }

// Build loads config from env vars and constructs the connector.
// Returns (nil, nil) when VERTEX_ENABLED is not "true".
func (Factory) Build(logger logging.Logger) (inferencegw.ProviderConnector, error) {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return nil, err
	}
	return New(cfg, logger)
}

// Connector implements inferencegw.ProviderConnector for Google Cloud Vertex AI.
// It wraps the OpenAI-compatible Vertex AI endpoint (via openai-go) and
// the Anthropic Messages API on Vertex (via anthropic-sdk-go/vertex).
//
// Dialect selection is the caller's responsibility. The connector never
// inspects the model name to choose a path.
type Connector struct {
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	logger          logging.Logger
}

// New constructs a Vertex AI Connector from a validated Config.
func New(cfg *Config, logger logging.Logger) (*Connector, error) {
	// OpenAI-compatible endpoint for Gemini models on Vertex AI.
	openaiEndpoint := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/endpoints/openapi/",
		cfg.Location, cfg.ProjectID, cfg.Location,
	)
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithAPIKey("vertex"), // auth handled by ADC transport
	)

	// Anthropic client with Vertex transport — uses Application Default Credentials.
	anthropicClient := anthropic.NewClient(
		anthropicvertex.WithGoogleAuth(context.Background(), cfg.Location, cfg.ProjectID),
	)

	return &Connector{
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderVertexAI
}

// ChatCompletion sends a non-streaming request via the OpenAI-compatible dialect.
func (c *Connector) ChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	return c.openaiChatCompletion(ctx, req)
}

// ChatCompletionStream opens a streaming request via the OpenAI-compatible dialect.
func (c *Connector) ChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	return c.openaiChatCompletionStream(ctx, req)
}

// AnthropicChatCompletion sends a non-streaming request via the Anthropic Messages dialect.
func (c *Connector) AnthropicChatCompletion(ctx context.Context, req inferencegw.ChatCompletionRequest) (inferencegw.ChatCompletionResponse, error) {
	return c.anthropicChatCompletion(ctx, req)
}

// AnthropicChatCompletionStream opens a streaming request via the Anthropic Messages dialect.
func (c *Connector) AnthropicChatCompletionStream(ctx context.Context, req inferencegw.ChatCompletionRequest) (<-chan inferencegw.ChatCompletionChunk, error) {
	return c.anthropicChatCompletionStream(ctx, req)
}
