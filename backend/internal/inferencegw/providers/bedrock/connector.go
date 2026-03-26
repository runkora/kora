package bedrock

import (
	"context"
	"fmt"
	"os"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	awsbedrock "github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// Config holds the Bedrock connector configuration.
// Populated exclusively from environment variables via LoadConfig — never from YAML.
type Config struct {
	Region          string
	AccessKeyID     string // optional — falls back to default AWS credential chain
	SecretAccessKey string // optional — falls back to default AWS credential chain
	SessionToken    string // optional — for temporary credentials
}

// LoadConfig reads Bedrock configuration from environment variables.
// Returns nil when BEDROCK_ENABLED is not "true", indicating the connector
// should not be registered. Returns an error if enabled but misconfigured.
//
// Environment variables:
//
//	BEDROCK_ENABLED          — set to "true" to enable
//	BEDROCK_REGION           — AWS region, e.g. "us-east-1" (required when enabled)
//	AWS_ACCESS_KEY_ID        — optional; falls back to the default AWS credential chain
//	AWS_SECRET_ACCESS_KEY    — optional; falls back to the default AWS credential chain
//	AWS_SESSION_TOKEN        — optional; for temporary credentials
func LoadConfig() (*Config, error) {
	if os.Getenv("BEDROCK_ENABLED") != "true" {
		return nil, nil
	}

	region := os.Getenv("BEDROCK_REGION")
	if region == "" {
		return nil, fmt.Errorf("bedrock: BEDROCK_REGION is required when BEDROCK_ENABLED=true")
	}

	return &Config{
		Region:          region,
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	}, nil
}

// Factory implements inferencegw.ConnectorFactory for AWS Bedrock.
type Factory struct{}

// Name returns the provider identifier.
func (Factory) Name() inferencegw.ProviderName { return inferencegw.ProviderBedrock }

// Build loads config from env vars and constructs the connector.
// Returns (nil, nil) when BEDROCK_ENABLED is not "true".
func (Factory) Build(logger logging.Logger) (inferencegw.ProviderConnector, error) {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return nil, err
	}
	return New(cfg, logger)
}

// Connector implements inferencegw.ProviderConnector for AWS Bedrock.
// It wraps the OpenAI-compatible Bedrock endpoint (via openai-go) and
// the Anthropic Messages API on Bedrock (via anthropic-sdk-go/bedrock).
//
// Dialect selection is the caller's responsibility. The connector never
// inspects the model name to choose a path.
type Connector struct {
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	logger          logging.Logger
}

// New constructs a Bedrock Connector from a validated Config.
func New(cfg *Config, logger logging.Logger) (*Connector, error) {
	ctx := context.Background()

	var awsCfg aws.Config
	var err error

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		awsCfg, err = awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(cfg.Region),
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
			),
		)
	} else {
		// Fall back to the default AWS credential chain (IAM role, instance profile, etc.).
		awsCfg, err = awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(cfg.Region),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("bedrock: load AWS config: %w", err)
	}

	// OpenAI-compatible Bedrock endpoint — requires SigV4 signing on every request.
	// We provide a signed *http.Client via httpclient.NewHTTPClient so the openai-go
	// SDK never needs to know about AWS auth.
	openaiEndpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/openai/v1", cfg.Region)
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second, newSigV4Transport(awsCfg))),
	)

	anthropicClient := anthropic.NewClient(
		awsbedrock.WithConfig(awsCfg),
	)

	return &Connector{
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderBedrock
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
