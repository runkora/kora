'use client';

import { useState, useCallback } from 'react';
import { AlertCircle, CircleDot, Layers, Network, ShieldCheck } from 'lucide-react';
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

interface CatalogTabProps {
  initialVirtualModels: ApiVirtualModel[];
  initialProviderModels: ApiProviderModel[];
  initialError?: string | null;
}

export function CatalogTab({
  initialVirtualModels,
  initialProviderModels,
  initialError,
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
            <AlertCircle className="size-3.5 shrink-0 text-status-error" />
            <Text size="caption" color="muted">
              Failed to load catalog: {initialError}
            </Text>
          </Alert>
        )}
      </Stack>

      <Grid cols={{ base: 2, lg: 4 }} gap="snug">
        <StatCard
          icon={<Layers className="size-4" />}
          label="Virtual Models"
          value={virtualModels.length}
          sub={`${enabledVMs} enabled`}
        />
        <StatCard
          icon={<CircleDot className="size-4" />}
          label="Provider Models"
          value={providerModels.length}
          sub={`${enabledPMs} enabled`}
        />
        <StatCard
          icon={<Network className="size-4" />}
          label="Connected Providers"
          value={connectedProviders || '—'}
          sub={connectedProviders > 0 ? 'discovered' : 'none yet'}
        />
        <StatCard
          icon={<ShieldCheck className="size-4" />}
          label="Policy Scopes"
          value={virtualModels.length}
          sub="auto-registered"
        />
      </Grid>

      <Tabs defaultValue="virtual">
        <TabsList>
          <TabsTrigger value="virtual">
            Virtual Models
            <output className="ml-2 rounded-full bg-surface-elevated px-1.5 py-0.5 text-[11px] text-muted">
              {virtualModels.length}
            </output>
          </TabsTrigger>
          <TabsTrigger value="provider">
            Provider Catalog
            <output className="ml-2 rounded-full bg-surface-elevated px-1.5 py-0.5 text-[11px] text-muted">
              {providerModels.length}
            </output>
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
