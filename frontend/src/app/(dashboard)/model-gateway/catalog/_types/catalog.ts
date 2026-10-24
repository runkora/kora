// ── Domain types ──────────────────────────────────────────────────────────────
//
// Provider is the name of a backend provider: a type, or a type and an
// instance name ('bedrock:aus', 'compatible:openrouter').
type ProviderType = 'bedrock' | 'vertexai' | 'azure' | 'compatible';
export type Provider = ProviderType | `${ProviderType}:${string}`;

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

export interface VirtualModelPricing {
  effective_input_cost_per_1m?: number;
  effective_output_cost_per_1m?: number;
  non_token_only?: boolean;
  // Populated only when non_token_only is true — the selected route's own
  // non-token dimensions. Mirrors ModelObjectPricing's field set.
  image_input_cost?: number;
  image_output_cost?: number;
  request_input_cost?: number;
  request_output_cost?: number;
  second_input_cost?: number;
  second_output_cost?: number;
  audio_input_cost_per_1m?: number;
  audio_output_cost_per_1m?: number;
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
  pricing?: VirtualModelPricing;
  /** Declared in the configuration repository: read-only here. */
  declared?: boolean;
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
  // Non-token dimensions — synced values only. Named synced_* to match the
  // token field convention and leave room for override_*/effective_* counterparts.
  // Absent when zero.
  synced_image_input_cost_per_image?: number;
  synced_image_output_cost_per_image?: number;
  synced_request_input_cost?: number;
  synced_request_output_cost?: number;
  synced_second_input_cost?: number;
  synced_second_output_cost?: number;
  synced_audio_input_cost_per_1m?: number;
  synced_audio_output_cost_per_1m?: number;
}
