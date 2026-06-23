'use client';

/** @tier 3 — agents/[slug]-scoped. Overview tab: stats, recent runs, system prompt, scopes, metadata. */

import { useState, useMemo, useEffect } from 'react';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CodeBlock,
  DataTable,
  Grid,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { ScopeChip, StatusBadge } from '@orbit/components';
import { fmtDate, fmtUsd, fmtTokens } from '@orbit/utils';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { fetchAgentTypeUsage } from '../_services/agent-detail-api';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';
import type { BudgetPrincipalStatus, TenantCapStatus } from '../../../model-gateway/_types';
import { AgentBudgetCard } from './agent-budget-card';
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
  const [promptExpanded, setPromptExpanded] = useState(false);
  const [agentUsage, setAgentUsage] = useState<AgentTypeUsage | null>(null);

  // Fetch 7-day token breakdown and spend from the inference gateway.
  useEffect(() => {
    if (!agent) return;
    void fetchAgentTypeUsage(agent.id).then(({ usage }) => {
      if (usage) setAgentUsage(usage);
    });
  }, [agent]);

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
            {row.tokensUsed ? fmtTokens(row.tokensUsed) : '—'}
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
        <Card column>
          <CardHeader compact title="Tokens (7d)" />
          <CardContent>
            <Text mono size="body" weight="emphasis">
              {agentUsage ? fmtTokens(agentUsage.total_tokens) : '—'}
            </Text>
            {agentUsage && (
              <Stack gap="none" paddingBlockStart="snug">
                <Stack direction="horizontal" align="center" justify="between" paddingBlock="tight">
                  <Text size="supporting" color="muted">
                    Input
                  </Text>
                  <Text size="supporting" mono tabular>
                    {fmtTokens(agentUsage.prompt_tokens)}
                  </Text>
                </Stack>
                <Separator />
                <Stack direction="horizontal" align="center" justify="between" paddingBlock="tight">
                  <Text size="supporting" color="muted">
                    Output
                  </Text>
                  <Text size="supporting" mono tabular>
                    {fmtTokens(agentUsage.completion_tokens)}
                  </Text>
                </Stack>
                <Separator />
                <Stack direction="horizontal" align="center" justify="between" paddingBlock="tight">
                  <Text size="supporting" color="muted">
                    Cache read
                  </Text>
                  <Text size="supporting" mono tabular>
                    {fmtTokens(agentUsage.cache_read_tokens)}
                  </Text>
                </Stack>
                <Separator />
                <Stack direction="horizontal" align="center" justify="between" paddingBlock="tight">
                  <Text size="supporting" color="muted">
                    Cache write
                  </Text>
                  <Text size="supporting" mono tabular>
                    {fmtTokens(agentUsage.cache_creation_tokens)}
                  </Text>
                </Stack>
              </Stack>
            )}
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
            toolbarActions={
              <Text size="supporting" weight="emphasis">
                Recent Runs
              </Text>
            }
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
