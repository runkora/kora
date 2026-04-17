import type { Provider, RoutingStrategy } from '../_types/catalog';

// ── Provider labels and colours ───────────────────────────────────────────────
//
// Provider brand hex values (for reference — Tailwind class strings must be
// literal so the JIT compiler can scan them):
//   AWS Orange:    #ff9900
//   Google Blue:   #4285f4
//   Azure Blue:    #0078d4

// satisfies enforces exhaustive coverage: adding a new Provider type value will
// cause a compile-time error here until PROVIDER_LABELS is updated.
export const PROVIDER_LABELS = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
} satisfies Record<Provider, string>;

export const PROVIDER_COLORS = {
  bedrock: 'bg-[#ff9900]/15 text-[#ff9900]', // AWS Orange
  vertexai: 'bg-[#4285f4]/15 text-[#4285f4]', // Google Blue
  azure: 'bg-[#0078d4]/15 text-[#0078d4]', // Azure Blue
} satisfies Record<Provider, string>;

// ── Strategy labels and colours ───────────────────────────────────────────────

export const STRATEGY_LABELS = {
  priority: 'Priority / Failover',
  weighted: 'Weighted Split',
  lowest_cost: 'Lowest Cost',
} satisfies Record<RoutingStrategy, string>;

export const STRATEGY_COLORS = {
  priority: 'bg-accent/15 text-accent',
  weighted: 'bg-[#a855f7]/15 text-[#a855f7]',
  lowest_cost: 'bg-status-healthy/15 text-status-healthy',
} satisfies Record<RoutingStrategy, string>;
