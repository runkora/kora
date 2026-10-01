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
  SegmentedControl,
  Separator,
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
import { openBuilder } from '../_lib/builder-url';
import { DraftsSection } from './drafts-section';
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
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid');
  const [identityFilter, setIdentityFilter] = useState<'all' | 'autonomous' | 'delegated'>('all');

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

  const totalTokensUsed = useMemo(() => {
    const usageSum = Object.values(agentUsage).reduce((acc, u) => acc + u.total_tokens, 0);
    return usageSum > 0 ? fmtTokens(usageSum) : '—';
  }, [agentUsage]);

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

  // ── Identity filter (list view) ────────────────────────────────────────────

  const identityCounts = useMemo(
    () => ({
      all: agentTypes.length,
      autonomous: agentTypes.filter((a) => a.identityMode === 'autonomous').length,
      delegated: agentTypes.filter((a) => a.identityMode === 'delegated').length,
    }),
    [agentTypes],
  );

  const filteredAgentTypes = useMemo(() => {
    if (identityFilter === 'all') return agentTypes;
    return agentTypes.filter((a) => a.identityMode === identityFilter);
  }, [agentTypes, identityFilter]);

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
          <Stack direction="horizontal" align="center" gap="tight" wrap>
            <StatusBadge status={statusMap[row.state] ?? 'unknown'} label={labelMap[row.state]} />
            {row.identityMode === 'delegated' && (
              <Badge variant="default" size="sm">
                Delegated
              </Badge>
            )}
          </Stack>
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
        gridWidth: '6rem',
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
          <Button onClick={() => openBuilder()}>
            <Icon name="plus" size="xs" />
            New Agent
          </Button>
          <Separator orientation="vertical" style={{ height: '1.5rem', alignSelf: 'center' }} />
          <Button
            variant={viewMode === 'grid' ? 'default' : 'ghost'}
            size="icon-sm"
            aria-label="Grid view"
            onClick={() => setViewMode('grid')}
          >
            <Icon name="layout-grid" size="xs" />
          </Button>
          <Button
            variant={viewMode === 'list' ? 'default' : 'ghost'}
            size="icon-sm"
            aria-label="List view"
            onClick={() => setViewMode('list')}
          >
            <Icon name="layout-list" size="xs" />
          </Button>
        </Stack>
      </Stack>

      {/* The caller's saved builder drafts (own section — a draft has no
          status, runs, or spend, so it does not merge into the agent table) */}
      <DraftsSection />

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
          action={<Button onClick={() => openBuilder()}>New Agent</Button>}
        />
      ) : viewMode === 'grid' ? (
        /* ── Grid view ─────────────────────────────────────────────────────── */
        <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
          {agentTypes.map((agent) => (
            <Card
              key={agent.id}
              column
              role="button"
              onClick={() => router.push(APP_ROUTES.agents.detail(agent.slug))}
              style={{ cursor: 'pointer' }}
            >
              <CardHeader
                leadingIcon="bot"
                title={agent.name}
                description={
                  <Text mono size="caption" color="muted">
                    {agent.slug}
                  </Text>
                }
                action={
                  <Stack direction="horizontal" align="center" gap="tight">
                    {agent.identityMode === 'delegated' && (
                      <Badge variant="default" size="sm">
                        Delegated
                      </Badge>
                    )}
                    <StatusBadge
                      status={statusMap[agent.state] ?? 'unknown'}
                      label={labelMap[agent.state]}
                    />
                  </Stack>
                }
                divider
              />

              <CardContent padded grow>
                <Stack gap="snug">
                  {/* Description — 2 lines truncated */}
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

                  {/* Meta row: framework badge. The identity model
                      sits in the header next to status, where posture signals live. */}
                  <Stack direction="horizontal" gap="tight" wrap>
                    <Badge variant={frameworkVariantMap[agent.frameworkKey] ?? 'outline'} size="sm">
                      {agent.framework}
                    </Badge>
                  </Stack>

                  {/* Per-agent 7-day cost + tokens (when usage exists) */}
                  {agentUsage[agent.slug] && (
                    <Text size="caption" color="muted">
                      {fmtUsd(agentUsage[agent.slug].estimated_cost_usd)} ·{' '}
                      {fmtTokens(agentUsage[agent.slug].total_tokens)} tokens · 7d
                    </Text>
                  )}
                </Stack>
              </CardContent>

              <CardFooter compact justify="between" onClick={(e) => e.stopPropagation()}>
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
          ))}

          {/* Create new agent card */}
          <Stack
            role="button"
            align="center"
            gap="tight"
            onClick={() => openBuilder()}
            style={{
              cursor: 'pointer',
              minHeight: '160px',
              borderWidth: '1.5px',
              borderStyle: 'dashed',
              borderColor: 'var(--color-border)',
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
        </Grid>
      ) : (
        /* ── List view ─────────────────────────────────────────────────────── */
        <DataTable
          key={identityFilter}
          data={filteredAgentTypes}
          columns={columns}
          getRowKey={(row) => row.id}
          noun="agents"
          searchPlaceholder="Search agents…"
          searchFn={agentTypeMatchesSearch}
          defaultSortId="name"
          defaultSortDir="asc"
          quickFilters={
            <SegmentedControl
              aria-label="Filter agents by identity mode"
              value={identityFilter}
              onChange={(v) => setIdentityFilter(v as typeof identityFilter)}
              options={[
                { value: 'all', label: 'All', count: identityCounts.all },
                { value: 'autonomous', label: 'Autonomous', count: identityCounts.autonomous },
                { value: 'delegated', label: 'Delegated', count: identityCounts.delegated },
              ]}
            />
          }
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
