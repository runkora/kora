'use client';

/** @tier 3 — connections-scoped. Read-only UI for the /connections list page. */

import { useMemo } from 'react';
import { Badge, DataTable, Heading, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState } from '@orbit/components';
import { adapterLabel } from '../_types';
import type { ConnectionRow } from '../_types';

interface ConnectionsPageProps {
  rows: ConnectionRow[];
  error: string | null;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function connectionMatchesSearch(row: ConnectionRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.name.toLowerCase().includes(lower) ||
    row.adapter.toLowerCase().includes(lower) ||
    adapterLabel(row.adapter).toLowerCase().includes(lower) ||
    row.actions.some((a) => a.toLowerCase().includes(lower)) ||
    row.usedBy.some((u) => u.name.toLowerCase().includes(lower))
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ConnectionsPage({ rows, error }: ConnectionsPageProps) {
  const columns = useMemo<DataTableColumn<ConnectionRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Text mono size="supporting" weight="emphasis">
            {row.name}
          </Text>
        ),
      },
      {
        id: 'adapter',
        header: 'Adapter',
        locked: true,
        sortable: true,
        sortAccessor: (row) => adapterLabel(row.adapter),
        cell: (row) => (
          <Badge variant="secondary" size="sm">
            {adapterLabel(row.adapter)}
          </Badge>
        ),
      },
      {
        id: 'actions',
        header: 'Grantable actions',
        locked: true,
        cell: (row) =>
          row.actions.length === 0 ? (
            <Text size="supporting" color="muted">
              —
            </Text>
          ) : (
            <Stack direction="horizontal" gap="tight" wrap>
              {row.actions.map((action) => (
                <Badge key={action} variant="elevated" size="sm" mono>
                  {action}
                </Badge>
              ))}
            </Stack>
          ),
      },
      {
        id: 'usedBy',
        header: 'Used by',
        locked: true,
        cell: (row) =>
          row.usedBy.length === 0 ? (
            <Text size="supporting" color="muted">
              Not attached
            </Text>
          ) : (
            <Stack direction="horizontal" gap="tight" wrap>
              {row.usedBy.map((usage) => (
                <Badge key={usage.slug} variant="outline" size="sm">
                  {usage.name}
                </Badge>
              ))}
            </Stack>
          ),
      },
    ],
    [],
  );

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack gap="tight">
        <Heading variant="title">Connections</Heading>
        <Text size="supporting" color="muted">
          Connections are defined as Connection custom resources (GitOps) and reconciled by the
          operator. This view is read-only.
        </Text>
      </Stack>

      {error ? (
        <Stack align="center" gap="snug" paddingBlock="comfortable">
          <Text size="supporting" color="muted">
            {error}
          </Text>
        </Stack>
      ) : rows.length === 0 ? (
        <EmptyState
          icon="network"
          title="No connections"
          description="Connections are registered as Connection custom resources via GitOps and reconciled by the operator. None are registered for this tenant yet."
        />
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(row) => row.name}
          noun="connections"
          searchPlaceholder="Search connections…"
          searchFn={connectionMatchesSearch}
          defaultSortId="name"
          defaultSortDir="asc"
          emptyMessage="No connections."
          filteredEmptyMessage="No connections match your search."
        />
      )}
    </Stack>
  );
}
