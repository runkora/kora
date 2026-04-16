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

// RoutingStrategy is the strategy value recorded in InferenceUsageRecord.
// It is the resolved, dispatch-time strategy — distinct from VirtualModelStrategy
// (defined in virtual_model.go) which is the catalog-config strategy configured
// by admins on each virtual model.
//
// At dispatch time, targetsForVirtualModel maps VirtualModelStrategy → RoutingStrategy
// so usage records carry a consistent strategy label regardless of which catalog
// config type drove the decision.
type RoutingStrategy string

const (
	// StrategyLowestCost selects the cheapest enabled model for the request.
	StrategyLowestCost RoutingStrategy = "lowest_cost"
	// StrategyFailoverChain tries providers in priority order, advancing on error.
	StrategyFailoverChain RoutingStrategy = "failover_chain"
	// StrategyExplicit honored the model field in the request exactly.
	// Retained in the DB CHECK constraint for historical usage records;
	// no longer produced by any current routing code path.
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
	ID               string           `json:"id"`
	TenantID         string           `json:"tenant_id"`
	PrincipalID      string           `json:"principal_id"`
	PrincipalType    string           `json:"principal_type"`
	TokenID          *string          `json:"token_id"`
	APIDialect       APIDialect       `json:"api_dialect"`
	Provider         *ProviderName    `json:"provider"`
	ModelID          string           `json:"model_id"`
	ModelFamily      *string          `json:"model_family"`
	RoutingStrategy  *RoutingStrategy `json:"routing_strategy"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	TotalTokens      int              `json:"total_tokens"`
	EstimatedCostUSD float64          `json:"estimated_cost_usd"`
	Status           UsageStatus      `json:"status"`
	RetryCount       int              `json:"retry_count"`
	ErrorCode        *string          `json:"error_code"`
	LatencyMS        int              `json:"latency_ms"`
	CreatedAt        time.Time        `json:"created_at"`
}

// InferenceUsageFilter is used to query usage records.
// TenantID is required — List and Get both reject an empty value to guarantee
// tenant isolation. Callers must populate it from the verified principal, never
// from user-supplied request data.
//
// TokenID, when set, restricts results to a single PAT. It must always be
// paired with PrincipalID — the AND predicate on the verified principal_id
// prevents any cross-user row access without additional pre-validation.
type InferenceUsageFilter struct {
	TenantID      string
	PrincipalID   *string
	TokenID       *string // PAT record UUID; nil returns all records for the principal
	PrincipalType *string
	Provider      *ProviderName
	ModelID       *string // filter by model_id column value
	ModelFamily   *string // filter by model_family column value
	APIDialect    *APIDialect
	Status        *UsageStatus
	From          *time.Time
	To            *time.Time
	Limit         int
	CursorTime    *time.Time // if set, return rows strictly before this timestamp
	CursorID      *string    // paired with CursorTime for stable sub-second ordering
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
