package azure

import (
	"context"
	"fmt"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	azureopenai "github.com/openai/openai-go/azure"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds the Azure AI connector configuration.
// Populated exclusively from environment variables via LoadConfig — never from YAML.
type Config struct {
	Endpoint          string
	APIKey            string
	APIVersion        string
	AnthropicEndpoint string // optional — defaults to Endpoint if unset
}

// LoadConfig reads Azure AI configuration from environment variables.
// Returns nil when AZURE_ENABLED is not "true", indicating the connector
// should not be registered. Returns an error if enabled but misconfigured.
//
// Environment variables:
//
//	AZURE_ENABLED                — set to "true" to enable
//	AZURE_AI_ENDPOINT            — Azure OpenAI resource endpoint (required when enabled)
//	AZURE_AI_API_KEY             — Azure resource API key (required when enabled)
//	AZURE_OPENAI_API_VERSION     — API version, e.g. "2024-02-01" (required when enabled)
//	AZURE_ANTHROPIC_ENDPOINT     — endpoint for Claude models via Anthropic Messages API
//	                               (optional; falls back to AZURE_AI_ENDPOINT if unset)
func LoadConfig() (*Config, error) {
	if os.Getenv("AZURE_ENABLED") != "true" {
		return nil, nil
	}

	endpoint := os.Getenv("AZURE_AI_ENDPOINT")
	if endpoint == "" {
		return nil, fmt.Errorf("azure: AZURE_AI_ENDPOINT is required when AZURE_ENABLED=true")
	}

	apiKey := os.Getenv("AZURE_AI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("azure: AZURE_AI_API_KEY is required when AZURE_ENABLED=true")
	}

	apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
	if apiVersion == "" {
		return nil, fmt.Errorf("azure: AZURE_OPENAI_API_VERSION is required when AZURE_ENABLED=true")
	}

	anthropicEndpoint := os.Getenv("AZURE_ANTHROPIC_ENDPOINT")
	if anthropicEndpoint == "" {
		anthropicEndpoint = endpoint
	}

	return &Config{
		Endpoint:          endpoint,
		APIKey:            apiKey,
		APIVersion:        apiVersion,
		AnthropicEndpoint: anthropicEndpoint,
	}, nil
}

// Factory implements inferencegw.ConnectorFactory for Azure AI.
type Factory struct{}

// Name returns the provider identifier.
func (Factory) Name() inferencegw.ProviderName { return inferencegw.ProviderAzure }

// Build loads config from env vars and constructs the connector.
// Returns (nil, nil) when AZURE_ENABLED is not "true".
func (Factory) Build(logger logging.Logger) (inferencegw.ProviderConnector, error) {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return nil, err
	}
	return New(cfg, logger)
}

// Connector implements inferencegw.ProviderConnector for Azure AI Inference.
// It wraps the Azure OpenAI endpoint (via openai-go/azure) for the OpenAI
// dialect and the Anthropic-compatible Azure endpoint for the Anthropic dialect.
//
// Dialect selection is the caller's responsibility. The connector never
// inspects the model name to choose a path.
type Connector struct {
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	logger          logging.Logger
}

// New constructs an Azure Connector from a validated Config.
func New(cfg *Config, logger logging.Logger) (*Connector, error) {
	openaiClient := openai.NewClient(
		azureopenai.WithEndpoint(cfg.Endpoint, cfg.APIVersion),
		azureopenai.WithAPIKey(cfg.APIKey),
	)

	anthropicClient := anthropic.NewClient(
		anthropicopt.WithBaseURL(cfg.AnthropicEndpoint),
		anthropicopt.WithHeader("api-key", cfg.APIKey),
	)

	return &Connector{
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		logger:          logger,
	}, nil
}

// Name returns the provider identifier.
func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderAzure
}

// ListModels returns the live list of model IDs from the Azure OpenAI endpoint.
func (c *Connector) ListModels(ctx context.Context) ([]string, error) {
	iter := c.openaiClient.Models.ListAutoPaging(ctx)
	var ids []string
	for iter.Next() {
		ids = append(ids, iter.Current().ID)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("azure: list models: %w", err)
	}
	return ids, nil
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
