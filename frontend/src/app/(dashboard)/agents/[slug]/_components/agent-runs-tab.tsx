'use client';

/** @tier 3 — agents/[slug]-scoped. The agent detail page's Runs tab: a list of run rows. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Button,
  DataTable,
  Icon,
  SegmentedControl,
  Stack,
  Text,
  ConfirmDialog,
  EmptyState,
} from '@runkora/hades';

import { DelegatedRunNotice } from '../../_components/motif/delegated-run-notice';
import type { DataTableColumn } from '@runkora/hades';
import { PermissionHint } from '@runkora/components';
import { APP_ROUTES } from '@runkora/lib/routing';
import type { RunStateCounts } from '@runkora/shared/api';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { RUN_FILTER_STATES } from '../_hooks/use-agent-detail';
import type { RunFilter } from '../_hooks/use-agent-detail';
import type { RunRow } from '../../_types';
import { RunListRow } from '../../_components/run-row';

/** How many runs a filter matches, from the server's counts; undefined without them. */
function filterCount(counts: RunStateCounts | null, filter: RunFilter): number | undefined {
  if (!counts) return undefined;
  if (filter === 'all') return counts.total;
  return RUN_FILTER_STATES[filter].reduce((n, state) => n + (counts.by_state[state] ?? 0), 0);
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

  const canRun = !!agent?.canRun;
  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
      {
        id: 'started',
        header: 'Run',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '20rem',
        locked: true,
        sortAccessor: (row) => row.startedAt ?? row.createdAt,
        cell: (row) => (
          <RunListRow
            row={row}
            action={
              // The button keeps its width; only the icon shows, on row hover or focus.
              <PermissionHint allowed={canRun} action="delete runs of this agent">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Delete run ${row.id.slice(0, 8)}`}
                  disabled={!canRun}
                  onClick={(e) => {
                    e.stopPropagation();
                    setDeleteError(null);
                    setDeleteTarget(row);
                  }}
                >
                  <Icon name="trash" size="2xs" color="muted" groupReveal />
                </Button>
              </PermissionHint>
            }
          />
        ),
      },
    ],
    [canRun],
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
            <PermissionHint allowed={!!agent?.canRun} action="start runs of this agent">
              <Button onClick={onStartRun} disabled={!agent?.canRun || agent.state !== 'active'}>
                Start your first run
              </Button>
            </PermissionHint>
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
          hideHeader
          columnToggle={false}
          searchFn={(row, q) =>
            row.id.toLowerCase().includes(q) ||
            (row.summary?.toLowerCase().includes(q) ?? false) ||
            (row.createdByName?.toLowerCase().includes(q) ?? false) ||
            row.createdBy.toLowerCase().includes(q) ||
            row.sources.some((s) => s.label.toLowerCase().includes(q))
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
          rowLabel={(row) => `Open run ${row.id.slice(0, 8)}`}
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
