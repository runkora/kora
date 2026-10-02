import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage, sessionRoutes, ALL_NAV_SCOPES } from '@orbit/storybook/dashboard-frame';
import CatalogLayout from '../layout';
import type { ProviderStatusWire } from '../../_services/catalog-server';
import { CatalogTab } from './catalog-tab';
import {
  PRICING_ENTRIES,
  PROVIDER_MODELS,
  VIRTUAL_MODELS,
} from '../../_components/model-mock-data';

/** Two registered providers, one healthy and one failing discovery. */
const PROVIDERS: ProviderStatusWire[] = [
  {
    name: 'bedrock:prod',
    type: 'bedrock',
    display_name: 'AWS Bedrock (prod)',
    resource: 'prod',
    location: 'us-east-1',
    credential_mode: 'assume-role',
    status: 'ready',
    model_count: 12,
    last_discovered_at: '2026-10-01T02:00:00Z',
  },
  {
    name: 'compatible:openrouter',
    type: 'compatible',
    resource: 'openrouter',
    location: 'https://openrouter.ai/api/v1',
    credential_mode: 'secret',
    status: 'error',
    error: 'discovery failed: upstream returned 401',
    model_count: 0,
  },
];

/** The platform admin catalog: virtual models, routing, and provider pricing. */
const meta: Meta<typeof CatalogTab> = {
  title: 'Pages/Models/Catalog',
  component: CatalogTab,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/model-gateway/catalog' } },
  },
  decorators: [
    dashboardPage(
      {
        // The catalog layout guards on this scope, which the default session lacks.
        ...sessionRoutes([...ALL_NAV_SCOPES, 'platform:catalog:write']),
        'GET /api/catalog/pricing': { data: PRICING_ENTRIES },
        'GET /api/catalog/virtual-models': { data: VIRTUAL_MODELS },
        'GET /api/catalog/provider-models': { data: PROVIDER_MODELS },
      },
      CatalogLayout,
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof CatalogTab>;

export const Default: Story = {
  args: {
    initialProviders: PROVIDERS,
    initialVirtualModels: VIRTUAL_MODELS,
    initialProviderModels: PROVIDER_MODELS,
    initialError: null,
    syncSummary: {
      lastSyncedAt: '2026-10-01T02:00:00Z',
      modelsWithPricing: PRICING_ENTRIES.length,
      pricingFetchFailed: false,
      isStale: false,
    },
  },
};

export const Empty: Story = {
  args: {
    initialProviders: [],
    initialVirtualModels: [],
    initialProviderModels: [],
    initialError: null,
    syncSummary: {
      lastSyncedAt: null,
      modelsWithPricing: 0,
      pricingFetchFailed: false,
      isStale: false,
    },
  },
};

export const LoadError: Story = {
  args: {
    initialProviders: [],
    initialVirtualModels: [],
    initialProviderModels: [],
    initialError: 'Failed to reach inference gateway',
    syncSummary: {
      lastSyncedAt: null,
      modelsWithPricing: 0,
      pricingFetchFailed: true,
      isStale: false,
    },
  },
};
