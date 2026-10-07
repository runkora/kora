/** @tier 3 — model-gateway-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo } from 'react';
import { Alert, CodeBlock, Icon, Stack, Text } from '@orbit/terra';
import { SmartTable, StatusBadge } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { fmtDate } from '@orbit/utils';
import type { ProviderStatusWire } from '../../_services/catalog-server';
import { ProviderBadge } from './provider-badge';

interface ProvidersSectionProps {
  providers: ProviderStatusWire[];
  error?: string | null;
}

const EXAMPLE = `apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: compatible
  compatible:
    baseURL: https://openrouter.ai/api/v1
    apis: [chatCompletions, responses, messages]
    apiKeySecretRef:
      name: openrouter
      key: apiKey`;

function providerMatchesSearch(p: ProviderStatusWire, q: string): boolean {
  return (
    p.name.toLowerCase().includes(q) ||
    (p.display_name ?? '').toLowerCase().includes(q) ||
    (p.location ?? '').toLowerCase().includes(q)
  );
}

export function ProvidersSection({ providers, error }: ProvidersSectionProps) {
  const columns = useMemo<DataTableColumn<ProviderStatusWire>[]>(
    () => [
      {
        id: 'name',
        header: 'Provider',
        sortable: true,
        sortAccessor: (p) => p.name,
        gridWidth: 'minmax(0, 1.4fr)',
        minWidth: '10rem',
        cell: (p) => (
          <Stack gap="tight">
            <ProviderBadge provider={p.name} />
            {p.display_name && (
              <Text size="caption" color="muted">
                {p.display_name}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        sortable: true,
        sortAccessor: (p) => p.status,
        wrap: true,
        gridWidth: '11rem',
        minWidth: '8rem',
        cell: (p) => (
          // align="start" keeps the badge its natural width; a stretched badge fills the column.
          <Stack gap="tight" align="start">
            <StatusBadge
              status={p.status === 'ready' ? 'healthy' : 'error'}
              label={p.status === 'ready' ? 'Ready' : 'Error'}
            />
            {p.error && (
              <Text size="caption" color="muted">
                {p.error}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'location',
        header: 'Region or endpoint',
        gridWidth: 'minmax(0, 2.4fr)',
        minWidth: '12rem',
        cell: (p) => (
          <Text size="supporting" color="muted" mono>
            {p.location || '—'}
          </Text>
        ),
      },
      {
        id: 'credentials',
        header: 'Credentials',
        gridWidth: 'minmax(0, 1.1fr)',
        minWidth: '7rem',
        cell: (p) => (
          <Stack gap="tight">
            <Text size="supporting">{p.credential_mode}</Text>
            {p.assume_role_arn && (
              <Text size="caption" color="muted" mono>
                assumes {p.assume_role_arn}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'resource',
        header: 'Resource',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '14rem',
        cell: (p) => (
          <Text size="supporting" mono>
            InferenceProvider/{p.resource}
          </Text>
        ),
      },
      {
        id: 'models',
        header: 'Models',
        align: 'right',
        sortable: true,
        sortAccessor: (p) => p.model_count,
        gridWidth: '5rem',
        cell: (p) => (
          <Text mono tabular size="supporting">
            {String(p.model_count)}
          </Text>
        ),
      },
      {
        id: 'discovered',
        header: 'Last discovery',
        defaultVisible: false,
        gridWidth: '9rem',
        cell: (p) => (
          <Text size="supporting" color="muted">
            {p.last_discovered_at ? fmtDate(p.last_discovered_at) : '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  if (error) {
    return (
      <Alert variant="error">
        <Icon name="alert-circle" size="2xs" color="status-error" />
        <Text size="caption" color="muted">
          Failed to load providers: {error}
        </Text>
      </Alert>
    );
  }

  if (providers.length === 0) {
    return (
      <Stack gap="snug">
        <Text size="supporting" color="muted">
          No providers are registered. Apply an InferenceProvider resource in the kora system
          namespace, with its API key in a Secret labelled orbit.io/inference-provider=true.
        </Text>
        <CodeBlock text={EXAMPLE} filename="openrouter.yaml" />
      </Stack>
    );
  }

  return (
    <Stack gap="snug">
      <Text size="supporting" color="muted">
        Providers are registered with InferenceProvider resources. Credentials stay in the gateway
        and are never shown here.
      </Text>
      <SmartTable
        tableId="catalogRegisteredProviders"
        data={providers}
        columns={columns}
        getRowKey={(p) => p.name}
        noun="providers"
        searchPlaceholder="Search providers…"
        searchFn={providerMatchesSearch}
        defaultSortId="name"
        defaultSortDir="asc"
        emptyMessage="No providers registered."
        filteredEmptyMessage="No providers match your search."
      />
    </Stack>
  );
}
