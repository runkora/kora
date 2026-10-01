'use client';

/** @tier 3 — environments-scoped. CRUD UI for the /environments list page. */

import { useCallback, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Badge,
  Button,
  DataTable,
  Heading,
  Icon,
  Stack,
  Text,
  TruncatingBadge,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ClampedList, ConfirmDialog, EmptyState } from '@orbit/components';
import type { ConnectionCatalogEntry } from '@orbit/shared/api';
import { deleteEnvironment } from '../_services/environments-api';
import { EnvironmentFormModal } from './environment-form-modal';
import type { EnvironmentRow } from '../_types';

interface EnvironmentsPageProps {
  rows: EnvironmentRow[];
  connections: ConnectionCatalogEntry[];
  error: string | null;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function environmentMatchesSearch(row: EnvironmentRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.name.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower) ||
    row.egressHosts.some((h) => h.toLowerCase().includes(lower)) ||
    row.connectors.some((c) => c.connection.toLowerCase().includes(lower))
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function EnvironmentsPage({ rows, connections, error }: EnvironmentsPageProps) {
  const router = useRouter();

  // null = closed; { environment: null } = create; { environment: row } = edit.
  const [formState, setFormState] = useState<{ environment: EnvironmentRow | null } | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<EnvironmentRow | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteCandidate) return;
    setDeleting(true);
    setDeleteError(null);
    const result = await deleteEnvironment(deleteCandidate.id);
    setDeleting(false);
    if (result.error) {
      setDeleteError(result.error);
      return;
    }
    setDeleteCandidate(null);
    router.refresh();
  }, [deleteCandidate, router]);

  const columns = useMemo<DataTableColumn<EnvironmentRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Name',
        gridWidth: 'minmax(0, 1.2fr)',
        minWidth: '10rem',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text size="supporting" weight="emphasis">
              {row.name}
            </Text>
            {row.description && (
              <Text size="caption" color="muted">
                {row.description}
              </Text>
            )}
          </Stack>
        ),
      },
      {
        id: 'egressHosts',
        header: 'Egress hosts',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '12rem',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.egressHosts.length,
        cell: (row) =>
          row.egressHosts.length === 0 ? (
            <Text size="supporting" color="muted">
              —
            </Text>
          ) : (
            <Badge variant="elevated" size="sm">
              {row.egressHosts.length} {row.egressHosts.length === 1 ? 'host' : 'hosts'}
            </Badge>
          ),
      },
      {
        id: 'connectors',
        header: 'Connectors',
        gridWidth: 'minmax(0, 1.5fr)',
        minWidth: '10rem',
        locked: true,
        wrap: true,
        sortable: true,
        filter: {
          mode: 'client',
          searchable: true,
          options: [...new Set(rows.flatMap((row) => row.connectors.map((c) => c.connection)))]
            .sort()
            .map((name) => ({ value: name, label: name })),
          filterFn: (row, selected) => row.connectors.some((c) => selected.has(c.connection)),
        },
        sortAccessor: (row) => row.connectors.length,
        cell: (row) =>
          row.connectors.length === 0 ? (
            <Text size="supporting" color="muted">
              —
            </Text>
          ) : (
            <ClampedList
              items={row.connectors}
              getKey={(c) => c.connection}
              renderItem={(c) => {
                const resourceCount = c.resources?.length ?? 0;
                return (
                  <TruncatingBadge
                    label={
                      resourceCount > 0
                        ? `${c.connection} · ${resourceCount} ${resourceCount === 1 ? 'resource' : 'resources'}`
                        : c.connection
                    }
                    variant="outline"
                    textColor="foreground"
                  />
                );
              }}
              noun="connectors"
              maxRows={2}
            />
          ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '5rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" justify="end">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={(e) => {
                e.stopPropagation();
                setFormState({ environment: row });
              }}
              aria-label={`Edit ${row.name}`}
            >
              <Icon name="edit" size="2xs" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={(e) => {
                e.stopPropagation();
                setDeleteCandidate(row);
              }}
              aria-label={`Delete ${row.name}`}
            >
              <Icon name="trash" size="2xs" />
            </Button>
          </Stack>
        ),
      },
    ],
    [rows],
  );

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Heading variant="title">Environments</Heading>
          <Text size="supporting" color="muted">
            An environment bundles egress hosts and connectors an agent type can reference. Attach
            one to an agent to fold its hosts and connectors into the agent&apos;s effective access.
          </Text>
        </Stack>
        <Button size="sm" onClick={() => setFormState({ environment: null })}>
          <Icon name="plus" size="xs" />
          New environment
        </Button>
      </Stack>

      {error ? (
        <Alert variant="error">{error}</Alert>
      ) : rows.length === 0 ? (
        <EmptyState
          icon="layers"
          title="No environments"
          description="Create an environment to bundle the egress hosts and connectors your agents need, then attach it to an agent type."
          action={
            <Button size="sm" onClick={() => setFormState({ environment: null })}>
              <Icon name="plus" size="xs" />
              New environment
            </Button>
          }
        />
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="environments"
          searchPlaceholder="Search environments…"
          searchFn={environmentMatchesSearch}
          defaultSortId="name"
          defaultSortDir="asc"
          emptyMessage="No environments."
          filteredEmptyMessage="No environments match your search."
        />
      )}

      {/* Create / edit modal — keyed so its internal state resets per open. */}
      {formState && (
        <EnvironmentFormModal
          key={formState.environment?.id ?? 'create'}
          open
          environment={formState.environment}
          connections={connections}
          onClose={() => setFormState(null)}
          onSaved={() => {
            setFormState(null);
            router.refresh();
          }}
        />
      )}

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteCandidate}
        title="Delete environment"
        description={`Permanently delete "${deleteCandidate?.name ?? ''}"? Agents that reference it will fall back to their own hosts and connectors.`}
        confirmLabel="Delete environment"
        confirming={deleting}
        error={deleteError}
        onConfirm={() => void handleDeleteConfirm()}
        onClose={() => {
          setDeleteCandidate(null);
          setDeleteError(null);
        }}
      />
    </Stack>
  );
}
