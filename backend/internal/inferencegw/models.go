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
	// DialectResponses is the OpenAI Responses API format (POST /v1/responses).
	DialectResponses APIDialect = "responses"
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
	UsageStatusSuccess        UsageStatus = "success"
	UsageStatusProviderError  UsageStatus = "provider_error"
	UsageStatusPolicyDenied   UsageStatus = "policy_denied"
	UsageStatusBudgetExceeded UsageStatus = "budget_exceeded"
	UsageStatusAuthError      UsageStatus = "auth_error"
	UsageStatusTimeout        UsageStatus = "timeout"
	UsageStatusRoutingFailed  UsageStatus = "routing_failed"
	UsageStatusCanceled       UsageStatus = "canceled"
)

// InferenceUsageRecord is a completed inference event written to the database.
type InferenceUsageRecord struct {
	ID                           string           `json:"id"`
	TenantID                     string           `json:"tenant_id"`
	PrincipalID                  string           `json:"principal_id"`
	PrincipalType                string           `json:"principal_type"`
	TokenID                      *string          `json:"token_id"`
	APIDialect                   APIDialect       `json:"api_dialect"`
	Provider                     *ProviderName    `json:"provider"`
	VirtualModelID               *string          `json:"virtual_model_id"` // consumer-facing "{family}/{name}"; NULL for pre-migration rows
	ModelID                      string           `json:"model_id"`
	ModelFamily                  *string          `json:"model_family"`
	RoutingStrategy              *RoutingStrategy `json:"routing_strategy"`
	PromptTokens                 int              `json:"prompt_tokens"`
	CompletionTokens             int              `json:"completion_tokens"`
	TotalTokens                  int              `json:"total_tokens"`
	CacheReadInputTokens         int              `json:"cache_read_input_tokens"`
	CacheCreationInputTokens     int              `json:"cache_creation_input_tokens"`
	EstimatedCostUSD             float64          `json:"estimated_cost_usd"`
	PriceUnknown                 bool             `json:"price_unknown"`
	EffectiveInputCostPer1M      float64          `json:"effective_input_cost_per_1m"`
	EffectiveOutputCostPer1M     float64          `json:"effective_output_cost_per_1m"`
	EffectiveCacheReadCostPer1M  float64          `json:"effective_cache_read_cost_per_1m"`
	EffectiveCacheWriteCostPer1M float64          `json:"effective_cache_write_cost_per_1m"`
	Status                       UsageStatus      `json:"status"`
	RetryCount                   int              `json:"retry_count"`
	ErrorCode                    *string          `json:"error_code"`
	FailureDetails               *FailureDetails  `json:"failure_details"`
	LatencyMS                    int              `json:"latency_ms"`
	CreatedAt                    time.Time        `json:"created_at"`
}

