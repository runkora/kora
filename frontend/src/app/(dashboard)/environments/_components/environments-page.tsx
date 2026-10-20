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
  ConfirmDialog,
  EmptyState,
  TruncatingBadge,
} from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import type { DataTableColumn } from '@runkora/hades';
import { ClampedList } from '@runkora/components';
import type { ConnectionCatalogEntry, EnvironmentSizes, SandboxImage } from '@runkora/shared/api';
import { deleteEnvironment } from '../_services/environments-api';
import { EnvironmentWizard } from './environment-wizard';
import { imageProblem, sizeLabel } from '@runkora/lib/environments';
import type { EnvironmentRow } from '../_types';
import { useHasScope } from '../../_components/permissions-provider';
import { PermissionHint } from '@runkora/components';

interface EnvironmentsPageProps {
  rows: EnvironmentRow[];
  connections: ConnectionCatalogEntry[];
  /** Registered sandbox images, or null when the list did not load. */
  images: SandboxImage[] | null;
  /** The platform's default and maximum sizes, or null when they did not load. */
  sizes: EnvironmentSizes | null;
  error: string | null;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function environmentMatchesSearch(row: EnvironmentRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.name.toLowerCase().includes(lower) ||
    row.slug.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower) ||
    row.sandboxImage.toLowerCase().includes(lower) ||
    row.egressHosts.some((h) => h.toLowerCase().includes(lower)) ||
    row.connectors.some((c) => c.connection.toLowerCase().includes(lower))
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function EnvironmentsPage({
  rows,
  connections,
  images,
  sizes,
  error,
}: EnvironmentsPageProps) {
  const router = useRouter();
  const { productName } = useBrand();

  // null = closed; { environment: null } = create; { environment: row } = edit.
  const [formState, setFormState] = useState<{ environment: EnvironmentRow | null } | null>(null);
  const canWrite = useHasScope('agents:environments:write');
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
            <Text size="caption" mono color="subtle">
              {row.slug}
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
        id: 'image',
        header: 'Sandbox image',
        gridWidth: 'minmax(0, 1.2fr)',
        minWidth: '10rem',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.sandboxImage,
        cell: (row) => {
          const problem = imageProblem(row.sandboxImage, images);
          return (
            <Stack gap="none">
              <Text size="supporting" mono>
                {row.sandboxImage}
              </Text>
              {problem && (
                <Text size="caption" color="status-error">
                  {problem}
                </Text>
              )}
            </Stack>
          );
        },
      },
      {
        id: 'size',
        header: 'Size',
        gridWidth: '13rem',
        minWidth: '9rem',
        locked: true,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {sizeLabel(row.size)}
          </Text>
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
            <PermissionHint allowed={canWrite} action="edit environments">
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite}
                onClick={(e) => {
                  e.stopPropagation();
                  setFormState({ environment: row });
                }}
                aria-label={`Edit ${row.name}`}
              >
                <Icon name="edit" size="2xs" />
              </Button>
            </PermissionHint>
            <PermissionHint allowed={canWrite} action="delete environments">
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite}
                onClick={(e) => {
                  e.stopPropagation();
                  setDeleteCandidate(row);
                }}
                aria-label={`Delete ${row.name}`}
              >
                <Icon name="trash" size="2xs" />
              </Button>
            </PermissionHint>
          </Stack>
        ),
      },
    ],
    [rows, images, canWrite],
  );

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Heading variant="title">Environments</Heading>
          <Text size="supporting" color="muted">
            An environment is a sandbox an agent can run in: a sandbox image, a size, and the egress
            hosts and connections agents get with it. Attach environments to an agent on its
            configuration page.
          </Text>
        </Stack>
        <PermissionHint allowed={canWrite} action="create environments">
          <Button
            size="sm"
            disabled={!canWrite}
            onClick={() => setFormState({ environment: null })}
          >
            <Icon name="plus" size="xs" />
            New environment
          </Button>
        </PermissionHint>
      </Stack>

      {error ? (
        <Alert variant="error">{error}</Alert>
      ) : rows.length === 0 ? (
        <EmptyState
          icon="layers"
          title="No environments"
          description={`Agents without an environment run on the ${productName} base image. Create an environment to give them a registered image, a size and access.`}
          action={
            <PermissionHint allowed={canWrite} action="create environments">
              <Button
                size="sm"
                disabled={!canWrite}
                onClick={() => setFormState({ environment: null })}
              >
                <Icon name="plus" size="xs" />
                New environment
              </Button>
            </PermissionHint>
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

      {/* Create / edit wizard, keyed so its internal state resets per open. */}
      {formState && (
        <EnvironmentWizard
          key={formState.environment?.id ?? 'create'}
          environment={formState.environment}
          environments={rows}
          connections={connections}
          images={images}
          sizes={sizes}
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
        description={`Permanently delete "${deleteCandidate?.name ?? ''}"? An environment attached to an agent cannot be deleted.`}
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
