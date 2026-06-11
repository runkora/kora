'use client';

/** @tier 3 — agents/[slug]-scoped. Overview tab: stats, recent runs, system prompt, scopes, metadata. */

import { useState, useMemo } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  DataTable,
  Grid,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ScopeChip, StatusBadge } from '@orbit/components';
import { fmtDate } from '@orbit/utils';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';
import { AgentBudgetCard } from './agent-budget-card';

const PROMPT_PREVIEW_LENGTH = 300;

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

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
  const [promptExpanded, setPromptExpanded] = useState(false);

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

  const totalTokens = useMemo(() => {
    const sum = runs.reduce((acc, r) => acc + (r.tokensUsed ?? 0), 0);
    return sum > 0 ? formatTokens(sum) : '—';
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
        id: 'started',
        header: 'Started',
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
    <Stack gap="comfortable" paddingBlock="comfortable">
      {/* ── Stat cards ────────────────────────────────────────────────────── */}
      <Grid cols={{ base: 2, md: 4 }} gap="comfortable">
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
              {totalTokens}
            </Text>
          </CardContent>
        </Card>
      </Grid>

      {/* ── Two-column layout ────────────────────────────────────────────── */}
      <Stack direction={{ base: 'vertical', lg: 'horizontal' }} gap="comfortable" align="start">
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
            toolbarActions={
              <Text size="supporting" weight="emphasis">
                Recent Runs
              </Text>
            }
          />

          {/* System prompt */}
          <Card>
            <CardHeader compact title="System Prompt" divider />
            <CardContent padded>
              <Stack gap="snug">
                <Text mono preserveWhitespace size="supporting">
                  {promptTruncated}
                </Text>
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

        {/* Right column: budget + tool scopes + grant scopes + metadata */}
        <Stack gap="comfortable" style={{ flexShrink: 0, flexBasis: '300px' }}>
          {/* Budget usage — the agent's grant ceilings joined with live spend */}
          <AgentBudgetCard budget={budget} tenantCap={tenantCap} />

          {/* Tool scopes */}
          <Card>
            <CardHeader compact title="Tool Scopes" divider />
            <CardContent padded>
              {agent.toolScopes.length > 0 ? (
                <Stack direction="horizontal" gap="snug" wrap>
                  {agent.toolScopes.map((scope) => (
                    <ScopeChip key={scope} scope={scope} />
                  ))}
                </Stack>
              ) : (
                <Text size="supporting" color="muted">
                  No tool scopes configured.
                </Text>
              )}
            </CardContent>
          </Card>

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
              </Stack>
            </CardContent>
          </Card>
        </Stack>
      </Stack>
    </Stack>
  );
}
