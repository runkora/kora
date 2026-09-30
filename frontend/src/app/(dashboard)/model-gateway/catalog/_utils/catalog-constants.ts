import type { RoutingStrategy } from '../_types/catalog';
import type { badgeVariants } from '@orbit/terra';
import type { VariantProps } from 'class-variance-authority';

type BadgeVariant = NonNullable<VariantProps<typeof badgeVariants>['variant']>;

// ── Provider labels and variants ──────────────────────────────────────────────

const BASE_PROVIDER_LABELS: Record<string, string> = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
  openai: 'OpenAI-compatible',
  anthropic: 'Anthropic',
};

const BASE_PROVIDER_VARIANTS: Record<string, BadgeVariant> = {
  bedrock: 'provider-bedrock',
  vertexai: 'provider-vertexai',
  azure: 'provider-azure',
};

// providerType returns the part of a provider name before ':'.
export function providerType(provider: string): string {
  return provider.split(':', 1)[0];
}

// providerLabel returns a human-readable label for any provider string.
// Named providers ('bedrock:aus', 'openai:openrouter') get the type label with
// the name in brackets and share the type's badge style.
export function providerLabel(provider: string): string {
  const type = providerType(provider);
  const base = BASE_PROVIDER_LABELS[type];
  if (!base) return provider;
  const name = provider.slice(type.length + 1);
  return name ? `${base} (${name})` : base;
}

export function providerBadgeVariant(provider: string): BadgeVariant {
  return BASE_PROVIDER_VARIANTS[providerType(provider)] ?? 'outline';
}

// Keep legacy record exports for the few callers that use PROVIDER_LABELS[p] ?? p.
// They already have the ?? p fallback so no change needed there, but we keep
// these for compatibility.
export const PROVIDER_LABELS = BASE_PROVIDER_LABELS;
export const PROVIDER_VARIANTS = BASE_PROVIDER_VARIANTS;

// ── Strategy labels and variants ──────────────────────────────────────────────

export const STRATEGY_LABELS = {
  priority: 'Priority / Failover',
  weighted: 'Weighted Split',
  lowest_cost: 'Lowest Cost',
} satisfies Record<RoutingStrategy, string>;

export const STRATEGY_VARIANTS = {
  priority: 'default',
  weighted: 'strategy-weighted',
  lowest_cost: 'healthy',
} as const satisfies Record<RoutingStrategy, BadgeVariant>;
