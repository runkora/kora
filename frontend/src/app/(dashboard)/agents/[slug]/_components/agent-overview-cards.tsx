'use client';

/** @tier 3 · agents/[slug]-scoped. The Overview tab's Recent Runs, Grant Scopes and Metadata cards. */

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  DataTable,
  Icon,
  Stack,
  Text,
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { ScopeChip } from '@runkora/components';
import { fmtDate } from '@runkora/utils';
import { APP_ROUTES } from '@runkora/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import type { AgentTypeRow, RunRow } from '../../_types';
import { RunListRow } from '../../_components/run-row';

const RECENT_RUNS = 5;

interface RecentRunsCardProps {
  /** Switches the page to the Runs tab. */
  onViewAll: () => void;
}

/** The five newest runs as Runs tab rows, without headers or row controls. */
export function RecentRunsCard({ onViewAll }: RecentRunsCardProps) {
  const { agent, recentRuns } = useAgentDetailContext();
  const router = useRouter();

  const rows = useMemo(
    () =>
      [...recentRuns]
        .sort((a, b) => (b.startedAt ?? b.createdAt).localeCompare(a.startedAt ?? a.createdAt))
        .slice(0, RECENT_RUNS),
    [recentRuns],
  );

  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
      {
        id: 'run',
        header: 'Run',
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '16rem',
        locked: true,
        // The card is narrower than the Runs tab, so one delivery chip shows before "+N".
        cell: (row) => <RunListRow row={row} maxChips={1} />,
      },
    ],
    [],
  );

  return (
    <DataTable
      data={rows}
      columns={columns}
      getRowKey={(row) => row.id}
      noun="runs"
      hideHeader
      columnToggle={false}
      emptyMessage="No runs yet."
      toolbarTitle={
        <Text size="supporting" weight="emphasis">
          Recent Runs
        </Text>
      }
      toolbarActions={
        <Button variant="ghost" size="sm" onClick={onViewAll}>
          View all runs
          <Icon name="chevron-right" size="2xs" />
        </Button>
      }
      onRowClick={(row) => {
        if (agent) router.push(APP_ROUTES.agents.run(agent.slug, row.id));
      }}
      rowLabel={(row) => `Open run ${row.id.slice(0, 8)}`}
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

export function MetadataCard({ agent, fill }: { agent: AgentTypeRow; fill?: boolean }) {
  return (
    <Stack asChild grow={fill}>
      <Card column>
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
              <Badge
                variant={agent.identityMode === 'delegated' ? 'default' : 'secondary'}
                size="sm"
              >
                {agent.identityMode === 'delegated' ? 'Delegated' : 'Autonomous'}
              </Badge>
            </MetadataRow>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
