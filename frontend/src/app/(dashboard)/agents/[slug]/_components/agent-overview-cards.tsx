'use client';

/** @tier 3 · agents/[slug]-scoped. The Overview tab's Recent Runs table, Grant Scopes and Metadata cards. */

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import { Badge, Card, CardContent, CardHeader, DataTable, Stack, Text } from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { ScopeChip } from '@runkora/components';
import { fmtDate } from '@runkora/utils';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';
import { StartedByCell } from './started-by-cell';
import { RunStateDot } from '../../_components/running-now-card';

/** The five most recent runs. */
export function RecentRunsTable() {
  const { agent, recentRuns: runs } = useAgentDetailContext();
  const router = useRouter();

  const recentRuns = useMemo(
    () =>
      [...runs]
        .sort((a, b) => {
          const aTime = a.startedAt ?? a.createdAt;
          const bTime = b.startedAt ?? b.createdAt;
          return bTime.localeCompare(aTime);
        })
        .slice(0, 5),
    [runs],
  );

  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
      {
        id: 'id',
        header: 'Run ID',
        gridWidth: '6rem',
        minWidth: '5rem',
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
        gridWidth: '7rem',
        minWidth: '5.5rem',
        locked: true,
        cell: (row) => <RunStateDot state={row.state} />,
      },
      {
        id: 'duration',
        header: 'Duration',
        gridWidth: '6rem',
        minWidth: '5rem',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {formatDuration(row.durationMs)}
          </Text>
        ),
      },
      {
        id: 'startedBy',
        header: 'Started by',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '4rem',
        locked: true,
        cell: (row) => <StartedByCell row={row} />,
      },
      {
        id: 'started',
        header: 'Started',
        gridWidth: '6.5rem',
        minWidth: '5rem',
        locked: true,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.startedAt ? formatRelativeTime(row.startedAt) : '–'}
          </Text>
        ),
      },
    ],
    [],
  );

  return (
    <DataTable
      data={recentRuns}
      columns={columns}
      getRowKey={(row) => row.id}
      noun="runs"
      emptyMessage="No runs yet."
      defaultSortId="started"
      defaultSortDir="desc"
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          Recent Runs
        </Text>
      }
      onRowClick={(row) => {
        if (agent) {
          router.push(APP_ROUTES.agents.run(agent.slug, row.id));
        }
      }}
      rowLabel={(row) => `View run ${row.id}`}
    />
  );
}

export function GrantScopesCard({ agent }: { agent: AgentTypeRow }) {
  return (
    <Card>
      <CardHeader compact title="Grant Scopes" divider />
      <CardContent padded>
        {agent.grantScopes.length > 0 ? (
          <Stack direction="horizontal" gap="snug" wrap>
            {agent.grantScopes.map((scope) => (
              <ScopeChip key={scope} scope={scope} />
            ))}
          </Stack>
        ) : (
          <Text size="supporting" color="muted">
            No grant scopes configured.
          </Text>
        )}
      </CardContent>
    </Card>
  );
}

function MetadataRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack direction="horizontal" align="center" justify="between">
      <Text size="supporting" color="muted">
        {label}
      </Text>
      {children}
    </Stack>
  );
}

export function MetadataCard({ agent }: { agent: AgentTypeRow }) {
  return (
    <Card>
      <CardHeader compact title="Metadata" divider />
      <CardContent padded>
        <Stack gap="snug">
          <MetadataRow label="Created">
            <Text size="supporting">{fmtDate(agent.createdAt)}</Text>
          </MetadataRow>
          <MetadataRow label="Last updated">
            <Text size="supporting">{fmtDate(agent.updatedAt)}</Text>
          </MetadataRow>
          <MetadataRow label="Max concurrent / pod">
            <Text mono size="supporting">
              {agent.maxConcurrentRunsPerPod}
            </Text>
          </MetadataRow>
          <MetadataRow label="Model">
            <Badge variant="secondary" size="sm">
              {agent.modelId}
            </Badge>
          </MetadataRow>
          <MetadataRow label="Identity model">
            <Badge variant={agent.identityMode === 'delegated' ? 'default' : 'secondary'} size="sm">
              {agent.identityMode === 'delegated' ? 'Delegated' : 'Autonomous'}
            </Badge>
          </MetadataRow>
        </Stack>
      </CardContent>
    </Card>
  );
}
