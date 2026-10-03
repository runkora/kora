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
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
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
import { RunningNowCard, isLiveRun } from './running-now-card';
import { PERIOD_OPTIONS, PERIOD_TEXT } from '../_lib/spend';
import type { AgentsInsights, Period } from '../_lib/spend';
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
  /**
   * The optional parts of the page; see `AgentsInsights`. Left out, the page
   * loads them itself from the BFF, so only stories and tests pass it.
   */
  insights?: AgentsInsights;
  /** The 7-day insights the server render fetched, so first paint has real data. */
  initialInsights?: AgentInsightsResponse | null;
}

export function AgentsPage({ insights, initialInsights }: AgentsPageProps) {
  const { agentTypes, recentRuns, status, error, refresh } = useAgentsContext();
  const router = useRouter();
  const { drafts, refresh: refreshDrafts } = useDrafts();

  const [startRunTarget, setStartRunTarget] = useState<AgentTypeRow | null>(null);
  const [globalRunOpen, setGlobalRunOpen] = useState(false);
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid');
  const [identityFilter, setIdentityFilter] = useState<'all' | 'autonomous' | 'delegated'>('all');
  const [draftsOpen, setDraftsOpen] = useState(false);
  const [period, setPeriod] = useState<Period>('7d');

  // ── The window the period figures cover ────────────────────────────────────
  // The control picks the window. The 7-day window comes with the server
  // render, and any other period is fetched when first selected. Without data
  // for the window the figures say so rather than show zeros.

  const live = useAgentsInsights(period, agentTypes, insights === undefined, initialInsights);
  const data = insights ?? live.insights;
  const windowStatus = insights ? 'ready' : live.status;
  const windowData = data?.byPeriod?.[period];
  const usage = useMemo(() => windowData?.usage ?? {}, [windowData]);
  const periodText = PERIOD_TEXT[period].text;
  const unavailableText = windowStatus === 'loading' ? 'Loading' : 'Unavailable';
  const spendEmptyText =
    windowStatus === 'loading' ? 'Loading spend…' : 'Spend is unavailable right now.';

  // ── Figures ────────────────────────────────────────────────────────────────

  const liveRuns = useMemo(() => recentRuns.filter(isLiveRun), [recentRuns]);

  const runCounts = useMemo(() => {
    const count = (...states: string[]) => liveRuns.filter((r) => states.includes(r.state)).length;
    return {
      running: count('active', 'completing'),
      starting: count('starting'),
      queued: count('pending'),
      longestMs: Math.max(0, ...liveRuns.map((r) => r.durationMs ?? 0)),
    };
  }, [liveRuns]);

  const outcomes = windowData?.outcomes ?? {
    completed: recentRuns.filter((r) => r.state === 'completed').length,
    failed: recentRuns.filter((r) => r.state === 'failed').length,
  };

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

  // The window's spend is the total over every agent, deleted ones included. The
  // legend lists the current agents, and what is left of the total is the
  // deleted agents', so the rows add up to it. A delegated agent's usage is the
  // caller's own unless the caller sees every row.
  const ownDelegated = windowData?.delegatedOwnOnly ?? false;
  const spendByAgent = useMemo(
    () =>
      agentTypes
        .map((a) => ({
          name: ownDelegated && a.identityMode === 'delegated' ? `${a.name} (your usage)` : a.name,
          cost: usage[a.slug]?.estimated_cost_usd ?? 0,
        }))
        .filter((row) => row.cost > 0)
        .sort((x, y) => y.cost - x.cost),
    [agentTypes, usage, ownDelegated],
  );
  const totalCost = windowData?.total ?? 0;
  const deletedCost = Math.max(totalCost - spendByAgent.reduce((n, row) => n + row.cost, 0), 0);

  const agentCounts = {
    total: agentTypes.length,
    healthy: agentTypes.filter((a) => a.state === 'active').length,
    provisioning: agentTypes.filter((a) => a.state === 'provisioning').length,
    delegated: agentTypes.filter((a) => a.identityMode === 'delegated').length,
  };

  // ── Active run counts per agent type ──────────────────────────────────────

  const activeRunsByType = useMemo(() => {
    const counts = new Map<string, number>();
    for (const run of liveRuns) {
      counts.set(run.agentTypeId, (counts.get(run.agentTypeId) ?? 0) + 1);
    }
    return counts;
  }, [liveRuns]);

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
            {activeRunsByType.get(row.id) ?? 0}
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
          {(insights ? insights.byPeriod : true) && (
            <SegmentedControl
              aria-label="Period"
              value={period}
              onChange={(value) => setPeriod(value as Period)}
              options={PERIOD_OPTIONS}
            />
          )}
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
          outcomesText={windowData?.outcomes ? periodText : 'recent runs'}
          periodText={periodText}
          tokensNote={unavailableText}
        />
        <AgentsSpendCard
          agents={spendByAgent}
          deletedCost={deletedCost > 1e-9 ? deletedCost : 0}
          total={totalCost}
          periodText={periodText}
          previousText={PERIOD_TEXT[period].previous}
          models={windowData?.models}
          trend={windowData?.trend}
          previousTotal={windowData?.previousTotal}
          budgets={data?.budgets}
          tenantBudget={data?.tenantBudget}
          emptyText={windowStatus === 'ready' ? undefined : spendEmptyText}
        />
      </Grid>

      <RunningNowCard runs={liveRuns} />

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
              liveRuns={activeRunsByType.get(agent.id) ?? 0}
              period={period}
              usageNote={windowStatus === 'ready' ? undefined : unavailableText}
              yourUsage={ownDelegated && agent.identityMode === 'delegated'}
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
