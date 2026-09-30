package vertexai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	anthropicvertex "github.com/anthropics/anthropic-sdk-go/vertex"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds one Vertex AI project and location.
type Config struct {
	ProjectID string
	Location  string
	// AnthropicLocation overrides Location for Claude-on-Vertex requests.
	// Claude 4 models are only available in specific regions (e.g. us-east5)
	// while Gemini/Llama models remain in Location (e.g. us-central1).
	// When empty, Location is used for all models.
	AnthropicLocation string
	// CredentialsJSON is a service account key. When empty, Application
	// Default Credentials apply.
	CredentialsJSON []byte
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
	name            inferencegw.ProviderName
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
func New(cfg *Config, name inferencegw.ProviderName, logger logging.Logger) (*Connector, error) {
	ctx := context.Background()

	// A service account key when configured. Otherwise ADC: on GKE the
	// metadata server (Workload Identity); locally
	// ~/.config/gcloud/application_default_credentials.json.
	creds, err := googleCredentials(ctx, cfg.CredentialsJSON)
	if err != nil {
		return nil, err
	}
	tokenSource := creds.TokenSource

	// OpenAI-compatible endpoint for Gemini and Llama models on Vertex AI.
	openaiEndpoint := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/endpoints/openapi/",
		cfg.Location, cfg.ProjectID, cfg.Location,
	)
	// No client-level timeout — context deadline governs streaming lifetime.
	// http.Client.Timeout covers the full body read; for SSE streams that
	// means the generation is killed after 30 s regardless of progress.
	// The oauth2.Transport wraps the tracing transport and injects
	// "Authorization: Bearer <google-access-token>" on every request,
	// overriding whatever the openai-go SDK would otherwise send.
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithAPIKey("-"), // placeholder — oauth2.Transport overrides the header
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
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
		anthropicvertex.WithCredentials(context.Background(), cfg.AnthropicLocation, cfg.ProjectID, creds),
		anthropicopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
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
		name:            name,
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		listHTTPClient:  listHTTPClient,
		cfg:             cfg,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return c.name
}

// isClaudeModel reports whether the model ID refers to a Claude model.
// Claude models on Vertex AI use the Anthropic Messages endpoint exclusively —
// they are not available on the OpenAI-compatible endpoint.
func isClaudeModel(modelID string) bool {
	return strings.HasPrefix(modelID, "claude-")
}

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// googleCredentials parses a service account key, or finds Application
// Default Credentials when none is given.
func googleCredentials(ctx context.Context, keyJSON []byte) (*google.Credentials, error) {
	if len(keyJSON) > 0 {
		creds, err := google.CredentialsFromJSONWithType(ctx, keyJSON, google.ServiceAccount, cloudPlatformScope)
		if err != nil {
			return nil, fmt.Errorf("vertexai: parse service account key: %w", err)
		}
		return creds, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("vertexai: find default credentials: %w", err)
	}
	return creds, nil
}
