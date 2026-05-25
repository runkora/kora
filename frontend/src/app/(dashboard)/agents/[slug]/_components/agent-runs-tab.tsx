'use client';

/** @tier 3 — agents/[slug]-scoped. Runs table tab for the agent detail page. */

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import { Button, DataTable, Stack, Text } from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// ── Status helpers ────────────────────────────────────────────────────────────

function runStatusBadge(row: RunRow) {
  const statusMap: Record<string, 'idle' | 'degraded' | 'healthy' | 'error'> = {
    pending: 'idle',
    starting: 'degraded',
    active: 'healthy',
    completing: 'degraded',
    completed: 'idle',
    failed: 'error',
  };
  const labelMap: Record<string, string> = {
    pending: 'Pending',
    starting: 'Starting',
    active: 'Active',
    completing: 'Completing',
    completed: 'Completed',
    failed: 'Failed',
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
  const { agent, runs, status, error, refresh } = useAgentDetailContext();
  const router = useRouter();

  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
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
        id: 'tokens',
        header: 'Tokens',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {row.tokensUsed ? formatTokens(row.tokensUsed) : '—'}
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
        width: '5rem',
        cell: (row) => (
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
        ),
      },
    ],
    [agent, router],
  );

  return (
    <Stack gap="snug" paddingTop="comfortable">
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
          data={runs}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="runs"
          defaultSortId="started"
          defaultSortDir="desc"
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
    </Stack>
  );
}
