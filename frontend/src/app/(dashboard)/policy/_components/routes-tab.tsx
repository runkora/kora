'use client';

import { useMemo } from 'react';
import { DataTable, Heading, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { Route } from 'lucide-react';
import type { RouteRegistration } from '../_types';
import { ScopeChip } from './scope-chip';
import { EmptyState } from './empty-state';
import { ErrorBanner } from './error-banner';

interface RoutesTabProps {
  initialRegistrations: RouteRegistration[];
  initialError: string | null;
}

interface FlatRoute {
  service: string;
  tenantId: string;
  pathPrefix: string;
  method: string;
  requiredScope: string;
}

const METHOD_COLORS: Record<string, string> = {
  GET: 'text-status-healthy',
  POST: 'text-accent',
  PUT: 'text-status-degraded',
  PATCH: 'text-status-degraded',
  DELETE: 'text-status-error',
};

export function RoutesTab({ initialRegistrations, initialError }: RoutesTabProps) {
  const flatRoutes = useMemo(() => {
    const rows: FlatRoute[] = [];
    for (const reg of initialRegistrations) {
      for (const route of reg.routes) {
        rows.push({
          service: reg.service,
          tenantId: reg.tenant_id || 'global',
          pathPrefix: route.path_prefix,
          method: route.method,
          requiredScope: route.required_scope,
        });
      }
    }
    return rows;
  }, [initialRegistrations]);

  const serviceOptions = useMemo(
    () =>
      Array.from(new Set(flatRoutes.map((r) => r.service)))
        .sort()
        .map((s) => ({ value: s, label: s })),
    [flatRoutes],
  );

  const methodOptions = useMemo(
    () =>
      Array.from(new Set(flatRoutes.map((r) => r.method)))
        .sort()
        .map((m) => ({ value: m, label: m })),
    [flatRoutes],
  );

  const columns = useMemo<DataTableColumn<FlatRoute>[]>(
    () => [
      {
        id: 'service',
        header: 'Service',
        sortable: true,
        sortAccessor: (r) => r.service,
        filter: {
          mode: 'client',
          options: serviceOptions,
          filterFn: (r, selected) => selected.has(r.service),
        },
        cell: (r) => (
          <span className="inline-flex items-center rounded-md border border-border bg-surface-elevated px-2 py-0.5 text-xs font-medium text-foreground">
            {r.service}
          </span>
        ),
      },
      {
        id: 'method',
        header: 'Method',
        sortable: true,
        sortAccessor: (r) => r.method,
        filter: {
          mode: 'client',
          options: methodOptions,
          filterFn: (r, selected) => selected.has(r.method),
        },
        cell: (r) => (
          <span className={`font-mono text-xs ${METHOD_COLORS[r.method] ?? 'text-muted'}`}>
            {r.method}
          </span>
        ),
      },
      {
        id: 'pathPrefix',
        header: 'Path Prefix',
        sortable: true,
        sortAccessor: (r) => r.pathPrefix,
        cell: (r) => <span className="font-mono text-xs text-subtle">{r.pathPrefix}</span>,
      },
      {
        id: 'requiredScope',
        header: 'Required Scope',
        sortable: true,
        sortAccessor: (r) => r.requiredScope,
        cell: (r) => <ScopeChip scope={r.requiredScope} />,
      },
    ],
    [serviceOptions, methodOptions],
  );

  if (flatRoutes.length === 0 && !initialError) {
    return (
      <Stack gap="comfortable">
        <Stack gap="none">
          <Heading variant="title">Routes</Heading>
          <Text size="supporting" color="muted">
            Registered route→scope mappings across all services.
          </Text>
        </Stack>
        <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
          <Text size="caption" color="muted">
            Services register their route→scope mappings at startup. This is the source of truth for
            what scope is required to call each endpoint — compiled into OPA bundles and enforced by
            Envoy sidecars.
          </Text>
        </section>
        <EmptyState
          icon={<Route className="size-5" />}
          title="No routes registered"
          description="Routes are registered by services at startup. Once a service declares its route→scope mappings, they will appear here."
        />
      </Stack>
    );
  }

  return (
    <Stack gap="comfortable">
      <Stack gap="none">
        <Heading variant="title">Routes</Heading>
        <Text size="supporting" color="muted">
          Registered route→scope mappings across all services.
        </Text>
      </Stack>

      <section className="rounded-lg border border-accent/20 bg-accent/5 px-4 py-3">
        <Text size="caption" color="muted">
          Services register their route→scope mappings at startup. This is the source of truth for
          what scope is required to call each endpoint — compiled into OPA bundles and enforced by
          Envoy sidecars.
        </Text>
      </section>

      {initialError && <ErrorBanner message={initialError} />}

      <DataTable
        data={flatRoutes}
        columns={columns}
        getRowKey={(r) => `${r.service}-${r.method}-${r.pathPrefix}-${r.requiredScope}`}
        noun="routes"
        searchPlaceholder="Search routes…"
        searchFn={(r, q) =>
          r.pathPrefix.toLowerCase().includes(q) ||
          r.requiredScope.toLowerCase().includes(q) ||
          r.service.toLowerCase().includes(q)
        }
        defaultSortId="service"
        defaultSortDir="asc"
        emptyMessage="No routes registered."
        filteredEmptyMessage="No routes match your filters."
      />
    </Stack>
  );
}
