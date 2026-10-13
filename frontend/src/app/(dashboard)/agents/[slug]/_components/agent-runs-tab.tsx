'use client';

/** @tier 3 — agents/[slug]-scoped. Runs table tab for the agent detail page. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Button,
  DataTable,
  SegmentedControl,
  Stack,
  Text,
  ConfirmDialog,
  EmptyState,
} from '@clear-route/hades';

import { DelegatedRunNotice } from '../../_components/motif/delegated-run-notice';
import type { DataTableColumn } from '@clear-route/hades';
import { StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import type { RunStateCounts } from '@orbit/shared/api';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { RUN_FILTER_STATES } from '../_hooks/use-agent-detail';
import type { RunFilter } from '../_hooks/use-agent-detail';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';
import { StartedByCell } from './started-by-cell';

/** How many runs a filter matches, from the server's counts; undefined without them. */
function filterCount(counts: RunStateCounts | null, filter: RunFilter): number | undefined {
  if (!counts) return undefined;
  if (filter === 'all') return counts.total;
  return RUN_FILTER_STATES[filter].reduce((n, state) => n + (counts.by_state[state] ?? 0), 0);
}

// ── Status helpers ────────────────────────────────────────────────────────────

function runStatusBadge(row: RunRow) {
  const statusMap: Record<string, 'idle' | 'degraded' | 'healthy' | 'error'> = {
    pending: 'idle',
    starting: 'degraded',
    active: 'healthy',
    idle: 'degraded',
    completing: 'degraded',
    completed: 'idle',
    failed: 'error',
    budget_exhausted: 'error',
  };
  const labelMap: Record<string, string> = {
    pending: 'Pending',
    starting: 'Starting',
    active: 'Active',
    idle: 'Waiting',
    completing: 'Completing',
    completed: 'Completed',
    failed: 'Failed',
    budget_exhausted: 'Budget exhausted',
  };
  return (
    <StatusBadge
      status={statusMap[row.state] ?? 'unknown'}
      label={labelMap[row.state] ?? row.state}
    />
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

interface AgentRunsTabProps {
  onStartRun: () => void;
}

export function AgentRunsTab({ onStartRun }: AgentRunsTabProps) {
  const {
    agent,
    runs,
    stateCounts,
    runFilter,
    setRunFilter,
    loadMoreRuns,
    status,
    error,
    refresh,
    deleteRun,
  } = useAgentDetailContext();
  const router = useRouter();

  const [deleteTarget, setDeleteTarget] = useState<RunRow | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const handleDelete = async () => {
    if (!deleteTarget) return;
    setIsDeleting(true);
    setDeleteError(null);
    const result = await deleteRun(deleteTarget.id);
    setIsDeleting(false);
    if (result.error) {
      setDeleteError(result.error);
    } else {
      setDeleteTarget(null);
    }
  };

  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
      {
        id: 'summary',
        header: 'Task',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '9rem',
        locked: true,
        cell: (row) =>
          row.summary ? (
            <Text
              size="caption"
              style={{
                maxWidth: '22rem',
                display: '-webkit-box',
                WebkitLineClamp: 2,
                WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
                lineHeight: 1.35,
              }}
              title={row.summary}
            >
              {row.summary}
            </Text>
          ) : (
            <Text size="caption" color="muted">
              —
            </Text>
          ),
      },
      {
        id: 'id',
        header: 'Run ID',
        gridWidth: '8rem',
        minWidth: '5.5rem',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {row.id.slice(0, 8)}
          </Text>
        ),
      },
      {
        id: 'state',
        header: 'Status',
        gridWidth: '7.5rem',
        minWidth: '4.5rem',
        locked: true,
        cell: (row) => runStatusBadge(row),
      },
      {
        id: 'startedBy',
        header: 'Started by',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '6rem',
        locked: true,
        cell: (row) => <StartedByCell row={row} />,
      },
      {
        id: 'started',
        header: 'Started',
        gridWidth: '8rem',
        minWidth: '5rem',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.startedAt ?? row.createdAt,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.startedAt ? formatRelativeTime(row.startedAt) : '—'}
          </Text>
        ),
      },
      {
        id: 'duration',
        header: 'Duration',
        gridWidth: '6.5rem',
        minWidth: '4.5rem',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {formatDuration(row.durationMs)}
          </Text>
        ),
      },
      {
        id: 'tools',
        header: 'Tools',
        gridWidth: '6rem',
        minWidth: '4rem',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {row.toolCalls !== null ? String(row.toolCalls) : '—'}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '7rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="snug">
            <Button
              variant="ghost"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                if (agent) {
                  router.push(APP_ROUTES.agents.run(agent.slug, row.id));
                }
              }}
            >
              Open
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                setDeleteError(null);
                setDeleteTarget(row);
              }}
            >
              Delete
            </Button>
          </Stack>
        ),
      },
    ],
    [agent, router],
  );

  // No paddingTop on the root: Hades' TabsContent already provides the
  // uniform gap below the tab rail; adding more here doubled it on this tab.
  return (
    <Stack gap="snug">
      {/* Delegated agents run on behalf of their starter, so their runs are
          private — make that legible rather than leaving lists mysteriously short. */}
      {agent?.identityMode === 'delegated' && <DelegatedRunNotice />}
      {/* Show EmptyState when there are no runs and no error */}
      {status !== 'error' &&
      runFilter === 'all' &&
      runs.rows.length === 0 &&
      status !== 'loading' ? (
        <EmptyState
          icon="play-circle"
          title="No runs yet"
          description="Start your first run to execute tasks with this agent."
          action={
            <Button onClick={onStartRun} disabled={agent?.state !== 'active'}>
              Start your first run
            </Button>
          }
        />
      ) : (
        <DataTable
          data={status === 'loading' ? [] : runs.rows}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="runs"
          defaultSortId="started"
          defaultSortDir="desc"
          searchFn={(row, q) =>
            row.id.toLowerCase().includes(q) ||
            (row.summary?.toLowerCase().includes(q) ?? false) ||
            (row.createdByName?.toLowerCase().includes(q) ?? false) ||
            row.createdBy.toLowerCase().includes(q)
          }
          searchPlaceholder="Search runs…"
          quickFilters={
            <SegmentedControl
              aria-label="Filter runs by status"
              value={runFilter}
              onChange={(v) => setRunFilter(v as RunFilter)}
              options={[
                { value: 'all', label: 'All', count: filterCount(stateCounts, 'all') },
                { value: 'active', label: 'Active', count: filterCount(stateCounts, 'active') },
                {
                  value: 'completed',
                  label: 'Completed',
                  count: filterCount(stateCounts, 'completed'),
                },
                { value: 'failed', label: 'Failed', count: filterCount(stateCounts, 'failed') },
              ]}
            />
          }
          error={
            status === 'error' ? (
              <Stack align="center" paddingBlock="comfortable">
                <Text size="supporting" color="muted">
                  {error ?? 'Failed to load runs.'}
                </Text>
                <Button variant="outline" size="sm" onClick={() => void refresh()}>
                  Try again
                </Button>
              </Stack>
            ) : undefined
          }
          emptyMessage={
            status === 'loading'
              ? 'Loading runs…'
              : runFilter === 'all'
                ? 'No runs yet.'
                : 'No runs match this filter.'
          }
          filteredEmptyMessage={
            status === 'loading' ? 'Loading runs…' : 'No runs match your search.'
          }
          onRowClick={(row) => {
            if (agent) {
              router.push(APP_ROUTES.agents.run(agent.slug, row.id));
            }
          }}
        />
      )}

      {runs.nextCursor && status !== 'error' && (
        <Stack align="center" gap="tight" paddingBlock="tight">
          <Button
            variant="outline"
            size="sm"
            onClick={() => void loadMoreRuns()}
            disabled={runs.loadingMore}
          >
            {runs.loadingMore ? 'Loading…' : runs.loadMoreError ? 'Retry' : 'Load more runs'}
          </Button>
          {runs.loadMoreError && (
            <Text size="caption" color="status-error">
              {runs.loadMoreError}
            </Text>
          )}
        </Stack>
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete run"
        description={`Delete run ${deleteTarget?.id.slice(0, 8) ?? ''}? This will remove all events and messages and cannot be undone.`}
        confirmLabel="Delete"
        confirming={isDeleting}
        error={deleteError}
        onConfirm={() => void handleDelete()}
        onClose={() => {
          setDeleteTarget(null);
          setDeleteError(null);
        }}
      />
    </Stack>
  );
}
