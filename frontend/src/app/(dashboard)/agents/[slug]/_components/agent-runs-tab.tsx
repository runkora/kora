'use client';

/** @tier 3 — agents/[slug]-scoped. Runs table tab for the agent detail page. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, DataTable, SegmentedControl, Stack, Text } from '@orbit/terra';

import { DelegatedRunNotice } from '../../_components/motif/delegated-run-notice';
import type { DataTableColumn } from '@orbit/terra';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';
import { StartedByCell } from './started-by-cell';

// ── Status groupings ──────────────────────────────────────────────────────────

const ACTIVE_STATES = ['pending', 'starting', 'active', 'idle', 'completing'];

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
  const { agent, runs, status, error, refresh, deleteRun } = useAgentDetailContext();
  const router = useRouter();

  const [deleteTarget, setDeleteTarget] = useState<RunRow | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<'all' | 'active' | 'completed' | 'failed'>(
    'all',
  );

  const runCounts = useMemo(
    () => ({
      all: runs.length,
      active: runs.filter((r) => ACTIVE_STATES.includes(r.state)).length,
      completed: runs.filter((r) => r.state === 'completed').length,
      failed: runs.filter((r) => r.state === 'failed' || r.state === 'budget_exhausted').length,
    }),
    [runs],
  );

  const filteredRuns = useMemo(() => {
    if (statusFilter === 'all') return runs;
    if (statusFilter === 'active') return runs.filter((r) => ACTIVE_STATES.includes(r.state));
    if (statusFilter === 'completed') return runs.filter((r) => r.state === 'completed');
    return runs.filter((r) => r.state === 'failed' || r.state === 'budget_exhausted');
  }, [runs, statusFilter]);

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
        locked: true,
        cell: (row) => runStatusBadge(row),
      },
      {
        id: 'startedBy',
        header: 'Started by',
        locked: true,
        cell: (row) => <StartedByCell row={row} />,
      },
      {
        id: 'started',
        header: 'Started',
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
        locked: true,
        gridWidth: '8rem',
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

  // No paddingTop on the root: Terra's TabsContent already provides the
  // uniform gap below the tab rail; adding more here doubled it on this tab.
  return (
    <Stack gap="snug">
      {/* Delegated agents run on behalf of their starter, so their runs are
          private — make that legible rather than leaving lists mysteriously short. */}
      {agent?.identityMode === 'delegated' && <DelegatedRunNotice />}
      {/* Show EmptyState when there are no runs and no error */}
      {status !== 'error' && runs.length === 0 && status !== 'loading' ? (
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
          data={filteredRuns}
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
              value={statusFilter}
              onChange={(v) => setStatusFilter(v as typeof statusFilter)}
              options={[
                { value: 'all', label: 'All', count: runCounts.all },
                { value: 'active', label: 'Active', count: runCounts.active },
                { value: 'completed', label: 'Completed', count: runCounts.completed },
                { value: 'failed', label: 'Failed', count: runCounts.failed },
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
          emptyMessage="No runs yet."
          filteredEmptyMessage="No runs match your search."
          onRowClick={(row) => {
            if (agent) {
              router.push(APP_ROUTES.agents.run(agent.slug, row.id));
            }
          }}
        />
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
