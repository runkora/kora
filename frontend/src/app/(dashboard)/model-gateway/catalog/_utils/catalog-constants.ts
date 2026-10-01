import type { RoutingStrategy } from '../_types/catalog';
import type { badgeVariants } from '@orbit/terra';
import type { VariantProps } from 'class-variance-authority';

type BadgeVariant = NonNullable<VariantProps<typeof badgeVariants>['variant']>;

// ── Provider labels and variants ──────────────────────────────────────────────

const BASE_PROVIDER_LABELS: Record<string, string> = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
  compatible: 'Compatible endpoint',
};

const BASE_PROVIDER_VARIANTS: Record<string, BadgeVariant> = {
  bedrock: 'provider-bedrock',
  vertexai: 'provider-vertexai',
  azure: 'provider-azure',
};

// providerType returns the part of a provider name before ':'.
function providerType(provider: string): string {
  return provider.split(':', 1)[0];
}

// providerLabel is the short label a badge shows: the InferenceProvider
// resource name ('bedrock:aus' shows 'aus'). The badge colour carries the type.
export function providerLabel(provider: string): string {
  const type = providerType(provider);
  const name = provider.slice(type.length + 1);
  return name || BASE_PROVIDER_LABELS[type] || provider;
}

// providerTitle names the type and the resource ('AWS Bedrock: aus'), for
// tooltips, selects and filters where the colour is not shown.
export function providerTitle(provider: string): string {
  const type = providerType(provider);
  const base = BASE_PROVIDER_LABELS[type];
  const name = provider.slice(type.length + 1);
  if (!base) return provider;
  return name ? `${base}: ${name}` : base;
}

export function providerBadgeVariant(provider: string): BadgeVariant {
  return BASE_PROVIDER_VARIANTS[providerType(provider)] ?? 'outline';
}

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
