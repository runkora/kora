import type { Meta, StoryObj } from '@storybook/react';
import { dashboardPage, sessionRoutes, ALL_NAV_SCOPES } from '@orbit/storybook/dashboard-frame';
import CatalogLayout from '../layout';
import { CatalogTab } from './catalog-tab';
import {
  PRICING_ENTRIES,
  PROVIDER_MODELS,
  VIRTUAL_MODELS,
} from '../../_components/model-mock-data';

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
