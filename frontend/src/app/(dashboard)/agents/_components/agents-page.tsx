'use client';

/** @tier 3 — agents-scoped. Main UI for the /agents list page. */

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  DataTable,
  Grid,
  Heading,
  Icon,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import Link from 'next/link';
import { EmptyState, MetricCard, StatusBadge } from '@orbit/components';
import { fmtTokens, fmtUsd } from '@orbit/utils';
import type { AgentTypeUsage } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentsContext } from '../_context/agents-context';
import { fetchAgentTypesUsage } from '../_services/agents-api';
import { StartRunModal } from './start-run-modal';
import { formatRelativeTime } from '../_types';
import type { AgentTypeRow } from '../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

function agentTypeMatchesSearch(row: AgentTypeRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.name.toLowerCase().includes(lower) ||
    row.slug.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower) ||
    row.framework.toLowerCase().includes(lower)
  );
}

const statusMap: Record<string, 'healthy' | 'degraded' | 'error'> = {
  active: 'healthy',
  provisioning: 'degraded',
  deleting: 'error',
};

const labelMap: Partial<Record<string, string>> = {
  provisioning: 'Provisioning',
  deleting: 'Deleting',
};

const frameworkVariantMap: Record<string, 'default' | 'outline' | 'secondary'> = {
  'claude-code': 'default',
  codex: 'outline',
  opencode: 'secondary',
};

// ── Component ─────────────────────────────────────────────────────────────────

