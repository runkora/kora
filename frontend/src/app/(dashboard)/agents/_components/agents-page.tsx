'use client';

/** @tier 3 — agents-scoped. Main UI for the /agents list page. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  DataTable,
  Grid,
  Heading,
  Icon,
  SegmentedControl,
  Separator,
  Stack,
  Text,
  EmptyState,
} from '@orbit/hades';
import type { DataTableColumn } from '@orbit/hades';
import Link from 'next/link';
import { StatusBadge } from '@orbit/components';
import type { AgentInsightsResponse, AgentTypeUsage } from '@orbit/shared/api';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentsContext } from '../_context/agents-context';
import { useAgentsInsights } from '../_hooks/use-agents-insights';
import { StartRunModal } from './start-run-modal';
import { openBuilder } from '../_lib/builder-url';
import { DraftsButton, DraftsList, useDrafts } from './drafts-section';
import { AgentCard } from './agent-card';
import { AgentStatTiles } from './agent-stat-tiles';
import { AgentsSpendCard } from './agents-spend-card';
import { RunningNowCard } from './running-now-card';
import { DEFAULT_PERIOD, PERIOD_OPTIONS, PERIOD_TEXT } from '../_lib/spend';
import type { Period } from '../_lib/spend';
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

interface AgentsPageProps {
  /** The default window's insights, fetched by the server render so first paint has data. */
  initialInsights?: AgentInsightsResponse | null;
}

export function AgentsPage({ initialInsights }: AgentsPageProps) {
  const { agentTypes, liveRuns, runStats, status, error, refresh } = useAgentsContext();
  const router = useRouter();
  const { drafts, refresh: refreshDrafts } = useDrafts();

  const [startRunTarget, setStartRunTarget] = useState<AgentTypeRow | null>(null);
  const [globalRunOpen, setGlobalRunOpen] = useState(false);
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid');
  const [identityFilter, setIdentityFilter] = useState<'all' | 'autonomous' | 'delegated'>('all');
  const [draftsOpen, setDraftsOpen] = useState(false);
  const [period, setPeriod] = useState<Period>(DEFAULT_PERIOD);

  // The control picks the window. Without data for it the figures say so rather than show zeros.
  const { insights, status: windowStatus } = useAgentsInsights(period, agentTypes, initialInsights);
  const windowData = insights?.byPeriod[period];
  const usage = useMemo(() => windowData?.usage ?? {}, [windowData]);
  const periodText = PERIOD_TEXT[period].text;
  const unavailableText = windowStatus === 'loading' ? 'Loading' : 'Unavailable';
  const spendEmptyText =
    windowStatus === 'loading' ? 'Loading spend…' : 'Spend is unavailable right now.';

  // ── Figures ────────────────────────────────────────────────────────────────

  const live = runStats?.current;
  const runCounts = live
    ? {
        running: (live.by_state.active ?? 0) + (live.by_state.completing ?? 0),
        starting: live.by_state.starting ?? 0,
        queued: live.by_state.pending ?? 0,
        longestMs: live.longest_live_ms,
      }
    : null;

  // Without the window's outcomes the tiles fall back to the default window's run stats.
  const outcomes = windowData?.outcomes ?? {
    completed: runStats?.completed ?? 0,
    failed: runStats?.failed ?? 0,
  };
  const outcomesText = windowData?.outcomes ? periodText : PERIOD_TEXT[DEFAULT_PERIOD].text;

  const tokens = useMemo(() => {
    if (!windowData) return null;
    const values = Object.values(usage);
    const sum = (pick: (u: AgentTypeUsage) => number) => values.reduce((n, u) => n + pick(u), 0);
    return {
      cached: sum((u) => u.cache_read_tokens + u.cache_creation_tokens),
      input: sum((u) => u.prompt_tokens),
      output: sum((u) => u.completion_tokens),
    };
  }, [usage, windowData]);

  // The total includes deleted agents, so what the legend rows leave over is theirs.
  const spendByAgent = useMemo(
    () =>
      agentTypes
        .map((a) => ({ name: a.name, cost: usage[a.slug]?.estimated_cost_usd ?? 0 }))
        .filter((row) => row.cost > 0)
        .sort((x, y) => y.cost - x.cost),
    [agentTypes, usage],
  );
  const totalCost = windowData?.total ?? 0;
  const remainder = totalCost - spendByAgent.reduce((n, row) => n + row.cost, 0);
  // Under half a cent is rounding in the sums, not a deleted agent.
  const deletedCost = remainder > 0.005 ? remainder : 0;

  const agentCounts = {
    total: agentTypes.length,
    healthy: agentTypes.filter((a) => a.state === 'active').length,
    provisioning: agentTypes.filter((a) => a.state === 'provisioning').length,
    delegated: agentTypes.filter((a) => a.identityMode === 'delegated').length,
  };

  // ── Active run counts per agent type ──────────────────────────────────────

  const activeRunsByType = live?.live_by_agent_type ?? {};

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
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '10rem',
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
        gridWidth: '8rem',
        minWidth: '6.5rem',
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
        gridWidth: 'minmax(0, 1fr)',
        minWidth: '8rem',
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
        gridWidth: '6.5rem',
        minWidth: '5rem',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {live ? (live.live_by_agent_type[row.id] ?? 0) : '–'}
          </Text>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        gridWidth: '7rem',
        minWidth: '5.5rem',
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
        pinEnd: true,
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
    [live],
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
          <SegmentedControl
            aria-label="Period"
            value={period}
            onChange={(value) => setPeriod(value as Period)}
            options={PERIOD_OPTIONS}
          />
          <DraftsButton
            count={drafts.length}
            open={draftsOpen}
            onToggle={() => setDraftsOpen((open) => !open)}
          />
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

      {/* The caller's saved builder drafts (a draft has no status, runs, or
          spend, so it does not merge into the agent list) */}
      {draftsOpen && drafts.length > 0 && <DraftsList drafts={drafts} onChanged={refreshDrafts} />}

      {/* Headline figures beside the spend card */}
      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
        <AgentStatTiles
          agents={agentCounts}
          runs={runCounts}
          outcomes={outcomes}
          tokens={tokens}
          outcomesText={outcomesText}
          periodText={periodText}
          tokensNote={unavailableText}
        />
        <AgentsSpendCard
          agents={spendByAgent}
          deletedCost={deletedCost}
          total={totalCost}
          periodText={periodText}
          previousText={PERIOD_TEXT[period].previous}
          models={windowData?.models}
          trend={windowData?.trend}
          previousTotal={windowData?.previousTotal}
          budgets={insights?.budgets}
          emptyText={windowStatus === 'ready' ? undefined : spendEmptyText}
        />
      </Grid>

      <RunningNowCard
        runs={liveRuns}
        counts={live && runCounts ? { ...runCounts, total: live.live } : null}
      />

      {/* Section header */}
      <Stack direction="horizontal" align="baseline" gap="snug">
        <Text size="supporting" weight="emphasis" color="subtle">
          Agents
        </Text>
        <Text size="caption" color="subtle" mono tabular>
          {agentTypes.length}
        </Text>
      </Stack>

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
            <AgentCard
              key={agent.id}
              agent={agent}
              usage={usage[agent.slug]}
              totalCost={totalCost}
              liveRuns={live ? (activeRunsByType[agent.id] ?? 0) : null}
              period={period}
              usageNote={windowStatus === 'ready' ? undefined : unavailableText}
              onRun={setStartRunTarget}
            />
          ))}
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
