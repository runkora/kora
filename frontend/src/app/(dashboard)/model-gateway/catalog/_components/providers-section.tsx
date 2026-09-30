/** @tier 3 — model-gateway-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo } from 'react';
import { Badge, CodeBlock, Stack, Text } from '@orbit/terra';
import { SmartTable } from '@orbit/components';
import type { DataTableColumn } from '@orbit/components';
import { fmtDate } from '@orbit/utils';
import type { ProviderStatusWire } from '../../_services/catalog-server';
import { ProviderBadge } from './provider-badge';

interface ProvidersSectionProps {
  providers: ProviderStatusWire[];
}

const EXAMPLE = `apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: openai
  openai:
    baseURL: https://openrouter.ai/api/v1
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

export function ProvidersSection({ providers }: ProvidersSectionProps) {
  const columns = useMemo<DataTableColumn<ProviderStatusWire>[]>(
    () => [
      {
        id: 'name',
        header: 'Provider',
        sortable: true,
        sortAccessor: (p) => p.name,
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
        id: 'location',
        header: 'Region or endpoint',
        cell: (p) => (
          <Text size="supporting" color="muted" mono>
            {p.location || '—'}
          </Text>
        ),
      },
      {
        id: 'credentials',
        header: 'Credentials',
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
        id: 'source',
        header: 'Source',
        cell: (p) =>
          p.source === 'resource' ? (
            <Text size="supporting" mono>
              InferenceProvider/{p.resource}
            </Text>
          ) : (
            <Badge variant="degraded" size="sm">
              Env vars (deprecated)
            </Badge>
          ),
      },
      {
        id: 'status',
        header: 'Status',
        sortable: true,
        sortAccessor: (p) => p.status,
        cell: (p) => (
          <Stack gap="tight">
            <Badge variant={p.status === 'ready' ? 'healthy' : 'error'} size="sm" dot>
              {p.status === 'ready' ? 'Ready' : 'Error'}
            </Badge>
            {p.error && (
              <Text size="caption" color="muted">
                {p.error}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'models',
        header: 'Models',
        sortable: true,
        sortAccessor: (p) => p.model_count,
        cell: (p) => <Text size="supporting">{String(p.model_count)}</Text>,
      },
      {
        id: 'discovered',
        header: 'Last discovery',
        defaultVisible: false,
        cell: (p) => (
          <Text size="supporting" color="muted">
            {p.last_discovered_at ? fmtDate(p.last_discovered_at) : '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  if (providers.length === 0) {
    return (
      <Stack gap="snug">
        <Text size="supporting" color="muted">
          No providers are registered. Apply an InferenceProvider resource in the Orbit system
          namespace, with its API key in a Secret beside it. The gateway picks it up without a
          restart.
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
