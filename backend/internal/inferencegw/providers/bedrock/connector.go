package bedrock

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	awsbedrock "github.com/anthropics/anthropic-sdk-go/bedrock"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	bedrockSDK "github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrockSDKtypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
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
// Bedrock is a multi-endpoint provider: Anthropic/Claude models must be called
// via the Anthropic Messages API (anthropicClient) because the OpenAI-compatible
// Bedrock endpoint does not support them. ChatCompletion and ChatCompletionStream
// therefore inspect the model ID and self-dispatch to the correct transport so
// that OpenAI-dialect callers targeting Claude models work transparently.
type Connector struct {
	openaiClient    *openai.Client
	anthropicClient *anthropic.Client
	bedrockClient   *bedrockSDK.Client
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
	// The tracing transport wraps SigV4 so every call gets an OTel client span
	// and W3C traceparent injection before SigV4 signs the final headers.
	openaiEndpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/openai/v1", cfg.Region)
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			httpclient.NewTracingTransport("orbit.bedrock-openai", newSigV4Transport(awsCfg)),
		)),
	)

	// Anthropic Messages API on Bedrock — SigV4 is handled by the SDK's bedrockMiddleware
	// (applied via WithConfig), so the tracing transport wraps http.DefaultTransport only.
	// This gives us OTel client spans + W3C traceparent on every Anthropic call without
	// interfering with the SDK's auth middleware chain.
	anthropicClient := anthropic.NewClient(
		awsbedrock.WithConfig(awsCfg),
		anthropicOption.WithHTTPClient(httpclient.NewHTTPClient(30*time.Second,
			httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
		)),
	)

	bedrockClient := bedrockSDK.NewFromConfig(awsCfg)

	return &Connector{
		openaiClient:    &openaiClient,
		anthropicClient: &anthropicClient,
		bedrockClient:   bedrockClient,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderBedrock
}

// fmEntry holds data from a ListFoundationModels summary.
type fmEntry struct {
	name       string                          // human-readable name from the API (e.g. "Claude 3 Sonnet")
	inputMods  []bedrockSDKtypes.ModelModality // input modalities from the API
	outputMods []bedrockSDKtypes.ModelModality // output modalities from the API
}

// profileEntry holds data from a ListInferenceProfiles summary.
type profileEntry struct {
	id   string // canonical profile ID (e.g. "apac.anthropic.claude-3-haiku-20240307-v1:0")
	name string // human-readable name from the API (e.g. "APAC Anthropic Claude 3 Haiku")
}

// ListModels returns the models that can be invoked via on-demand (pay-per-token)
// throughput. It deduplicates models that appear both as a foundation model ID
// and as an inference profile ID, always preferring the profile ID (which is
// what the Bedrock OpenAI-compatible endpoint requires for many newer models).
//
// Algorithm:
//  1. Fetch all foundation models; keep only those with ON_DEMAND throughput support.
//     Note: ModelLifecycleStatus is not populated in all regions (returns empty in
//     ap-southeast-2), so we do NOT filter on lifecycle status.
//  2. Fetch all system-defined cross-region inference profiles (paginated).
//  3. For each profile, strip its region prefix (apac., au., global., us., …) to
//     find the base FM ID. When a match is found in the on-demand set, the profile
//     ID replaces the raw FM ID so callers get the routable form.
//  4. Return the merged, deduplicated list with real display names from the API.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	activeFMs, excludedFMs, err := c.listOnDemandFMs(ctx)
	if err != nil {
		return nil, err
	}
	profilesByBase := c.listSystemProfiles(ctx)
	return mergeModels(activeFMs, excludedFMs, profilesByBase), nil
}

// listOnDemandFMs fetches all foundation models that support ON_DEMAND throughput.
// ModelLifecycleStatus is intentionally not checked — it is unpopulated in some
// regions (returns empty string in ap-southeast-2).
// listOnDemandFMs fetches all foundation models that support ON_DEMAND throughput.
// Returns two maps: activeFMs (passed all filters) and excludedFMs (rejected as LEGACY).
// ModelLifecycle nil is treated as active (field is unpopulated in some regions, e.g. ap-southeast-2).
func (c *Connector) listOnDemandFMs(ctx context.Context) (activeFMs map[string]fmEntry, excludedFMs map[string]struct{}, err error) {
	out, err := c.bedrockClient.ListFoundationModels(ctx, &bedrockSDK.ListFoundationModelsInput{})
	if err != nil {
		return nil, nil, fmt.Errorf("bedrock: list foundation models: %w", err)
	}
	activeFMs = make(map[string]fmEntry, len(out.ModelSummaries))
	excludedFMs = make(map[string]struct{}, 8)
	for _, m := range out.ModelSummaries {
		if m.ModelId == nil {
			continue
		}
		if !slices.Contains(m.InferenceTypesSupported, bedrockSDKtypes.InferenceTypeOnDemand) {
			continue
		}
		// Exclude models explicitly marked LEGACY by AWS and track their IDs so that
		// any inference profiles pointing back to the same base FM are also suppressed.
		if m.ModelLifecycle != nil && m.ModelLifecycle.Status == bedrockSDKtypes.FoundationModelLifecycleStatusLegacy {
			excludedFMs[*m.ModelId] = struct{}{}
			continue
		}
		name := ""
		if m.ModelName != nil {
			name = *m.ModelName
		}
		activeFMs[*m.ModelId] = fmEntry{
			name:       name,
			inputMods:  m.InputModalities,
			outputMods: m.OutputModalities,
		}
	}
	return activeFMs, excludedFMs, nil
}

