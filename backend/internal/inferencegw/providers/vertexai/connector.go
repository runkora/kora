package vertexai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	anthropicvertex "github.com/anthropics/anthropic-sdk-go/vertex"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds the Vertex AI connector configuration.
// Populated exclusively from environment variables via LoadConfig — never from YAML.
type Config struct {
	ProjectID string
	Location  string
	// AnthropicLocation overrides Location for Claude-on-Vertex requests.
	// Claude 4 models are only available in specific regions (e.g. us-east5)
	// while Gemini/Llama models remain in Location (e.g. us-central1).
	// When empty, Location is used for all models.
	AnthropicLocation string
}

// LoadConfig reads Vertex AI configuration from environment variables.
// Returns nil when VERTEX_ENABLED is not "true", indicating the connector
// should not be registered. Returns an error if enabled but misconfigured.
//
// Environment variables:
//
//	VERTEX_ENABLED              — set to "true" to enable
//	VERTEX_PROJECT_ID           — GCP project ID (required when enabled)
//	VERTEX_LOCATION             — GCP region for Gemini/Llama models, e.g. "us-central1" (required when enabled)
//	VERTEX_ANTHROPIC_LOCATION   — GCP region for Claude models, e.g. "us-east5" (optional; defaults to VERTEX_LOCATION)
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

	anthropicLocation := os.Getenv("VERTEX_ANTHROPIC_LOCATION")
	if anthropicLocation == "" {
		anthropicLocation = location
	}

	return &Config{
		ProjectID:         projectID,
		Location:          location,
		AnthropicLocation: anthropicLocation,
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
// Vertex AI topology note: Claude models are only available on the Anthropic
// Messages endpoint, not the OpenAI-compatible endpoint. ChatCompletion and
// ChatCompletionStream internally detect claude- model IDs and route them
// through the Anthropic client with full SDK-to-SDK conversion, preserving
// all tool_calls, structured outputs, and other fields.
type Connector struct {
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	// listHTTPClient is used exclusively by ListModels to query the Vertex AI
	// Model Garden v1beta1 API. It shares the same ADC token source as the
	// inference clients but uses a shorter timeout.
	listHTTPClient *http.Client
	cfg            *Config
	logger         logging.Logger
}

// New constructs a Vertex AI Connector from a validated Config.
func New(cfg *Config, logger logging.Logger) (*Connector, error) {
	ctx := context.Background()

	// Obtain an ADC token source scoped to the Vertex AI API.
	// On GKE this queries the metadata server (Workload Identity);
	// locally this reads ~/.config/gcloud/application_default_credentials.json.
	tokenSource, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("vertexai: get ADC token source: %w", err)
	}

	// OpenAI-compatible endpoint for Gemini and Llama models on Vertex AI.
	openaiEndpoint := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/endpoints/openapi/",
		cfg.Location, cfg.ProjectID, cfg.Location,
	)
	// The oauth2.Transport wraps the tracing transport and injects
	// "Authorization: Bearer <google-access-token>" on every request,
	// overriding whatever the openai-go SDK would otherwise send.
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithAPIKey("-"), // placeholder — oauth2.Transport overrides the header
		openaiopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			&oauth2.Transport{
				Source: tokenSource,
				Base:   httpclient.NewTracingTransport("orbit.vertexai-openai", nil),
			},
		)),
	)

	// Anthropic client for Claude-on-Vertex.
	// WithGoogleAuth sets the Vertex AI base URL and model path — we keep it for that,
	// but we override the HTTP client so we can insert our tracing transport.
	// The oauth2.Transport injects "Authorization: Bearer <adc-token>" on every request
	// using the same tokenSource as the OpenAI client above, then the inner tracing
	// transport records the OTel client span with W3C traceparent.
	// AnthropicLocation may differ from Location — Claude 4 models are only available
	// in certain regions (e.g. us-east5) while Gemini/Llama stay in Location.
	anthropicClient := anthropic.NewClient(
		anthropicvertex.WithGoogleAuth(context.Background(), cfg.AnthropicLocation, cfg.ProjectID),
		anthropicopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			&oauth2.Transport{
				Source: tokenSource,
				Base:   httpclient.NewTracingTransport("orbit.vertexai-anthropic", nil),
			},
		)),
	)

	// HTTP client for Model Garden listing — shares the same ADC token source
	// but uses a shorter timeout since list calls are expected to be fast.
	listHTTPClient := httpclient.NewHTTPClient(10*time.Second,
		&oauth2.Transport{
			Source: tokenSource,
			Base:   httpclient.NewTracingTransport("orbit.vertexai-list", nil),
		},
	)

	return &Connector{
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		listHTTPClient:  listHTTPClient,
		cfg:             cfg,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderVertexAI
}

// isClaudeModel reports whether the model ID refers to a Claude model.
// Claude models on Vertex AI use the Anthropic Messages endpoint exclusively —
// they are not available on the OpenAI-compatible endpoint.
func isClaudeModel(modelID string) bool {
	return strings.HasPrefix(modelID, "claude-")
}
