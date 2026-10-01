'use client';

/** @tier 3 — agents/[slug]-scoped. Overview tab: stats, recent runs, system prompt, scopes, metadata. */

import { useState, useMemo, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CodeBlock,
  DataTable,
  Grid,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ScopeChip, StatusBadge } from '@orbit/components';
import { fmtDate, fmtUsd, fmtTokens } from '@orbit/utils';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { fetchAgentTypeUsage } from '../_services/agent-detail-api';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';
import { AgentBudgetCard } from './agent-budget-card';
import { StartedByCell } from './started-by-cell';
import type { AgentTypeUsage } from '@orbit/shared/api';

const PROMPT_PREVIEW_LENGTH = 300;

// ── Helpers ───────────────────────────────────────────────────────────────────

function runStatusBadge(row: RunRow) {
  const statusMap: Record<string, 'idle' | 'degraded' | 'healthy' | 'error'> = {
    pending: 'idle',
    starting: 'degraded',
    active: 'healthy',
    completing: 'degraded',
    completed: 'idle',
    failed: 'error',
    budget_exhausted: 'error',
  };
  const labelMap: Record<string, string> = {
    pending: 'Pending',
    starting: 'Starting',
    active: 'Active',
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

interface AgentOverviewTabProps {
  budget: BudgetPrincipalStatus | null;
  tenantCap: TenantCapStatus | null;
}

export function AgentOverviewTab({ budget, tenantCap }: AgentOverviewTabProps) {
  const { agent, runs } = useAgentDetailContext();
  const router = useRouter();
  const [promptExpanded, setPromptExpanded] = useState(false);
  const [agentUsage, setAgentUsage] = useState<AgentTypeUsage | null>(null);

  // Fetch 7-day token breakdown and spend from the inference gateway.
  useEffect(() => {
    if (!agent) return;
    void fetchAgentTypeUsage(agent.slug).then(({ usage }) => {
      if (usage) setAgentUsage(usage);
    });
  }, [agent]);

  // Token-usage rows for the sidebar breakdown (under Budget). Precise counts,
  // shown as a compact label → value definition list.
  const usageRows = useMemo(() => {
    if (!agentUsage) return null;
    return [
      { label: 'Input', value: agentUsage.prompt_tokens },
      { label: 'Output', value: agentUsage.completion_tokens },
      { label: 'Cache read', value: agentUsage.cache_read_tokens },
      { label: 'Cache write', value: agentUsage.cache_creation_tokens },
    ];
  }, [agentUsage]);

  // ── Stats computation ──────────────────────────────────────────────────────

  const totalRuns = runs.length;

  const completedRuns = useMemo(() => runs.filter((r) => r.state === 'completed').length, [runs]);

  const successRate = useMemo(() => {
    if (totalRuns === 0) return 'N/A';
    return `${Math.round((completedRuns / totalRuns) * 100)}%`;
  }, [completedRuns, totalRuns]);

  const avgDuration = useMemo(() => {
    const completed = runs.filter((r) => r.state === 'completed' && r.durationMs !== null);
    if (completed.length === 0) return '—';
    const avg = completed.reduce((sum, r) => sum + (r.durationMs ?? 0), 0) / completed.length;
    return formatDuration(avg);
  }, [runs]);

  // ── Recent runs (5 most recent) ────────────────────────────────────────────

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

  const recentRunColumns = useMemo<DataTableColumn<RunRow>[]>(
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
        cell: (row) => runStatusBadge(row),
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
            {row.startedAt ? formatRelativeTime(row.startedAt) : '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  if (!agent) return null;

  const promptTruncated =
    !promptExpanded && agent.systemPrompt.length > PROMPT_PREVIEW_LENGTH
      ? agent.systemPrompt.slice(0, PROMPT_PREVIEW_LENGTH) + '…'
      : agent.systemPrompt;

  return (
    <Stack gap="comfortable">
      {/* No root paddingBlock: Terra's TabsContent provides the uniform gap
          below the tab rail, matching the other tabs. */}
      {/* ── Stat cards ────────────────────────────────────────────────────── */}
      <Grid cols={{ base: 2, md: 3, lg: 5 }} gap="comfortable">
        <Card>
          <CardHeader compact title="Total Runs" />
          <CardContent>
            <Text size="body" weight="emphasis">
              {totalRuns}
            </Text>
          </CardContent>
        </Card>
        <Card>
          <CardHeader compact title="Success Rate" />
          <CardContent>
            <Text size="body" weight="emphasis">
              {successRate}
            </Text>
          </CardContent>
        </Card>
        <Card>
          <CardHeader compact title="Avg Duration" />
          <CardContent>
            <Text mono size="body" weight="emphasis">
              {avgDuration}
            </Text>
          </CardContent>
        </Card>
        <Card>
          <CardHeader compact title="Tokens (7d)" />
          <CardContent>
            <Text mono size="body" weight="emphasis">
              {agentUsage ? fmtTokens(agentUsage.total_tokens) : '—'}
            </Text>
          </CardContent>
        </Card>
        <Card>
          <CardHeader compact title="Spend (7d)" />
          <CardContent>
            <Text mono size="body" weight="emphasis">
              {agentUsage ? fmtUsd(agentUsage.estimated_cost_usd) : '—'}
            </Text>
          </CardContent>
        </Card>
      </Grid>

      {/* ── Two-column layout ──────────────────────────────────────────────
          Columns stretch to full width (default align-items: stretch). Do NOT
          set align="start" here: on mobile the stack is vertical, where
          align-items:flex-start makes the children size to their (wide) content
          instead of the column width — which pushes the table and prompt past
          the viewport. Stretch keeps them full-width so they wrap/scroll. */}
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="comfortable">
        {/* Left column: recent runs + system prompt */}
        <Stack gap="comfortable" grow>
          {/* Recent runs */}
          <DataTable
            data={recentRuns}
            columns={recentRunColumns}
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

          {/* System prompt — wrapping CodeBlock so long lines wrap within the
              column instead of forcing the page wider than the viewport. */}
          <Card>
            <CardHeader compact title="System Prompt" divider />
            <CardContent padded>
              <Stack gap="snug">
                <CodeBlock text={promptTruncated} wrap />
                {agent.systemPrompt.length > PROMPT_PREVIEW_LENGTH && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setPromptExpanded((prev) => !prev)}
                  >
                    {promptExpanded ? 'Show less' : 'Show full prompt'}
                  </Button>
                )}
              </Stack>
            </CardContent>
          </Card>
        </Stack>

        {/* Right column: budget + grant scopes + metadata */}
        <Stack gap="comfortable" style={{ flexShrink: 0, flexBasis: '300px' }}>
          {/* Budget usage — the agent's grant ceilings joined with live spend */}
          <AgentBudgetCard budget={budget} tenantCap={tenantCap} />

          {/* Token usage (7d) — sits with Budget; the spend total is in the
              stat row above. Compact label → value list. */}
          {usageRows && (
            <Card>
              <CardHeader compact title="Token Usage (7d)" divider />
              <CardContent padded>
                <Stack gap="snug">
                  {usageRows.map((row) => (
                    <Stack key={row.label} direction="horizontal" align="center" justify="between">
                      <Text size="supporting" color="muted">
                        {row.label}
                      </Text>
                      <Text mono tabular size="supporting">
                        {fmtTokens(row.value)}
                      </Text>
                    </Stack>
                  ))}
                </Stack>
              </CardContent>
            </Card>
          )}

          {/* Grant scopes */}
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

          {/* Metadata */}
          <Card>
            <CardHeader compact title="Metadata" divider />
            <CardContent padded>
              <Stack gap="snug">
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Created
                  </Text>
                  <Text size="supporting">{fmtDate(agent.createdAt)}</Text>
                </Stack>
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Last updated
                  </Text>
                  <Text size="supporting">{fmtDate(agent.updatedAt)}</Text>
                </Stack>
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Max concurrent / pod
                  </Text>
                  <Text mono size="supporting">
                    {agent.maxConcurrentRunsPerPod}
                  </Text>
                </Stack>
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Model
                  </Text>
                  <Badge variant="secondary" size="sm">
                    {agent.modelId}
                  </Badge>
                </Stack>
                <Stack direction="horizontal" align="center" justify="between">
                  <Text size="supporting" color="muted">
                    Identity model
                  </Text>
                  <Badge
                    variant={agent.identityMode === 'delegated' ? 'default' : 'secondary'}
                    size="sm"
                  >
                    {agent.identityMode === 'delegated' ? 'Delegated' : 'Autonomous'}
                  </Badge>
                </Stack>
              </Stack>
            </CardContent>
          </Card>
        </Stack>
      </Stack>
    </Stack>
  );
}