export function AgentsPage() {
  const { agentTypes, recentRuns, status, error, refresh } = useAgentsContext();
  const router = useRouter();

  const [startRunTarget, setStartRunTarget] = useState<AgentTypeRow | null>(null);
  const [globalRunOpen, setGlobalRunOpen] = useState(false);

  // ── Metric card computations ───────────────────────────────────────────────

  const activeAgentCount = useMemo(
    () => agentTypes.filter((a) => a.state === 'active').length,
    [agentTypes],
  );

  const activeRunCount = useMemo(
    () =>
      recentRuns.filter((r) => ['active', 'starting', 'pending', 'completing'].includes(r.state))
        .length,
    [recentRuns],
  );

  const completedTodayCount = recentRuns.filter(
    (r) => r.state === 'completed' && r.completedAt !== null,
  ).length;

  // ── 7-day cost/usage per agent type (inference gateway) ────────────────────
  // Fetched client-side so the list can show total + per-agent spend without
  // blocking the initial render on the usage aggregate.

  const [agentUsage, setAgentUsage] = useState<Record<string, AgentTypeUsage>>({});

  useEffect(() => {
    let cancelled = false;
    void fetchAgentTypesUsage().then(({ byAgentType }) => {
      if (!cancelled) setAgentUsage(byAgentType);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const totalCost = useMemo(() => {
    const values = Object.values(agentUsage);
    if (values.length === 0) return '—';
    return fmtUsd(values.reduce((acc, u) => acc + u.estimated_cost_usd, 0));
  }, [agentUsage]);

  // Tokens Used: prefer the accurate inference_usage total (same source as cost
  // + per-agent breakdown); fall back to the runs-based estimate when the usage
  // aggregate has no data yet.
  const totalTokensUsed = useMemo(() => {
    const usageSum = Object.values(agentUsage).reduce((acc, u) => acc + u.total_tokens, 0);
    if (usageSum > 0) return fmtTokens(usageSum);
    const runsSum = recentRuns.reduce((acc, r) => acc + (r.tokensUsed ?? 0), 0);
    return runsSum > 0 ? fmtTokens(runsSum) : '—';
  }, [agentUsage, recentRuns]);

  // ── Active run counts per agent type ──────────────────────────────────────

  const activeRunsByType = useMemo(() => {
    const counts = new Map<string, number>();
    for (const run of recentRuns) {
      if (['active', 'starting', 'pending', 'completing'].includes(run.state)) {
        counts.set(run.agentTypeId, (counts.get(run.agentTypeId) ?? 0) + 1);
      }
    }
    return counts;
  }, [recentRuns]);

  // ── Table columns ─────────────────────────────────────────────────────────

  const columns = useMemo<DataTableColumn<AgentTypeRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Agent',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text size="supporting" weight="emphasis">
              {row.name}
            </Text>
            <Text mono size="caption" color="muted">
              {row.slug}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'framework',
        header: 'Framework',
        locked: true,
        cell: (row) => (
          <Badge variant={frameworkVariantMap[row.frameworkKey] ?? 'outline'} size="sm">
            {row.framework}
          </Badge>
        ),
      },
      {
        id: 'state',
        header: 'Status',
        locked: true,
        cell: (row) => (
          <StatusBadge status={statusMap[row.state] ?? 'unknown'} label={labelMap[row.state]} />
        ),
      },
      {
        id: 'activeRuns',
        header: 'Active Runs',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {activeRunsByType.get(row.id) ?? 0}
          </Text>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.createdAt,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {formatRelativeTime(row.createdAt)}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '6rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" onClick={(e) => e.stopPropagation()}>
            <Button
              variant="ghost"
              size="icon-sm"
              title="Start run"
              aria-label={`Start run for ${row.name}`}
              onClick={() => setStartRunTarget(row)}
            >
              <Icon name="play" size="2xs" />
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Settings for ${row.name}`} asChild>
              <Link href={APP_ROUTES.agents.detail(row.slug)}>
                <Icon name="settings" size="2xs" />
              </Link>
            </Button>
          </Stack>
        ),
      },
    ],
    [activeRunsByType],
  );

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Heading variant="title">Agents</Heading>
          <Text size="supporting" color="muted">
            Manage agent definitions and start runs.
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="snug" align="center">
          <Button variant="outline" onClick={() => setGlobalRunOpen(true)}>
            <Icon name="play" size="xs" />
            New Run
          </Button>
          <Button asChild>
            <Link href={APP_ROUTES.agents.new}>
              <Icon name="plus" size="xs" />
              New Agent
            </Link>
          </Button>
        </Stack>
      </Stack>

      {/* Metric cards */}
      <Grid cols={{ base: 2, sm: 3, lg: 5 }} gap="comfortable">
        <MetricCard icon={<Icon name="bot" />} label="Active Agents" value={activeAgentCount} />
        <MetricCard icon={<Icon name="activity" />} label="Active Runs" value={activeRunCount} />
        <MetricCard
          icon={<Icon name="check-circle" />}
          label="Completed Runs"
          value={completedTodayCount}
        />
        <MetricCard icon={<Icon name="zap" />} label="Tokens Used" value={totalTokensUsed} />
        <MetricCard icon={<Icon name="dollar-sign" />} label="Total Cost (7d)" value={totalCost} />
      </Grid>

      {/* Section header */}
      <Text size="supporting" weight="emphasis" color="subtle">
        Agents
      </Text>

      {/* Agent types view — show EmptyState when there are no agents and no error */}
      {status !== 'error' && agentTypes.length === 0 && status !== 'loading' ? (
        <EmptyState
          icon="bot"
          title="No agents yet"
          description="Create your first agent definition to start running AI tasks."
          action={
            <Button asChild>
              <Link href={APP_ROUTES.agents.new}>New Agent</Link>
            </Button>
          }
        />
      ) : (
        <DataTable
          data={agentTypes}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="agents"
          searchPlaceholder="Search agents…"
          searchFn={agentTypeMatchesSearch}
          defaultSortId="name"
          defaultSortDir="asc"
          error={
            status === 'error' ? (
              <Stack align="center" paddingBlock="comfortable">
                <Text size="supporting" color="muted">
                  {error ?? 'Failed to load agents.'}
                </Text>
                <Button variant="outline" size="sm" onClick={() => void refresh()}>
                  Try again
                </Button>
              </Stack>
            ) : undefined
          }
          emptyMessage="No agents yet."
          filteredEmptyMessage="No agents match your search."
          onRowClick={(row) => router.push(APP_ROUTES.agents.detail(row.slug))}
          mobileCard={(agent) => (
            <Card column>
              <CardHeader
                leadingIcon="bot"
                title={agent.name}
                description={
                  <Text mono size="caption" color="muted">
                    {agent.slug}
                  </Text>
                }
                action={
                  <StatusBadge
                    status={statusMap[agent.state] ?? 'unknown'}
                    label={labelMap[agent.state]}
                  />
                }
                divider
              />

              <CardContent padded grow>
                <Stack gap="snug">
                  <Text
                    size="supporting"
                    color="muted"
                    style={{
                      display: '-webkit-box',
                      WebkitLineClamp: 2,
                      WebkitBoxOrient: 'vertical',
                      overflow: 'hidden',
                    }}
                  >
                    {agent.description || 'No description.'}
                  </Text>

                  <Stack direction="horizontal" gap="tight" wrap>
                    <Badge variant={frameworkVariantMap[agent.frameworkKey] ?? 'outline'} size="sm">
                      {agent.framework}
                    </Badge>
                    <Badge variant="secondary" size="sm">
                      {agent.toolScopes.length} tools
                    </Badge>
                  </Stack>

                  {/* Per-agent 7-day cost + tokens (when usage exists) */}
                  {agentUsage[agent.id] && (
                    <Text size="caption" color="muted">
                      {fmtUsd(agentUsage[agent.id].estimated_cost_usd)} ·{' '}
                      {fmtTokens(agentUsage[agent.id].total_tokens)} tokens · 7d
                    </Text>
                  )}
                </Stack>
              </CardContent>

              <CardFooter compact justify="between">
                <Text size="caption" color="muted">
                  {activeRunsByType.get(agent.id) ?? 0} active
                </Text>
                <Stack direction="horizontal" gap="tight">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="Start run"
                    aria-label={`Start run for ${agent.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      setStartRunTarget(agent);
                    }}
                  >
                    <Icon name="play" size="2xs" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Settings for ${agent.name}`}
                    asChild
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Link href={APP_ROUTES.agents.detail(agent.slug)}>
                      <Icon name="settings" size="2xs" />
                    </Link>
                  </Button>
                </Stack>
              </CardFooter>
            </Card>
          )}
          mobileCardFooter={
            <Stack
              role="button"
              align="center"
              gap="tight"
              onClick={() => router.push(APP_ROUTES.agents.new)}
              style={{
                cursor: 'pointer',
                minHeight: '120px',
                borderWidth: '1.5px',
                borderStyle: 'dashed',
                borderColor: 'oklch(var(--color-border) / 1)',
                borderRadius: '8px',
                justifyContent: 'center',
                padding: '1.5rem',
              }}
            >
              <Icon name="plus" size="sm" />
              <Text size="supporting" color="muted">
                Create new agent
              </Text>
            </Stack>
          }
        />
      )}

      {/* Row-level start run modal (agent pre-selected) */}
      <StartRunModal
        open={startRunTarget !== null}
        agentType={startRunTarget}
        onClose={() => setStartRunTarget(null)}
        onStarted={(runId, agentSlug) => {
          setStartRunTarget(null);
          router.push(APP_ROUTES.agents.run(agentSlug, runId));
        }}
      />

      {/* Global start run modal (user picks agent) */}
      <StartRunModal
        open={globalRunOpen}
        agentType={null}
        agentTypes={agentTypes}
        onClose={() => setGlobalRunOpen(false)}
        onStarted={(runId, agentSlug) => {
          setGlobalRunOpen(false);
          router.push(APP_ROUTES.agents.run(agentSlug, runId));
        }}
      />
    </Stack>
  );
}
