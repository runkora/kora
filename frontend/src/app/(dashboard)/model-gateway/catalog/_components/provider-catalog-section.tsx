'use client';

import { useState, useCallback, useMemo } from 'react';
import { Badge, DataTable, Stack, Text, Toggle } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import type { ProviderModel } from '../_types/catalog';
import { ProviderBadge } from './provider-badge';

const PROVIDER_OPTIONS = [
  { value: 'bedrock', label: 'AWS Bedrock' },
  { value: 'vertexai', label: 'Vertex AI' },
  { value: 'azure', label: 'Azure OpenAI' },
];

const STATUS_OPTIONS = [
  { value: 'enabled', label: 'Enabled' },
  { value: 'disabled', label: 'Disabled' },
];

interface ProviderCatalogSectionProps {
  models: ProviderModel[];
}

function providerModelMatchesSearch(m: ProviderModel, q: string): boolean {
  return (
    m.modelId.toLowerCase().includes(q) ||
    m.displayName.toLowerCase().includes(q) ||
    m.family.toLowerCase().includes(q)
  );
}

function Chips({ values }: { values: string[] }) {
  if (values.length === 0)
    return (
      <Text size="caption" color="muted">
        —
      </Text>
    );
  return (
    <Stack direction="horizontal" wrap gap="tight">
      {values.map((v) => (
        <Badge key={v} variant="outline" size="sm">
          {v}
        </Badge>
      ))}
    </Stack>
  );
}

export function ProviderCatalogSection({ models: initialModels }: ProviderCatalogSectionProps) {
  const [localModels, setLocalModels] = useState(initialModels);

  const toggle = useCallback(async (provider: string, modelId: string, enabled: boolean) => {
    setLocalModels((prev) =>
      prev.map((m) => (m.provider === provider && m.modelId === modelId ? { ...m, enabled } : m)),
    );
    try {
      const res = await fetch(
        `/api/catalog/provider-models/${encodeURIComponent(provider)}/${encodeURIComponent(modelId)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled }),
        },
      );
      if (!res.ok) {
        setLocalModels((prev) =>
          prev.map((m) =>
            m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
          ),
        );
      }
    } catch {
      setLocalModels((prev) =>
        prev.map((m) =>
          m.provider === provider && m.modelId === modelId ? { ...m, enabled: !enabled } : m,
        ),
      );
    }
  }, []);

  const columns = useMemo<DataTableColumn<ProviderModel>[]>(
    () => [
      {
        id: 'modelId',
        header: 'Model ID',
        locked: true,
        sortable: true,
        sortAccessor: (m) => m.modelId,
        cell: (m) => (
          <Stack gap="none">
            <Text size="supporting" weight={m.enabled ? 'emphasis' : 'regular'} mono>
              {m.modelId}
            </Text>
            {m.displayName && (
              <Text size="caption" color="muted">
                {m.displayName}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'provider',
        header: 'Provider',
        sortable: true,
        sortAccessor: (m) => m.provider,
        filter: {
          mode: 'client',
          options: PROVIDER_OPTIONS,
          filterFn: (m, selected) => selected.has(m.provider),
        },
        cell: (m) => <ProviderBadge provider={m.provider} />,
      },
      {
        id: 'family',
        header: 'Family',
        sortable: true,
        sortAccessor: (m) => m.family,
        cell: (m) => (
          <Text size="supporting" color="muted" mono>
            {m.family || '—'}
          </Text>
        ),
      },
      {
        id: 'capabilities',
        header: 'Capabilities',
        cell: (m) => <Chips values={m.capabilities} />,
      },
      {
        id: 'input',
        header: 'Input',
        defaultVisible: false,
        cell: (m) => <Chips values={m.inputModalities} />,
      },
      {
        id: 'output',
        header: 'Output',
        defaultVisible: false,
        cell: (m) => <Chips values={m.outputModalities} />,
      },
      {
        id: 'dialects',
        header: 'Dialects',
        defaultVisible: false,
        cell: (m) => <Chips values={m.dialects} />,
      },
      {
        id: 'enabled',
        header: 'Enabled',
        locked: true,
        filter: {
          mode: 'client',
          options: STATUS_OPTIONS,
          filterFn: (m, selected) => selected.has(m.enabled ? 'enabled' : 'disabled'),
        },
        cell: (m) => (
          <Toggle checked={m.enabled} onChange={(v) => void toggle(m.provider, m.modelId, v)} />
        ),
      },
    ],
    [toggle],
  );

  return (
    <DataTable
      data={localModels}
      columns={columns}
      getRowKey={(m) => `${m.provider}/${m.modelId}`}
      noun="provider models"
      searchPlaceholder="Search models…"
      searchFn={providerModelMatchesSearch}
      defaultSortId="modelId"
      defaultSortDir="asc"
      emptyMessage="No provider models discovered yet."
      filteredEmptyMessage="No provider models match your filters."
    />
  );
}
