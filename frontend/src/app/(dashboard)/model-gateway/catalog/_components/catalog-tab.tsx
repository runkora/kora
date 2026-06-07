'use client';

import { useState, useCallback } from 'react';
import {
  Alert,
  Badge,
  Grid,
  Heading,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
  Icon,
} from '@orbit/terra';
import type {
  VirtualModel as ApiVirtualModel,
  ProviderModel as ApiProviderModel,
} from '../../_services/catalog-server';
import type { VirtualModel } from '../_types/catalog';
import { toVirtualModel, toProviderModel } from '../_utils/catalog-api';
import { StatCard } from './stat-card';
import { VirtualModelsSection } from './virtual-models-section';
import { ProviderCatalogSection } from './provider-catalog-section';
import { EditorPanel } from './editor-panel';
import { SyncStatusBanner } from './sync-status-banner';
import type { SyncSummary } from './sync-status-banner';

interface CatalogTabProps {
  initialVirtualModels: ApiVirtualModel[];
  initialProviderModels: ApiProviderModel[];
  initialError?: string | null;
  syncSummary: SyncSummary;
}

export function CatalogTab({
  initialVirtualModels,
  initialProviderModels,
  initialError,
  syncSummary,
}: CatalogTabProps) {
  const [selectedModel, setSelectedModel] = useState<VirtualModel | null>(null);
  const [virtualModels, setVirtualModels] = useState<VirtualModel[]>(
    initialVirtualModels.map(toVirtualModel),
  );
  const providerModels = initialProviderModels.map(toProviderModel);

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

  const enabledVMs = virtualModels.filter((m) => m.enabled).length;
  const enabledPMs = providerModels.filter((m) => m.enabled).length;
  const connectedProviders = new Set(providerModels.map((m) => m.provider)).size;

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
          — no gateway restart required.
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
          icon={<Icon name="layers" />}
          label="Virtual Models"
          value={virtualModels.length}
          sub={`${enabledVMs} enabled`}
        />
        <StatCard
          icon={<Icon name="circle-dot" />}
          label="Provider Models"
          value={providerModels.length}
          sub={`${enabledPMs} enabled`}
        />
        <StatCard
          icon={<Icon name="network" />}
          label="Connected Providers"
          value={connectedProviders || '—'}
          sub={connectedProviders > 0 ? 'discovered' : 'none yet'}
        />
        <StatCard
          icon={<Icon name="shield-check" />}
          label="Policy Scopes"
          value={virtualModels.length}
          sub="auto-registered"
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
        </TabsList>
        <TabsContent value="virtual">
          <VirtualModelsSection models={virtualModels} onSelect={setSelectedModel} />
        </TabsContent>
        <TabsContent value="provider">
          <ProviderCatalogSection models={providerModels} />
        </TabsContent>
      </Tabs>

      {selectedModel && (
        <EditorPanel
          model={selectedModel}
          onClose={() => setSelectedModel(null)}
          onSaved={handleModelSaved}
          providerModels={providerModels}
        />
      )}
    </Stack>
  );
}
