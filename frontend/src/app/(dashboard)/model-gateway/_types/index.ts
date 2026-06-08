// ── Feature-local types ───────────────────────────────────────────────────────
//
// Single import point for all types used across the model-gateway feature.
// Components import from here, never directly from `_data/mock`.

import type {
  ModelObject,
  ModelsListResponse,
  Modality,
  APIDialect,
  ModelCapability,
} from '@orbit/shared/api';

export type { ModelObject, ModelsListResponse, Modality, APIDialect, ModelCapability };

// ── Inference Usage ───────────────────────────────────────────────────────────
//
// Mirrors the backend InferenceUsageRecord struct (inferencegw/models.go).
// Mirrors UsageStatus constants in backend/internal/inferencegw/models.go.
export type InferenceUsageStatus =
  | 'success'
  | 'provider_error'
  | 'policy_denied'
  | 'auth_error'
  | 'timeout'
  | 'routing_failed'
  | 'canceled';

// Mirrors FailureDetails in backend/internal/inferencegw/models.go.
// Populated only for provider_error records.
export interface FailureDetails {
  provider_http_status?: number;
  provider_error_type?: string;
  provider_error_message?: string;
}

// Mirrors InferenceUsageRecord in backend/internal/inferencegw/models.go.
// Field names are snake_case matching the Go json tags.
export interface InferenceUsageRecord {
  id: string;
  tenant_id: string;
  principal_id: string;
  principal_type: string;
  token_id: string | null;
  api_dialect: string;
  provider: string | null;
  virtual_model_id: string | null; // consumer-facing "{family}/{name}"; null for pre-migration rows
  model_id: string;
  model_family: string | null;
  routing_strategy: string | null;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cache_read_input_tokens: number;
  cache_creation_input_tokens: number;
  estimated_cost_usd: number;
  status: InferenceUsageStatus;
  retry_count: number;
  error_code: string | null;
  failure_details: FailureDetails | null;
  latency_ms: number;
  created_at: string; // RFC3339
}

// Mirrors ListUsageResponse in backend/internal/inferencegw/handlers_usage.go.
export interface ListUsageResponse {
  data: InferenceUsageRecord[];
  has_next_page: boolean;
  next_cursor: string | null;
  limit: number;
}

// ── Analytics ─────────────────────────────────────────────────────────────────
//
// Mirrors AnalyticsFilter, AnalyticsKPIs, AnalyticsBucket, AnalyticsCostByModel,
// and AnalyticsResponse in backend/internal/inferencegw/models.go.

export interface AnalyticsKPIs {
  total_requests: number;
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  estimated_cost_usd: number;
  avg_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  error_rate: number;
  cache_hit_rate: number;
}

export interface AnalyticsBucket {
  date: string; // "YYYY-MM-DD"
  requests: number;
  successful_requests: number;
  failed_requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  estimated_cost_usd: number;
  p50_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
}

export interface AnalyticsCostByModel {
  model_id: string;
  estimated_cost_usd: number;
}

export interface AnalyticsModelTokenBucket {
  date: string; // "YYYY-MM-DD"
  model_id: string;
  total_tokens: number;
}

export interface AnalyticsResponse {
  scope: 'personal' | 'tenant';
  period_days: number;
  granularity: 'daily' | 'weekly' | 'monthly';
  kpis: AnalyticsKPIs;
  buckets: AnalyticsBucket[];
  cost_by_model: AnalyticsCostByModel[];
  tokens_by_model: AnalyticsModelTokenBucket[];
}

export type AnalyticsDays = 7 | 30 | 90;
export type AnalyticsGranularity = 'daily' | 'weekly' | 'monthly';

// ── Principal analytics ───────────────────────────────────────────────────────

// Mirrors PrincipalUsageSummary in backend/internal/inferencegw/models.go.
export interface PrincipalUsageSummary {
  principal_id: string;
  principal_type: string;
  request_count: number;
  total_tokens: number;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  estimated_cost_usd: number;
  error_rate: number;
  avg_latency_ms: number;
  last_active_at: string; // RFC3339
}

// Enriched by the BFF with identity service data.
export interface PrincipalUsageRow extends PrincipalUsageSummary {
  email: string | null;
  display_name: string | null;
}

export interface AnalyticsPrincipalsPageResponse {
  data: PrincipalUsageRow[];
  total: number;
  page: number;
  limit: number;
}

// ── Quota ─────────────────────────────────────────────────────────────────────
//
// Mirrors QuotaStatus in backend/internal/inferencegw/models.go.

export interface QuotaStatus {
  monthly_budget_usd: number;
  current_spend_usd: number;
  spend_limit_reached: boolean;
  has_limit: boolean;
}

// ── Value-type unions ─────────────────────────────────────────────────────────

export type ModelStatus = 'available' | 'unavailable';

// ── ModelRow ──────────────────────────────────────────────────────────────────
//
// Display-layer shape derived from the backend ModelObject. All UI components
// work with ModelRow; the raw ModelObject is only used in toModelRow and the
// API service layer.

export interface ModelRow {
  // ── Identity (always populated from the API) ─────────────────────────────
  id: string;
  name: string; // display_name ?? id
  owned_by: string; // hosting provider: "bedrock" | "vertexai" | "azure"
  family: string; // vendor/model-family: "anthropic" | "google" | "meta" | …
  aliases: string[];

  // ── Capabilities (always populated from the API) ──────────────────────────
  input_modalities: Modality[];
  output_modalities: Modality[];
  dialects: APIDialect[];
  capabilities: ModelCapability[];

  // ── Availability ──────────────────────────────────────────────────────────
  status: ModelStatus;

  // ── UI state ──────────────────────────────────────────────────────────────
  favourite: boolean;
}

// ── Transformer ───────────────────────────────────────────────────────────────

export function toModelRow(m: ModelObject): ModelRow {
  return {
    id: m.id,
    name: m.display_name || m.id,
    owned_by: m.owned_by,
    family: m.family ?? '',
    aliases: m.aliases ?? [],
    input_modalities: m.input_modalities ?? [],
    output_modalities: m.output_modalities ?? [],
    dialects: m.dialects ?? [],
    capabilities: m.capabilities ?? [],
    status: m.status ?? 'available',
    favourite: false,
  };
}
