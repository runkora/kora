package azure

import (
	"context"
	"fmt"
	"slices"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	azureopenai "github.com/openai/openai-go/v3/azure"
	openaiopt "github.com/openai/openai-go/v3/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds one Azure AI resource.
type Config struct {
	Endpoint          string
	APIKey            string
	APIVersion        string
	AnthropicEndpoint string // optional — defaults to Endpoint if unset
}

// Connector implements inferencegw.ProviderConnector for Azure AI Inference.
// It wraps the Azure OpenAI endpoint (via openai-go/azure) for the OpenAI
// dialect and the Anthropic-compatible Azure endpoint for the Anthropic dialect.
//
// Dialect selection is the caller's responsibility. The connector never
// inspects the model name to choose a path.
type Connector struct {
	name            inferencegw.ProviderName
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	logger          logging.Logger
}

// New constructs an Azure Connector from a validated Config.
func New(cfg *Config, name inferencegw.ProviderName, logger logging.Logger) (*Connector, error) {
	// No client-level timeout — context deadline governs streaming lifetime.
	// http.Client.Timeout covers the full body read; for SSE streams that
	// means the generation is killed after 30 s regardless of progress.
	openaiClient := openai.NewClient(
		azureopenai.WithEndpoint(cfg.Endpoint, cfg.APIVersion),
		azureopenai.WithAPIKey(cfg.APIKey),
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.azure-openai", nil),
		)),
	)

	anthropicClient := anthropic.NewClient(
		anthropicopt.WithBaseURL(cfg.AnthropicEndpoint),
		anthropicopt.WithHeader("api-key", cfg.APIKey),
		anthropicopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.azure-anthropic", nil),
		)),
	)

	return &Connector{
		name:            name,
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		logger:          logger,
	}, nil
}

// Name returns the provider identifier.
func (c *Connector) Name() inferencegw.ProviderName {
	return c.name
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
			Provider:         c.name,
			ModelID:          id,
			Family:           inferencegw.InferFamily(id),
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
// Rules follow docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md: output modality drives capability, with text → chat,
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
func (c *Connector) ChatCompletion(ctx context.Context, params openai.ChatCompletionNewParams) (*inferencegw.OpenAIResult, error) {
	resp, err := c.openaiChatCompletion(ctx, params)
	if err != nil {
		return nil, err
	}
	return &inferencegw.OpenAIResult{Completion: resp}, nil
}

// ChatCompletionStream opens a streaming request via the OpenAI-compatible dialect.
func (c *Connector) ChatCompletionStream(ctx context.Context, params openai.ChatCompletionNewParams) (<-chan inferencegw.OpenAIStreamResult, error) {
	return c.openaiChatCompletionStream(ctx, params)
}
