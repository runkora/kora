import type {
  AgentOverviewResponse,
  AgentType,
  EffectiveAccess,
  RunSource,
  RunWithAttribution,
  Trigger,
  TriggerDelivery,
  WebhookSource,
} from '@runkora/shared/api';
import { dashboardPage } from '@runkora/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import {
  CODE_REVIEWER,
  ENVIRONMENTS,
  CONNECTIONS,
  MCP_CATALOG,
  MCP_SERVERS,
  MODELS_CATALOGUE,
  RAW_RUNS,
  DAY,
  ago,
  prSources,
} from '../../_components/agents-mock-data';
import { indexAgentTypes, toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';
import type { Period } from '../../_lib/spend';
import { mockOverview } from './agent-overview-mock-data';

// Mock data for the agent detail stories.

// ── Mock data specific to the detail page ────────────────────────────────────

const SOURCES: WebhookSource[] = [
  {
    id: 'src_github',
    tenant_id: 'ten_8f3a21',
    name: 'kora/platform on GitHub',
    provider: 'github',
    enabled: true,
    created_at: ago(30 * DAY),
    updated_at: ago(30 * DAY),
  },
];

const TRIGGERS: Trigger[] = [
  {
    id: 'trg_pr_opened',
    tenant_id: 'ten_8f3a21',
    kind: 'webhook',
    source_id: 'src_github',
    name: 'github pull_request.opened',
    agent_type_id: CODE_REVIEWER.id,
    agent_slug: CODE_REVIEWER.slug,
    event_type: 'pull_request.opened',
    match_condition: { 'repository.full_name': ['kora/platform'] },
    prompt_template: 'Review pull request {{pull_request.number}} in {{repository.full_name}}.',
    enabled: true,
    created_at: ago(28 * DAY),
    updated_at: ago(10 * DAY),
    max_parallel: 4,
    overflow: 'wait',
    max_queue_depth: 32,
    timeout_seconds: 30,
    dedupe_enabled: true,
    autopause_enabled: true,
    consecutive_failures: 0,
    pending_count: 0,
  },
  {
    id: 'trg_weekly_digest',
    tenant_id: 'ten_8f3a21',
    kind: 'scheduled',
    name: 'weekly review digest',
    agent_type_id: CODE_REVIEWER.id,
    agent_slug: CODE_REVIEWER.slug,
    prompt_template: 'Summarise the pull requests reviewed this week.',
    schedule_config: { cron_expression: '0 9 * * 1', timezone: 'Australia/Sydney' },
    enabled: true,
    created_at: ago(14 * DAY),
    updated_at: ago(14 * DAY),
    max_parallel: 1,
    overflow: 'refuse',
    max_queue_depth: 0,
    timeout_seconds: 30,
    dedupe_enabled: false,
    autopause_enabled: true,
    consecutive_failures: 0,
    pending_count: 0,
  },
];

const DELIVERIES: TriggerDelivery[] = [];

function effectiveAccess(agent: AgentType): EffectiveAccess {
  const envs = ENVIRONMENTS.filter((e) =>
    (agent.environments ?? []).some((a) => a.environment_id === e.id),
  );
  const own = agent.connectivity.hosts ?? [];
  const envHosts = [...new Set(envs.flatMap((e) => e.egress_hosts))].sort();
  const connectorEgress = (agent.connectors ?? []).map((c) => ({
    connection: c.connection,
    adapter: c.connection,
    hosts: c.connection === 'github' ? ['api.github.com'] : ['slack.com'],
  }));
  return {
    open: agent.connectivity.mode === 'open',
    own_hosts: own,
    environments: envs.map((e) => ({ id: e.id, name: e.name, hosts: e.egress_hosts })),
    environment_hosts: envHosts,
    connector_egress: connectorEgress,
    connectors: agent.connectors ?? [],
    hosts: [...new Set([...own, ...envHosts, ...connectorEgress.flatMap((c) => c.hosts)])].sort(),
  };
}

/**
 * Every route the detail page and its four tabs call. `overview` answers the
 * Overview's period requests by slug and period.
 */
function detailRoutes(
  agent: AgentType,
  overview: (slug: string, period: Period) => AgentOverviewResponse,
  runs: RunWithAttribution[],
): MockRoutes {
  return {
    'GET /api/agents/types/overview/:slug': ({ url }: MockRequest) =>
      overview(agent.slug, url.searchParams.get('period') as Period),
    'GET /api/agents/types/:id/effective-access': { data: effectiveAccess(agent) },
    'POST /api/agents/types/effective-access': { data: effectiveAccess(agent) },
    'GET /api/agents/types/:id/runs': { data: runs, next_cursor: null },
    'GET /api/agents/types/:id': { data: agent },
    'GET /api/models': { data: MODELS_CATALOGUE },
    'GET /api/agents/connectors': { data: CONNECTIONS },
    'GET /api/agents/mcp-tools': { servers: MCP_SERVERS, catalog: MCP_CATALOG },
    'GET /api/agents/environments': { data: ENVIRONMENTS },
    'GET /api/triggers': {
      data: {
        triggers: TRIGGERS.filter((t) => t.agent_type_id === agent.id),
        stats: null,
      },
    },
    'GET /api/trigger-sources': { data: SOURCES },
    'GET /api/trigger-deliveries': { data: DELIVERIES },
  };
}

// ── Runs ─────────────────────────────────────────────────────────────────────

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const COSTS = [0.42, 1.87, 0.31, 0.27, 0.03, 0.12, 0.66, 0.09];

const SLACK: RunSource = {
  kind: 'trigger',
  url: 'https://app.slack.com/client/T0123/C0123/thread/C0123-1700000000.000100',
  label: 'Slack thread',
  provider: 'slack',
};
const ALERT: RunSource = {
  kind: 'trigger',
  url: 'https://prometheus.example.com/graph?g0.expr=p95_latency',
  label: 'Alert',
  provider: 'alertmanager',
};

/** Runs from Slack, an alert with a long trigger name, a schedule, a manual fire, an event and a person. */
function extraRuns(agent: AgentType): RunWithAttribution[] {
  const base: RunWithAttribution = {
    id: '',
    tenant_id: 'ten_8f3a21',
    agent_type_id: agent.id,
    state: 'completed',
    pod_ordinal: 0,
    created_by: 'trigger',
    created_at: '',
    updated_at: '',
    started_at: null,
    completed_at: null,
  };
  const at = (startedAgo: number, lasted: number | null) => ({
    started_at: ago(startedAgo),
    created_at: ago(startedAgo),
    updated_at: ago(startedAgo),
    completed_at: lasted === null ? null : ago(startedAgo - lasted),
  });
  return [
    {
      ...base,
      id: 'run_0101',
      state: 'idle',
      ...at(12 * MINUTE, null),
      summary: 'Answering a question in #platform-eng about the retry budget for webhooks.',
      started_by: {
        trigger_id: 'trg_slack',
        trigger_name: 'slack review request',
        trigger_kind: 'webhook',
      },
      sources: [SLACK],
    },
    {
      ...base,
      id: 'run_0102',
      ...at(2 * HOUR, 3 * MINUTE),
      summary: 'Checked the p95 latency alert on the inference gateway; it cleared on its own.',
      started_by: {
        trigger_id: 'trg_alert',
        trigger_name: 'prometheus inference-gateway p95 latency above 2s for five minutes',
        trigger_kind: 'webhook',
      },
      sources: [ALERT],
    },
    {
      ...base,
      id: 'run_0104',
      ...at(4 * HOUR, 5 * MINUTE),
      summary: 'Summarised the 14 pull requests reviewed this week for the platform team.',
      started_by: {
        trigger_id: 'trg_weekly_digest',
        trigger_name: 'weekly review digest',
        trigger_kind: 'scheduled',
      },
    },
    {
      ...base,
      id: 'run_0105',
      ...at(3 * HOUR, 4 * MINUTE),
      summary: 'Reviewed the release branch before the 0.12 cut.',
      started_by: {
        trigger_id: 'trg_release_check',
        trigger_name: 'release branch check',
        trigger_kind: 'manual',
      },
    },
    {
      ...base,
      id: 'run_0106',
      ...at(4.5 * HOUR, 6 * MINUTE),
      summary: 'Looked into a failed review run and found the sandbox ran out of disk.',
      started_by: {
        trigger_id: 'trg_run_failed',
        trigger_name: 'on review run failure',
        trigger_kind: 'event',
      },
    },
    {
      ...base,
      id: 'run_0103',
      ...at(5 * HOUR, 8 * MINUTE),
      created_by: 'usr_dana',
      created_by_name: 'Dana Lee',
      summary: 'Re-reviewed pull request 401 after the force push.',
    },
  ];
}

/** The run list a story shows: the default, or one edge case. */
export type RunsData = 'default' | 'long' | 'many' | 'none' | 'empty';

const LONG =
  'Reviewing pull request 412, which moves session storage from the in-process cache to Redis, adds a migration for the existing session rows, and rewrites the refresh token rotation so concurrent refreshes from two tabs settle on one token';

/** The agent's runs, newest first with costs, plus the extras, shaped for `set`. */
export function runsFor(agent: AgentType, set: RunsData = 'default'): RunWithAttribution[] {
  if (set === 'empty') return [];
  const runs = [...RAW_RUNS.filter((r) => r.agent_type_id === agent.id), ...extraRuns(agent)]
    .sort((a, b) => (b.started_at ?? '').localeCompare(a.started_at ?? ''))
    .map((r, i) => ({
      ...r,
      usage: {
        cost_usd: COSTS[i % COSTS.length] ?? 0,
        input_tokens: 0,
        output_tokens: 0,
        request_count: 1,
      },
    }));
  if (set === 'long')
    return runs.map((r) => (r.summary ? { ...r, summary: `${LONG} (${r.id}).` } : r));
  if (set === 'none') return runs.map((r) => ({ ...r, sources: [] }));
  if (set === 'many') {
    const many = [
      ...prSources('example-org/platform', 412, 'feat/redis-sessions', 'main', 'release/0.12'),
      SLACK,
      ALERT,
    ];
    return runs.map((r, i) => (i === 0 ? { ...r, sources: many } : r));
  }
  return runs;
}

export const withRoutes = (
  agent: AgentType,
  overview: (slug: string, period: Period) => AgentOverviewResponse = mockOverview,
  runs: RunWithAttribution[] = runsFor(agent),
) => [dashboardPage(detailRoutes(agent, overview, runs))];

export const rowFor = (agent: AgentType): AgentTypeRow => toAgentTypeRow(agent);

export const runRowsFor = (agent: AgentType, set: RunsData = 'default'): RunRow[] => {
  const byId = indexAgentTypes([rowFor(agent)]);
  return runsFor(agent, set).map((r) => toRunRow(r, byId));
};
