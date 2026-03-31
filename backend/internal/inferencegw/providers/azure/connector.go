package azure

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	azureopenai "github.com/openai/openai-go/azure"
	openaiopt "github.com/openai/openai-go/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
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
		openaiopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			httpclient.NewTracingTransport("orbit.azure-openai", nil),
		)),
	)

	anthropicClient := anthropic.NewClient(
		anthropicopt.WithBaseURL(cfg.AnthropicEndpoint),
		anthropicopt.WithHeader("api-key", cfg.APIKey),
		anthropicopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			httpclient.NewTracingTransport("orbit.azure-anthropic", nil),
		)),
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

// ListModels returns the live list of models from the Azure OpenAI endpoint.
// Each model carries modality and dialect metadata inferred from the model ID.
// Azure does not expose a separate inference profile concept, so no deduplication
// is needed.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	iter := c.openaiClient.Models.ListAutoPaging(ctx)
	var models []inferencegw.ProviderModel
	for iter.Next() {
		id := iter.Current().ID
		inputMods, outputMods := inferAzureModalities(id)
		models = append(models, inferencegw.ProviderModel{
			Provider:         inferencegw.ProviderAzure,
			ModelID:          id,
			DisplayName:      displayNameFromID(id),
			InputModalities:  inputMods,
			OutputModalities: outputMods,
			Dialects:         inferAzureDialects(id, outputMods),
			Capabilities:     inferAzureCapabilities(id, outputMods),
		})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("azure: list models: %w", err)
	}
	return models, nil
}

// inferAzureModalities derives input/output modalities from an Azure model ID.
// Azure does not expose modality metadata via its Models API, so we infer from
// well-known model ID patterns.
func inferAzureModalities(id string) (input, output []inferencegw.Modality) {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "embed"):
		return []inferencegw.Modality{inferencegw.ModalityText},
			[]inferencegw.Modality{inferencegw.ModalityEmbedding}
	case strings.Contains(lower, "dall-e") || strings.Contains(lower, "dalle") ||
		strings.Contains(lower, "image-gen"):
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityImage}
	case strings.Contains(lower, "whisper") || strings.Contains(lower, "tts"):
		// Speech models - audio input/output
		return []inferencegw.Modality{inferencegw.ModalityAudio},
			[]inferencegw.Modality{inferencegw.ModalityText}
	default:
		// GPT-4o, GPT-4V, o1, o3, Mistral, Llama etc. are all text+vision chat models.
		// Azure routes GPT-4 Vision models under the same gpt-4 ID with vision capability.
		return []inferencegw.Modality{inferencegw.ModalityText, inferencegw.ModalityImage},
			[]inferencegw.Modality{inferencegw.ModalityText}
	}
}

// inferAzureCapabilities returns the endpoint families supported for an Azure model.
// Rules follow ADR-0026: output modality drives capability, with text → chat,
// embedding → embedding, image → image_generation. Audio speech models (whisper/tts)
// that output text are treated as chat-capable since they use the completions path.
func inferAzureCapabilities(id string, outputMods []inferencegw.Modality) []inferencegw.ModelCapability {
	lower := strings.ToLower(id)
	if strings.Contains(lower, "rerank") {
		return []inferencegw.ModelCapability{inferencegw.CapabilityRerank}
	}
	var caps []inferencegw.ModelCapability
	for _, m := range outputMods {
		switch m {
		case inferencegw.ModalityText:
			caps = append(caps, inferencegw.CapabilityChat)
		case inferencegw.ModalityEmbedding:
			caps = append(caps, inferencegw.CapabilityEmbedding)
		case inferencegw.ModalityImage:
			caps = append(caps, inferencegw.CapabilityImageGeneration)
		case inferencegw.ModalityVideo:
			caps = append(caps, inferencegw.CapabilityVideoGeneration)
		}
	}
	return caps
}

// inferAzureDialects returns the gateway wire protocols supported for an Azure model.
// Azure Claude models (deployed from the Azure Model Catalog) support both dialects.
// All other text-output models support the openai dialect only.
// Non-chat models (embedding, image-gen, audio) return no dialects.
func inferAzureDialects(id string, outputMods []inferencegw.Modality) []inferencegw.APIDialect {
	hasTextOutput := slices.Contains(outputMods, inferencegw.ModalityText)
	if !hasTextOutput {
		return nil
	}
	lower := strings.ToLower(id)
	if strings.Contains(lower, "claude") {
		return []inferencegw.APIDialect{inferencegw.DialectOpenAI, inferencegw.DialectAnthropic}
	}
	return []inferencegw.APIDialect{inferencegw.DialectOpenAI}
}

// displayNameFromID produces a human-readable label from an Azure model ID slug.
// e.g. "gpt-4o" → "Gpt 4O", "gpt-4o-mini" → "Gpt 4O Mini".
// This is intentionally simple — operator curation will improve names later.
func displayNameFromID(id string) string {
	if id == "" {
		return id
	}
	parts := splitSlug(id)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// splitSlug breaks a model ID on hyphens and underscores.
func splitSlug(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
}

// ChatCompletion sends a non-streaming request via the OpenAI-compatible dialect.
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return c.openaiChatCompletion(ctx, params)
}

// ChatCompletionStream opens a streaming request via the OpenAI-compatible dialect.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	return c.openaiChatCompletionStream(ctx, params)
}
