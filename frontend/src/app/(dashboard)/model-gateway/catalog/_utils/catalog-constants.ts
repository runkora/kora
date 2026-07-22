import type { RoutingStrategy } from '../_types/catalog';
import type { badgeVariants } from '@orbit/terra';
import type { VariantProps } from 'class-variance-authority';

type BadgeVariant = NonNullable<VariantProps<typeof badgeVariants>['variant']>;

// ── Provider labels and variants ──────────────────────────────────────────────

const BASE_PROVIDER_LABELS: Record<string, string> = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
};

const BASE_PROVIDER_VARIANTS: Record<string, BadgeVariant> = {
  bedrock: 'provider-bedrock',
  vertexai: 'provider-vertexai',
  azure: 'provider-azure',
};

// providerLabel returns a human-readable label for any provider string.
// Bedrock aliases ('bedrock:us', 'bedrock:aus') get a label of
// 'AWS Bedrock (us)' / 'AWS Bedrock (aus)' and share the bedrock badge style.
export function providerLabel(provider: string): string {
  if (BASE_PROVIDER_LABELS[provider]) return BASE_PROVIDER_LABELS[provider];
  if (provider.startsWith('bedrock:')) {
    const alias = provider.slice('bedrock:'.length);
    return `AWS Bedrock (${alias})`;
  }
  return provider;
}

export function providerBadgeVariant(provider: string): BadgeVariant {
  return (
    BASE_PROVIDER_VARIANTS[provider] ??
    (provider.startsWith('bedrock:') ? 'provider-bedrock' : 'outline')
  );
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
