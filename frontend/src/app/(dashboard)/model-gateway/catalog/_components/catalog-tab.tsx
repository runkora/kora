'use client';

import { useState, useCallback, useMemo } from 'react';
import {
  Alert,
  Badge,
  Grid,
  Heading,
  Stack,
  StatCard,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
  Icon,
} from '@runkora/hades';
import type {
  VirtualModel as ApiVirtualModel,
  ProviderModel as ApiProviderModel,
  ProviderStatusWire,
} from '../../_services/catalog-server';
import type { VirtualModel, Provider } from '../_types/catalog';
import { toVirtualModel, toProviderModel } from '../_utils/catalog-api';
import { VirtualModelsSection } from './virtual-models-section';
import { ProviderCatalogSection } from './provider-catalog-section';
import { ProvidersSection } from './providers-section';
import { EditorPanel } from './editor-panel';
import { SyncStatusBanner } from './sync-status-banner';
import type { SyncSummary } from './sync-status-banner';

interface CatalogTabProps {
  initialVirtualModels: ApiVirtualModel[];
  initialProviderModels: ApiProviderModel[];
  initialProviders: ProviderStatusWire[];
  providersError?: string | null;
  initialError?: string | null;
  syncSummary: SyncSummary;
}

/** Enabled against disabled for a stat card ring, so a half-switched-off catalog shows at a glance. */
function enabledSplit(enabled: number, total: number) {
  return [
    { label: 'Enabled', value: enabled },
    { label: 'Disabled', value: total - enabled },
  ];
}

export function CatalogTab({
  initialVirtualModels,
  initialProviderModels,
  initialProviders,
  providersError,
  initialError,
  syncSummary,
}: CatalogTabProps) {
  const [selectedModel, setSelectedModel] = useState<VirtualModel | null>(null);
  const [virtualModels, setVirtualModels] = useState<VirtualModel[]>(
    initialVirtualModels.map(toVirtualModel),
  );
  const providerModels = useMemo(
    () => initialProviderModels.map(toProviderModel),
    [initialProviderModels],
  );
  // Routes may target any registered provider, including one whose discovery failed.
  const routeProviders = useMemo(
    () =>
      Array.from(
        new Set([...initialProviders.map((p) => p.name), ...providerModels.map((m) => m.provider)]),
      ).sort() as Provider[],
    [initialProviders, providerModels],
  );

  const handleModelSaved = useCallback((saved: VirtualModel) => {
    setVirtualModels((prev) => {
      const idx = prev.findIndex((m) => m.id === saved.id);
      if (idx >= 0) {
        const updated = [...prev];
        updated[idx] = saved;
        return updated;
      }
      return [...prev, saved];
    });
  }, []);

  const handleModelDeleted = useCallback((id: string) => {
    setVirtualModels((prev) => prev.filter((m) => m.id !== id));
  }, []);

  const enabledVMs = virtualModels.filter((m) => m.enabled).length;
  const enabledPMs = providerModels.filter((m) => m.enabled).length;
  const readyProviders = initialProviders.filter((p) => p.status === 'ready').length;

  return (
    <Stack gap="comfortable">
      <Stack gap="snug">
        <Stack direction="horizontal" align="center" gap="snug">
          <Heading variant="title">Model Catalog</Heading>
          <Badge variant="outline" size="sm">
            Platform Admin
          </Badge>
        </Stack>
        <Text size="supporting" color="muted">
          Manage virtual models, routing rules, and provider access. Changes take effect immediately
          with no gateway restart required.
        </Text>
        {initialError && (
          <Alert variant="error">
            <Icon name="alert-circle" size="2xs" color="status-error" />
            <Text size="caption" color="muted">
              Failed to load catalog: {initialError}
            </Text>
          </Alert>
        )}
        <SyncStatusBanner sync={syncSummary} />
      </Stack>

      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        <StatCard
          icon={<Icon name="layers" size="xs" />}
          label="Virtual Models"
          value={String(virtualModels.length)}
          breakdown={enabledSplit(enabledVMs, virtualModels.length)}
        />
        <StatCard
          icon={<Icon name="circle-dot" size="xs" />}
          label="Provider Models"
          value={String(providerModels.length)}
          breakdown={enabledSplit(enabledPMs, providerModels.length)}
        />
        <StatCard
          icon={<Icon name="network" size="xs" />}
          label="Providers"
          value={initialProviders.length > 0 ? String(initialProviders.length) : '—'}
          detail={
            providersError
              ? 'unavailable'
              : initialProviders.length > 0
                ? `${readyProviders} ready`
                : 'none registered'
          }
        />
        <StatCard
          icon={<Icon name="shield-check" size="xs" />}
          label="Policy Scopes"
          value={String(virtualModels.length)}
          detail="auto-registered"
        />
      </Grid>

      <Tabs defaultValue="virtual">
        <TabsList>
          <TabsTrigger value="virtual">
            Virtual Models
            <Badge variant="secondary" size="sm">
              {virtualModels.length}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value="provider">
            Provider Catalog
            <Badge variant="secondary" size="sm">
              {providerModels.length}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value="providers">
            Providers
            <Badge variant="secondary" size="sm">
              {initialProviders.length}
            </Badge>
          </TabsTrigger>
        </TabsList>
        <TabsContent value="virtual">
          <VirtualModelsSection models={virtualModels} onSelect={setSelectedModel} />
        </TabsContent>
        <TabsContent value="provider">
          <ProviderCatalogSection models={providerModels} />
        </TabsContent>
        <TabsContent value="providers">
          <ProvidersSection providers={initialProviders} error={providersError} />
        </TabsContent>
      </Tabs>

      {selectedModel && (
        <EditorPanel
          model={selectedModel}
          onClose={() => setSelectedModel(null)}
          onSaved={handleModelSaved}
          onDeleted={handleModelDeleted}
          providerModels={providerModels}
          providers={routeProviders}
        />
      )}
    </Stack>
  );
}
