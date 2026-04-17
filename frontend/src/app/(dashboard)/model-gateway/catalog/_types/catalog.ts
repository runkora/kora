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
}
