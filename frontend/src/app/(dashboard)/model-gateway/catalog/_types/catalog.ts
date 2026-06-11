// ── Domain types ──────────────────────────────────────────────────────────────
//
// Provider must stay in sync with the Go ProviderName enum and the DB CHECK
// constraint (provider IN ('bedrock', 'vertexai', 'azure')).
// When a new provider is added, update this type, PROVIDER_LABELS/PROVIDER_COLORS
// in catalog-constants.ts, and the backend simultaneously.
export type Provider = 'bedrock' | 'vertexai' | 'azure';

export type RoutingStrategy = 'priority' | 'weighted' | 'lowest_cost';

export interface RouteTarget {
  id: string;
  provider: Provider;
  modelId: string;
  priority: number;
  weight: number;
  inputCostPer1M: number;
  outputCostPer1M: number;
  enabled: boolean;
}

export interface VirtualModel {
  id: string;
  family: string;
  name: string;
  displayName: string;
  description: string;
  strategy: RoutingStrategy;
  routes: RouteTarget[];
  enabled: boolean;
}

export interface ProviderModel {
  provider: Provider;
  modelId: string;
  displayName: string;
  family: string;
  capabilities: string[];
  aliases: string[];
  inputModalities: string[];
  outputModalities: string[];
  dialects: string[];
  enabled: boolean;
  pricing?: PricingEntry;
}

// Mirrors pricingEntryJSON in backend/internal/inferencegw/handlers_catalog.go.
export interface PricingEntry {
  provider: string;
  model_id: string;
  synced_input_cost_per_1m: number;
  synced_output_cost_per_1m: number;
  synced_cache_read_cost_per_1m: number;
  synced_cache_write_cost_per_1m: number;
  override_input_cost_per_1m: number | null;
  override_output_cost_per_1m: number | null;
  override_cache_read_cost_per_1m: number | null;
  override_cache_write_cost_per_1m: number | null;
  effective_input_cost_per_1m: number;
  effective_output_cost_per_1m: number;
  has_override: boolean;
  last_synced_at: string; // RFC3339
}
