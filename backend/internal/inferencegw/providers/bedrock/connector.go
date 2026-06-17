package bedrock

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	awsbedrock "github.com/anthropics/anthropic-sdk-go/bedrock"
	anthropicOption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	bedrockSDK "github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrockSDKtypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	bedrockruntimeSDK "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	openai "github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"

	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/inferencegw/providers/bridge"
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
// It wraps three separate endpoints:
//   - openaiClient: OpenAI-compatible Bedrock endpoint (bedrock-runtime.{region}.amazonaws.com/openai/v1)
//     for non-Anthropic, non-Mantle models (Titan, Nova, Llama, Mistral, etc.)
//   - anthropicClient: Anthropic Messages API on Bedrock for Claude models
//   - mantleClient: Bedrock Mantle endpoint (bedrock-mantle.{region}.api.aws/v1) for OpenAI
//     models (e.g. openai.gpt-oss-120b) hosted by AWS. Supports ChatCompletion and Responses API.
//
// ChatCompletion/ChatCompletionStream dispatch to anthropicClient for Anthropic models,
// mantleClient for Mantle models, and openaiClient for everything else.
// anthropicBridge translates Responses API requests to the Anthropic Messages API for
// Claude models (which lack native Responses API support on Bedrock's OpenAI-compat endpoint).
// openAIBridge translates Responses API requests to Chat Completions for all other
// OpenAI-compatible models (Nova, Llama, Mistral, etc.) on the standard Bedrock endpoint.
// mantleModelIDs is populated at ListModels time; before it is populated, Mantle model
// requests fall through to openaiClient, fail, and the router's failover advances to
// the next target (same cold-start behavior as all provider model caches).
type Connector struct {
	openaiClient    *openai.Client
	mantleClient    *openai.Client
	anthropicClient *anthropic.Client
	anthropicBridge *bridge.AnthropicResponsesBridge
	openAIBridge    *bridge.ChatCompletionsResponsesBridge
	bedrockClient   *bedrockSDK.Client
	runtimeClient   *bedrockruntimeSDK.Client
	region          string // AWS region; used to prefer regionally-matching inference profiles
	logger          logging.Logger
	modelRegistry   atomic.Pointer[map[string]backendID]
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
	// No client-level timeout — context deadline governs streaming lifetime.
	// http.Client.Timeout covers the full body read; for SSE streams that
	// means the generation is killed after 30 s regardless of progress.
	openaiClient := openai.NewClient(
		openaiopt.WithBaseURL(openaiEndpoint),
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.bedrock-openai", newSigV4Transport(awsCfg)),
		)),
	)

	// Bedrock Mantle endpoint — hosts OpenAI models (e.g. openai.gpt-oss-120b) under
	// the same AWS credentials via SigV4. Uses the Responses API and ChatCompletions.
	mantleEndpoint := fmt.Sprintf("https://bedrock-mantle.%s.api.aws/v1", cfg.Region)
	mantleClient := openai.NewClient(
		openaiopt.WithBaseURL(mantleEndpoint),
		openaiopt.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.bedrock-mantle", newSigV4Transport(awsCfg)),
		)),
	)

	// Anthropic Messages API on Bedrock — SigV4 is handled by the SDK's bedrockMiddleware
	// (applied via WithConfig), so the tracing transport wraps http.DefaultTransport only.
	// This gives us OTel client spans + W3C traceparent on every Anthropic call without
	// interfering with the SDK's auth middleware chain.
	anthropicClient := anthropic.NewClient(
		awsbedrock.WithConfig(awsCfg),
		anthropicOption.WithHTTPClient(httpclient.NewStreamingHTTPClient(
			httpclient.NewTracingTransport("orbit.bedrock-anthropic", nil),
		)),
	)

	bedrockClient := bedrockSDK.NewFromConfig(awsCfg)
	runtimeClient := bedrockruntimeSDK.NewFromConfig(awsCfg)

	return &Connector{
		openaiClient:    &openaiClient,
		mantleClient:    &mantleClient,
		anthropicClient: &anthropicClient,
		anthropicBridge: bridge.NewAnthropicResponsesBridge(&anthropicClient, inferencegw.ProviderBedrock, logger),
		openAIBridge:    bridge.NewChatCompletionsResponsesBridge(&openaiClient, inferencegw.ProviderBedrock, logger),
		bedrockClient:   bedrockClient,
		runtimeClient:   runtimeClient,
		region:          cfg.Region,
		logger:          logger,
	}, nil
}

func (c *Connector) Name() inferencegw.ProviderName {
	return inferencegw.ProviderBedrock
}