// FailureDetails holds structured provider error context for non-success
// inference requests. Stored as JSONB; populated only for provider_error status
// records. All fields are optional — zero-value providers or non-HTTP errors
// will leave some fields nil.
type FailureDetails struct {
	ProviderHTTPStatus   *int    `json:"provider_http_status,omitempty"`
	ProviderErrorType    *string `json:"provider_error_type,omitempty"`
	ProviderErrorMessage *string `json:"provider_error_message,omitempty"`
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

// TokenUsageStat holds aggregated usage metrics for a single PAT.
type TokenUsageStat struct {
	TokenID      string `json:"token_id"`
	RequestCount int64  `json:"request_count"`
	LastUsed     string `json:"last_used"` // RFC3339
}

// ─── Analytics ────────────────────────────────────────────────────────────────

// AnalyticsFilter controls what the aggregation query returns.
// PrincipalID is set for the personal path (GET /v1/analytics/me) and nil
// for the tenant-wide path (GET /v1/analytics).
type AnalyticsFilter struct {
	TenantID       string
	PrincipalID    *string // nil → tenant-wide
	TokenID        *string // optional PAT filter (personal path only)
	VirtualModelID *string // optional virtual model filter, e.g. "anthropic/claude-sonnet-4-6" (tenant-wide path only)
	Days           int     // lookback window: 7, 30, or 90
	Granularity    string  // "daily", "weekly", or "monthly"
}

// AnalyticsKPIs holds the aggregated KPI values for the full query window.
type AnalyticsKPIs struct {
	TotalRequests       int64   `json:"total_requests"`
	TotalTokens         int64   `json:"total_tokens"`
	PromptTokens        int64   `json:"prompt_tokens"`
	CompletionTokens    int64   `json:"completion_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	AvgLatencyMS        float64 `json:"avg_latency_ms"`
	P95LatencyMS        float64 `json:"p95_latency_ms"`
	P99LatencyMS        float64 `json:"p99_latency_ms"`
	ErrorRate           float64 `json:"error_rate"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
}

// AnalyticsBucket is a single time-series data point for the chosen granularity.
type AnalyticsBucket struct {
	Date                string  `json:"date"`
	Requests            int64   `json:"requests"`
	SuccessfulRequests  int64   `json:"successful_requests"`
	FailedRequests      int64   `json:"failed_requests"`
	PromptTokens        int64   `json:"prompt_tokens"`
	CompletionTokens    int64   `json:"completion_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	P50LatencyMS        float64 `json:"p50_latency_ms"`
	P95LatencyMS        float64 `json:"p95_latency_ms"`
	P99LatencyMS        float64 `json:"p99_latency_ms"`
}

// AnalyticsCostByModel is one row of the cost-by-model breakdown.
type AnalyticsCostByModel struct {
	ModelID          string  `json:"model_id"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// AnalyticsModelTokenBucket is one data point in the per-model token time-series.
type AnalyticsModelTokenBucket struct {
	Date        string `json:"date"`
	ModelID     string `json:"model_id"`
	TotalTokens int64  `json:"total_tokens"`
}

// AnalyticsResponse is the payload returned by GET /v1/analytics and GET /v1/analytics/me.
type AnalyticsResponse struct {
	Scope         string                      `json:"scope"`
	PeriodDays    int                         `json:"period_days"`
	Granularity   string                      `json:"granularity"`
	KPIs          AnalyticsKPIs               `json:"kpis"`
	Buckets       []AnalyticsBucket           `json:"buckets"`
	CostByModel   []AnalyticsCostByModel      `json:"cost_by_model"`
	TokensByModel []AnalyticsModelTokenBucket `json:"tokens_by_model"`
}

// ─── Principal analytics ──────────────────────────────────────────────────────

// PrincipalUsageSummary holds aggregated usage metrics for a single principal
// over the analytics window. Returned by GET /v1/analytics/principals.
type PrincipalUsageSummary struct {
	PrincipalID         string  `json:"principal_id"`
	PrincipalType       string  `json:"principal_type"`
	RequestCount        int64   `json:"request_count"`
	TotalTokens         int64   `json:"total_tokens"`
	PromptTokens        int64   `json:"prompt_tokens"`
	CompletionTokens    int64   `json:"completion_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	ErrorRate           float64 `json:"error_rate"`
	AvgLatencyMS        float64 `json:"avg_latency_ms"`
	LastActiveAt        string  `json:"last_active_at"` // RFC3339
}

// AnalyticsPrincipalsFilter controls the principal usage aggregation query.
type AnalyticsPrincipalsFilter struct {
	TenantID string
	Days     int // lookback window: 7, 30, or 90
	Page     int // 1-based page number
	Limit    int // page size (default 20, max 100)
}

// AnalyticsPrincipalsResponse is the payload returned by GET /v1/analytics/principals.
type AnalyticsPrincipalsResponse struct {
	Data  []PrincipalUsageSummary `json:"data"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
	Limit int                     `json:"limit"`
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
	ErrModelPricingNotFound  = errors.New("model pricing not found")
	ErrBudgetExceeded        = errors.New("budget exceeded")
)

// QuotaStatus is returned by the personal quota endpoint and the GetPersonalQuota
// service method. HasLimit indicates whether a spend limit is configured for the
// principal (via individual quota or group membership) as reported by OPA.
type QuotaStatus struct {
	MonthlyBudgetUSD  float64 `json:"monthly_budget_usd"`
	CurrentSpendUSD   float64 `json:"current_spend_usd"`
	SpendLimitReached bool    `json:"spend_limit_reached"`
	HasLimit          bool    `json:"has_limit"`
}
