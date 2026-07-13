'use client';

/** @tier 3 — dev-environments-scoped. List/provision/connect/extend/destroy UI
 * for the ephemeral, vcluster-backed dev environments feature
 * (docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md). */

import { useCallback, useMemo, useState } from 'react';
import { Alert, Badge, Button, DataTable, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import type { DevEnvironment } from '@orbit/shared/api';
import { useSession } from '../../_components/permissions-provider';
import { useDevEnvironments } from '../_hooks/use-dev-environments';
import { formatCountdown, formatTimestamp, isInFlight, phaseStatus } from '../_types';
import { ProvisionModal } from './provision-modal';
import { ConnectModal } from './connect-modal';
import { ExtendModal } from './extend-modal';

interface DevEnvironmentsPageProps {
  environments: DevEnvironment[];
  initialError: string | null;
}

function matchesSearch(row: DevEnvironment, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.id.toLowerCase().includes(lower) ||
    row.ref.toLowerCase().includes(lower) ||
    row.owner.toLowerCase().includes(lower) ||
    (row.appURL ?? '').toLowerCase().includes(lower)
  );
}

export function DevEnvironmentsPage({ environments, initialError }: DevEnvironmentsPageProps) {
  const {
    environments: rows,
    error,
    create,
    extend,
    wake,
    destroy,
    kubeconfig,
  } = useDevEnvironments(environments, initialError);
  const { devEnvironmentId } = useSession();
  const canCreate = !devEnvironmentId;

  const [provisionOpen, setProvisionOpen] = useState(false);
  const [connectTarget, setConnectTarget] = useState<DevEnvironment | null>(null);
  const [extendTarget, setExtendTarget] = useState<DevEnvironment | null>(null);
  const [destroyTarget, setDestroyTarget] = useState<DevEnvironment | null>(null);
  const [destroying, setDestroying] = useState(false);
  const [destroyError, setDestroyError] = useState<string | null>(null);
  const [wakingId, setWakingId] = useState<string | null>(null);

  const handleWake = useCallback(
    async (id: string) => {
      setWakingId(id);
      const result = await wake(id);
      setWakingId(null);
      return result;
    },
    [wake],
  );

  const handleDestroyConfirm = async () => {
    if (!destroyTarget) return;
    setDestroying(true);
    setDestroyError(null);
    const result = await destroy(destroyTarget.id);
    setDestroying(false);
    if (result.error) {
      setDestroyError(result.error);
      return;
    }
    setDestroyTarget(null);
  };

  const columns = useMemo<DataTableColumn<DevEnvironment>[]>(
    () => [
      {
        id: 'ref',
        header: 'Environment',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.ref || row.id,
        cell: (row) => (
          <Stack gap="none">
            <Text size="supporting" weight="emphasis">
              {row.ref || 'main'}
            </Text>
            <Text size="caption" color="muted" mono>
              {row.id}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'phase',
        header: 'Status',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.phase,
        cell: (row) => <StatusBadge status={phaseStatus(row.phase)} label={row.phase} />,
      },
      {
        id: 'appURL',
        header: 'URL',
        locked: true,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.appURL ? row.appURL.replace(/^https?:\/\//, '') : '—'}
          </Text>
        ),
      },
      {
        id: 'createdAt',
        header: 'Created',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.createdAt,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {formatTimestamp(row.createdAt)}
          </Text>
        ),
      },
      {
        id: 'expiresAt',
        header: 'Expires',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.expiresAt ?? '',
        cell: (row) =>
          row.expiresAt ? (
            <Badge variant="outline" size="sm">
              {formatCountdown(row.expiresAt)}
            </Badge>
          ) : (
            <Text size="supporting" color="muted">
              —
            </Text>
          ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '16rem',
        cell: (row) => {
          if (isInFlight(row.phase)) {
            return (
              <Stack direction="horizontal" align="center" justify="end" gap="tight">
                <Text size="caption" color="muted">
                  {row.phase}…
                </Text>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    setDestroyTarget(row);
                  }}
                  aria-label={`Destroy ${row.id}`}
                >
                  <Icon name="trash" size="2xs" />
                </Button>
              </Stack>
            );
          }
          return (
            <Stack direction="horizontal" gap="tight" justify="end">
              {row.phase === 'Sleeping' && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    void handleWake(row.id);
                  }}
                  disabled={wakingId === row.id}
                  aria-label={`Wake ${row.id}`}
                >
                  <Icon name="sun" size="2xs" />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={(e) => {
                  e.stopPropagation();
                  window.open(row.appURL ?? undefined, '_blank', 'noopener,noreferrer');
                }}
                disabled={!row.appURL}
                aria-label={`Open ${row.id}`}
              >
                <Icon name="external-link" size="2xs" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={(e) => {
                  e.stopPropagation();
                  setConnectTarget(row);
                }}
                aria-label={`Connect to ${row.id}`}
              >
                <Icon name="square-terminal" size="2xs" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={(e) => {
                  e.stopPropagation();
                  setExtendTarget(row);
                }}
                aria-label={`Extend ${row.id}`}
              >
                <Icon name="clock" size="2xs" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={(e) => {
                  e.stopPropagation();
                  setDestroyTarget(row);
                }}
                aria-label={`Destroy ${row.id}`}
              >
                <Icon name="trash" size="2xs" />
              </Button>
            </Stack>
          );
        },
      },
    ],
    [wakingId, handleWake],
  );

  return (
    <Stack gap="comfortable">
      <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
        <Stack gap="tight">
          <Heading variant="title">Dev Environments</Heading>
          <Text size="supporting" color="muted">
            {canCreate
              ? 'Disposable, full-stack Orbit installs — each a real vcluster running the actual operator and charts. TTL-bounded, sleep when idle, delete and re-provision to recover.'
              : 'This install is itself a dev environment, so it cannot provision further dev environments.'}
          </Text>
        </Stack>
        {canCreate && (
          <Button size="sm" onClick={() => setProvisionOpen(true)}>
            <Icon name="rocket" size="xs" />
            New environment
          </Button>
        )}
      </Stack>

      {error ? (
        <Alert variant="error">{error}</Alert>
      ) : rows.length === 0 ? (
        <EmptyState
          icon="rocket"
          title="No dev environments"
          description="Provision a disposable Orbit install to iterate on operator, chart, or mesh changes without touching shared staging."
          action={
            canCreate ? (
              <Button size="sm" onClick={() => setProvisionOpen(true)}>
                <Icon name="rocket" size="xs" />
                New environment
              </Button>
            ) : undefined
          }
        />
      ) : (
        <DataTable
          data={rows}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="environments"
          searchPlaceholder="Search environments…"
          searchFn={matchesSearch}
          defaultSortId="createdAt"
          defaultSortDir="desc"
          emptyMessage="No dev environments."
          filteredEmptyMessage="No environments match your search."
        />
      )}

      <ProvisionModal
        open={provisionOpen}
        onClose={() => setProvisionOpen(false)}
        onCreate={create}
      />

      <ConnectModal
        environment={connectTarget}
        onClose={() => setConnectTarget(null)}
        onFetchKubeconfig={kubeconfig}
        onWake={wake}
      />

      <ExtendModal
        environment={extendTarget}
        onClose={() => setExtendTarget(null)}
        onExtend={extend}
      />

      <ConfirmDialog
        open={!!destroyTarget}
        title="Destroy dev environment"
        description={`Permanently delete "${destroyTarget?.ref || destroyTarget?.id || ''}"? Its namespace, vcluster, and everything in it are torn down immediately — this cannot be undone.`}
        confirmLabel="Destroy environment"
        confirming={destroying}
        error={destroyError}
        onConfirm={() => void handleDestroyConfirm()}
        onClose={() => {
          setDestroyTarget(null);
          setDestroyError(null);
        }}
      />
    </Stack>
  );
}