// regionToProfilePrefix maps the configured AWS region to the corresponding
// cross-region inference profile prefix. For example, "us-east-1" → "us.",
// "eu-west-1" → "eu.", "ap-northeast-1" → "ap.".
// Returns "" for regions that have no known cross-region prefix.
func (c *Connector) regionToProfilePrefix() string {
	switch {
	case strings.HasPrefix(c.region, "us-"):
		return "us."
	case strings.HasPrefix(c.region, "eu-"):
		return "eu."
	case strings.HasPrefix(c.region, "ap-"):
		return "ap."
	default:
		return ""
	}
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
// It also fetches models from the Bedrock Mantle endpoint and appends them,
// updating the mantleModelIDs set for dispatch-time routing.
//
// Algorithm:
//  1. Fetch all foundation models; keep only those with ON_DEMAND throughput support.
//     Note: ModelLifecycleStatus is not populated in all regions (returns empty in
//     ap-southeast-2), so we do NOT filter on lifecycle status.
//  2. Fetch all system-defined cross-region inference profiles (paginated).
//  3. For each profile, strip its region prefix (apac., au., global., us., …) to
//     find the base FM ID. When a match is found in the on-demand set, the profile
//     ID replaces the raw FM ID so callers get the routable form.
//  4. Fetch Mantle models via the Mantle endpoint's /v1/models; populate mantleModelIDs.
//  5. Return the merged, deduplicated list with real display names from the API.
func (c *Connector) ListModels(ctx context.Context) ([]inferencegw.ProviderModel, error) {
	activeFMs, excludedFMs, err := c.listOnDemandFMs(ctx)
	if err != nil {
		return nil, err
	}
	profilesByBase := c.listSystemProfiles(ctx)
	models := mergeModels(activeFMs, excludedFMs, profilesByBase)
	mantleModels, mantleIDs := c.listMantleModels(ctx)

	// Deduplicate Mantle models against the merged list. Mantle returns bare
	// IDs (e.g. "moonshotai.kimi-k2.5") while mergeModels already produced the
	// routable profile-prefixed form (e.g. "us.moonshotai.kimi-k2.5"). Build a
	// set of stripped IDs already present, then only append Mantle models whose
	// bare ID is genuinely absent.
	strippedSeen := make(map[string]bool, len(models))
	for _, m := range models {
		strippedSeen[stripRegionPrefix(m.ModelID)] = true
	}
	for _, m := range mantleModels {
		if !strippedSeen[stripRegionPrefix(m.ModelID)] {
			models = append(models, m)
			strippedSeen[stripRegionPrefix(m.ModelID)] = true
		}
	}

	registry := make(map[string]backendID, len(models))
	for _, m := range models {
		if mantleIDs[m.ModelID] {
			registry[m.ModelID] = backendMantle
		} else {
			registry[m.ModelID] = inferBackendID(m.ModelID)
		}
	}
	c.modelRegistry.Store(&registry)

	return models, nil
}

// listMantleModels fetches the model list from the Bedrock Mantle endpoint.
// Returns the discovered models and the set of their IDs. Errors are logged
// as warnings and treated as non-fatal — the standard Bedrock model list is
// still returned even when Mantle is unavailable.
func (c *Connector) listMantleModels(ctx context.Context) ([]inferencegw.ProviderModel, map[string]bool) {
	iter := c.mantleClient.Models.ListAutoPaging(ctx)
	var models []inferencegw.ProviderModel
	ids := make(map[string]bool)
	for iter.Next() {
		id := iter.Current().ID
		ids[id] = true
		models = append(models, buildProviderModel(id, "", nil, nil))
	}
	if err := iter.Err(); err != nil {
		c.logger.Warn(ctx, "bedrock: list mantle models skipped",
			logging.Field{Key: "error", Value: err})
		return nil, nil
	}
	return models, ids
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
// has multiple regional profiles, the profile whose region prefix matches
// cfg.Region is preferred; if no match exists, the first one returned is used.
func (c *Connector) listSystemProfiles(ctx context.Context) map[string]profileEntry {
	result := make(map[string]profileEntry, 64)
	preferred := make(map[string]bool, 64) // baseID → whether result[baseID] is a regional match
	regionalPrefix := c.regionToProfilePrefix()
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
			name := ""
			if p.InferenceProfileName != nil {
				name = *p.InferenceProfileName
			}
			entry := profileEntry{id: profileID, name: name}
			isRegionalMatch := regionalPrefix != "" && strings.HasPrefix(profileID, regionalPrefix)
			_, exists := result[baseID]
			if !exists {
				result[baseID] = entry
				preferred[baseID] = isRegionalMatch
			} else if isRegionalMatch && !preferred[baseID] {
				// Upgrade to a regionally-matching profile so that a gateway in
				// us-east-1 prefers "us.anthropic.*" over "eu.anthropic.*".
				result[baseID] = entry
				preferred[baseID] = true
			}
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
