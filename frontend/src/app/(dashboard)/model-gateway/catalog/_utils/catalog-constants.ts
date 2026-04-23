import type { Provider, RoutingStrategy } from '../_types/catalog';
import type { badgeVariants } from '@orbit/terra';
import type { VariantProps } from 'class-variance-authority';

type BadgeVariant = NonNullable<VariantProps<typeof badgeVariants>['variant']>;

// ── Provider labels and variants ──────────────────────────────────────────────
//
// Each provider maps to a typed Badge `variant`. The variants resolve to
// `--color-provider-*` design tokens defined in tokens.css — no arbitrary hex
// values anywhere in the component tree.
//
// satisfies enforces exhaustive coverage: adding a new Provider type value will
// cause a compile-time error here until PROVIDER_LABELS / PROVIDER_VARIANTS is updated.

export const PROVIDER_LABELS = {
  bedrock: 'AWS Bedrock',
  vertexai: 'Vertex AI',
  azure: 'Azure OpenAI',
} satisfies Record<Provider, string>;

export const PROVIDER_VARIANTS = {
  bedrock: 'provider-bedrock',
  vertexai: 'provider-vertexai',
  azure: 'provider-azure',
} as const satisfies Record<Provider, BadgeVariant>;

// ── Strategy labels and variants ──────────────────────────────────────────────

export const STRATEGY_LABELS = {
  priority: 'Priority / Failover',
  weighted: 'Weighted Split',
  lowest_cost: 'Lowest Cost',
} satisfies Record<RoutingStrategy, string>;

export const STRATEGY_VARIANTS = {
  priority: 'default', // accent tint — highest-priority selection
  weighted: 'strategy-weighted', // purple — resolves to --color-strategy-weighted
  lowest_cost: 'healthy', // green — resolves to --color-status-healthy
} as const satisfies Record<RoutingStrategy, BadgeVariant>;