// listSystemProfiles fetches all system-defined cross-region inference profiles
// (paginated) and returns a map of base FM ID → profileEntry. When a base FM
// has multiple regional profiles, only the first one returned by the API is kept.
func (c *Connector) listSystemProfiles(ctx context.Context) map[string]profileEntry {
	result := make(map[string]profileEntry, 64)
	var nextToken *string
	for {
		out, err := c.bedrockClient.ListInferenceProfiles(ctx, &bedrockSDK.ListInferenceProfilesInput{
			TypeEquals: bedrockSDKtypes.InferenceProfileTypeSystemDefined,
			NextToken:  nextToken,
		})
		if err != nil {
			// Non-fatal: some accounts or regions have no inference profiles.
			c.logger.Warn(ctx, "bedrock: list inference profiles skipped",
				logging.Field{Key: "error", Value: err})
			return result
		}
		for _, p := range out.InferenceProfileSummaries {
			if p.InferenceProfileId == nil {
				continue
			}
			profileID := *p.InferenceProfileId
			baseID := stripRegionPrefix(profileID)
			if _, exists := result[baseID]; exists {
				continue // keep only the first profile per base FM
			}
			name := ""
			if p.InferenceProfileName != nil {
				name = *p.InferenceProfileName
			}
			result[baseID] = profileEntry{id: profileID, name: name}
		}
		if out.NextToken == nil || *out.NextToken == "" {
			return result
		}
		nextToken = out.NextToken
	}
}

// mergeModels combines on-demand foundation models and inference profiles into a
// deduplicated []ProviderModel list. Where a profile exists for a given FM, the
// profile ID (routable form) is used as the canonical ID rather than the raw FM ID.
func mergeModels(activeFMs map[string]fmEntry, excludedFMs map[string]struct{}, profilesByBase map[string]profileEntry) []inferencegw.ProviderModel {
	seen := make(map[string]bool, len(activeFMs))
	models := make([]inferencegw.ProviderModel, 0, len(activeFMs))

	for fmID, fm := range activeFMs {
		canonicalID := fmID
		// Use the routable profile ID when available. We keep the FM's ModelName
		// (cleaner than the profile name which includes the region, e.g.
		// "APAC Anthropic Claude 3 Haiku" vs "Claude 3 Haiku").
		if profile, ok := profilesByBase[fmID]; ok {
			canonicalID = profile.id
		}
		if seen[canonicalID] {
			continue
		}
		seen[canonicalID] = true
		inputMods := convertModalities(fm.inputMods)
		outputMods := convertModalities(fm.outputMods)
		models = append(models, buildProviderModel(canonicalID, fm.name, inputMods, outputMods))
	}

	// Include profiles whose base FM is not in activeFMs
	// (e.g. a model exposed via profiles before it appears in ListFoundationModels).
	// Skip profiles whose base FM was explicitly excluded as LEGACY.
	for baseID, profile := range profilesByBase {
		if _, inActive := activeFMs[baseID]; inActive {
			continue // already handled above
		}
		if _, excluded := excludedFMs[baseID]; excluded {
			continue // base FM is LEGACY - suppress its profiles too
		}
		if seen[profile.id] {
			continue
		}
		seen[profile.id] = true
		// Profiles don't carry modality data - inferred from model ID in buildProviderModel.
		models = append(models, buildProviderModel(profile.id, profile.name, nil, nil))
	}

	return models
}

// isAnthropicModel reports whether the given model ID belongs to the Anthropic
// family. It handles all three forms the routing engine may deliver:
//
//   - Canonical Bedrock namespace:  "anthropic.claude-3-5-sonnet-20241022-v1:0"
//   - Cross-region profile:         "us.anthropic.claude-3-5-sonnet-20241022-v1:0"
//   - Short alias (client-supplied or cold-start before catalog is warm):
//     "claude-sonnet-4-6", "claude-3-5-haiku", etc.
//
// When the model catalog is warm, the routing engine resolves short aliases to
// their canonical Bedrock IDs before the connector is called, so the
// "anthropic." prefix check is the common path. The "claude-" prefix check is
// the cold-start / direct-alias fallback: every Anthropic chat model carries
// this prefix, and no other Bedrock provider uses it.
//
// HasPrefix is used in both cases rather than Contains so that a hypothetical
// third-party model whose name merely contains the substring cannot
// accidentally be routed to the Anthropic client.
func isAnthropicModel(id string) bool {
	base := stripRegionPrefix(id)
	// Fully-qualified Bedrock form: "anthropic.claude-..."
	if strings.HasPrefix(base, "anthropic.") {
		return true
	}
	// Short alias form — "claude-..." is the universal Anthropic model prefix
	// used by clients and by the routing engine on a cold start (before the
	// first ListModels sweep has populated the alias table).
	return strings.HasPrefix(base, "claude-")
}
