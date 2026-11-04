'use client';

/** @tier 3 — connections-scoped. Read-only UI for the /connections list page. */

import { useMemo, useState } from 'react';
import {
  Alert,
  DataTable,
  EmptyState,
  Heading,
  Stack,
  Text,
  TruncatingBadge,
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { ScopeChipList, scopeColumnFilter } from '@runkora/components';
import { adapterLabel } from '../_types';
import type { ConnectionRow } from '../_types';
import { ConnectionDetailModal } from './connection-detail-modal';
import { UsedByList } from './used-by-list';

// Rows commonly carry a dozen-plus actions. Show as many as fit in two lines;
// the rest are one click away in the "+N more" overlay and the detail modal.
const MAX_ACTION_ROWS = 2;

interface ConnectionsPageProps {
  rows: ConnectionRow[];
  error: string | null;
  /** Why the "used by" column is empty, when the agents could not be read. */
  usedByError?: string | null;
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

export function ConnectionsPage({ rows, error, usedByError = null }: ConnectionsPageProps) {
  const [selectedConnection, setSelectedConnection] = useState<ConnectionRow | null>(null);

  const columns = useMemo<DataTableColumn<ConnectionRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        // Room for `constellation-kubernetes` + 50% (about 300px of mono text); longer
        // names truncate, with the full name on hover.
        gridWidth: '19rem',
        minWidth: '8rem', // shrinks toward this only when the viewport is too narrow
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
        // The `Kubernetes` badge + 50%; longer labels truncate.
        gridWidth: '8.5rem',
        minWidth: '6rem',
        cell: (row) => (
          // Horizontal row so the badge keeps its natural width instead of
          // stretching to fill the cell.
          <Stack direction="horizontal" minWidth="none">
            <TruncatingBadge
              label={adapterLabel(row.adapter)}
              variant="secondary"
              textColor="muted"
            />
          </Stack>
        ),
      },
      {
        id: 'actions',
        header: 'Grantable actions',
        locked: true,
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '16rem',
        wrap: true,
        filter: scopeColumnFilter(rows, (row) => row.actions),
        cell: (row) => <ScopeChipList scopes={row.actions} maxRows={MAX_ACTION_ROWS} />,
      },
      {
        id: 'usedBy',
        header: 'Used by',
        locked: true,
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '14rem',
        wrap: true,
        filter: {
          mode: 'client',
          searchable: true,
          // Keyed on slug, the unique field; two agent types can share a name.
          options: [
            ...new Map(
              rows.flatMap((row) => row.usedBy.map((u): [string, string] => [u.slug, u.name])),
            ).entries(),
          ]
            .sort(([, a], [, b]) => a.localeCompare(b))
            .map(([slug, name]) => ({ value: slug, label: name })),
          filterFn: (row, selected) => row.usedBy.some((u) => selected.has(u.slug)),
        },
        cell: (row) => <UsedByList usages={row.usedBy} />,
      },
    ],
    [rows],
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

      {!error && usedByError && (
        <Alert variant="info">
          <Text size="caption">Agent usage is not shown. {usedByError}</Text>
        </Alert>
      )}

      {error ? (
        <EmptyState icon="lock" title="Connections unavailable" description={error} />
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
          onRowClick={setSelectedConnection}
          rowLabel={(row) => `View details for ${row.name}`}
        />
      )}

      <ConnectionDetailModal
        connection={selectedConnection}
        onClose={() => setSelectedConnection(null)}
      />
    </Stack>
  );
}
