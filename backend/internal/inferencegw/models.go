// Package inferencegw implements the Orbit Inference Gateway service.
// It provides a provider-agnostic inference API, routing requests to
// cloud AI providers (AWS Bedrock, Google Vertex AI, Azure AI Inference)
// with per-principal usage attribution and policy enforcement hooks.
package inferencegw

import (
	"errors"
	"time"
)

// ─── Dialect ─────────────────────────────────────────────────────────────────

// APIDialect identifies which wire protocol was used for the inbound request.
type APIDialect string

const (
	// DialectOpenAI is the OpenAI Chat Completions API format (default).
	DialectOpenAI APIDialect = "openai"
	// DialectAnthropic is the Anthropic Messages API format.
	DialectAnthropic APIDialect = "anthropic"
)

// ─── Provider ────────────────────────────────────────────────────────────────

// ProviderName identifies a cloud AI provider.
type ProviderName string

const (
	ProviderBedrock  ProviderName = "bedrock"
	ProviderVertexAI ProviderName = "vertexai"
	ProviderAzure    ProviderName = "azure"
)

// ─── Routing ─────────────────────────────────────────────────────────────────

// RoutingStrategy controls how the routing engine selects a provider+model.
type RoutingStrategy string

const (
	// StrategyLowestCost selects the cheapest enabled model for the request.
	StrategyLowestCost RoutingStrategy = "lowest_cost"
	// StrategyFailoverChain tries providers in priority order, advancing on error.
	StrategyFailoverChain RoutingStrategy = "failover_chain"
	// StrategyExplicit honors the model field in the request exactly.
	StrategyExplicit RoutingStrategy = "explicit"
	// StrategyPriority tries virtual model routes in configured priority order.
	StrategyPriority RoutingStrategy = "priority"
	// StrategyWeighted selects a virtual model route probabilistically by weight.
	StrategyWeighted RoutingStrategy = "weighted"
)

// ─── Request (routing only) ───────────────────────────────────────────────────

// ChatCompletionRequest carries the fields the routing engine needs to select
// a provider and model. For the OpenAI dialect, the full wire request is
// decoded as openai.ChatCompletionNewParams in the handler and passed directly
// to the connector — this struct is never used for provider I/O.
type ChatCompletionRequest struct {
	Model string `json:"model"`
}

// ─── Usage record ────────────────────────────────────────────────────────────

// UsageStatus describes the outcome of an inference request.
type UsageStatus string

const (
	UsageStatusSuccess       UsageStatus = "success"
	UsageStatusProviderError UsageStatus = "provider_error"
	UsageStatusPolicyDenied  UsageStatus = "policy_denied"
	UsageStatusAuthError     UsageStatus = "auth_error"
	UsageStatusTimeout       UsageStatus = "timeout"
	UsageStatusRoutingFailed UsageStatus = "routing_failed"
)

// InferenceUsageRecord is a completed inference event written to the database.
type InferenceUsageRecord struct {
	ID               string
	TenantID         string
	PrincipalID      string
	PrincipalType    string
	APIDialect       APIDialect
	Provider         *ProviderName // nil when routing failed
	ModelID          string
	RoutingStrategy  *RoutingStrategy // nil when routing failed
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCostUSD float64
	Status           UsageStatus
	RetryCount       int
	ErrorCode        *string
	LatencyMS        int
	CreatedAt        time.Time
}

// InferenceUsageFilter is used to query usage records.
// TenantID is required — List and Get both reject an empty value to guarantee
// tenant isolation. Callers must populate it from the verified principal, never
// from user-supplied request data.
type InferenceUsageFilter struct {
	TenantID      string
	PrincipalID   *string
	PrincipalType *string
	Provider      *ProviderName
	APIDialect    *APIDialect
	Status        *UsageStatus
	From          *time.Time
	To            *time.Time
	Limit         int
	Offset        int
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrPolicyDenied          = errors.New("inference request denied by policy")
	ErrNoProvidersEnabled    = errors.New("no providers enabled")
	ErrProviderNotConfigured = errors.New("provider is not enabled or configured")
	ErrRoutingFailed         = errors.New("routing failed: no suitable provider/model found")
	ErrProviderError         = errors.New("provider returned an error")
	ErrInvalidRequest        = errors.New("invalid inference request")
	ErrTokenParseFailed      = errors.New("failed to parse platform token")
	ErrUsageRecordNotFound   = errors.New("inference usage record not found")
	ErrVirtualModelNotFound  = errors.New("virtual model not found")
	ErrNoCatalog             = errors.New("virtual model catalog not loaded")
)
