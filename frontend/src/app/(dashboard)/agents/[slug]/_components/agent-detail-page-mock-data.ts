import type {
  AgentOverviewResponse,
  AgentType,
  EffectiveAccess,
  Trigger,
  TriggerDelivery,
  WebhookSource,
} from '@orbit/shared/api';
import { dashboardPage } from '@orbit/storybook/dashboard-frame';
import type { MockRequest, MockRoutes } from '@orbit/storybook/mock-api';
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
  runsForAgent,
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
): MockRoutes {
  return {
    'GET /api/agents/types/overview/:slug': ({ url }: MockRequest) =>
      overview(agent.slug, url.searchParams.get('period') as Period),
    'GET /api/agents/types/:id/effective-access': { data: effectiveAccess(agent) },
    'POST /api/agents/types/effective-access': { data: effectiveAccess(agent) },
    'GET /api/agents/types/:id/runs': { data: runsForAgent(agent.id), next_cursor: null },
    'GET /api/agents/types/:id': { data: agent },
    'GET /api/models': { data: MODELS_CATALOGUE },
    'GET /api/agents/connectors': { data: CONNECTIONS },
    'GET /api/agents/mcp-tools': { servers: MCP_SERVERS, catalog: MCP_CATALOG },
    'GET /api/agents/environments': { data: ENVIRONMENTS },
    'GET /api/triggers': { data: TRIGGERS.filter((t) => t.agent_type_id === agent.id) },
    'GET /api/trigger-sources': { data: SOURCES },
    'GET /api/trigger-deliveries': { data: DELIVERIES },
  };
}

export const withRoutes = (
  agent: AgentType,
  overview: (slug: string, period: Period) => AgentOverviewResponse = mockOverview,
) => [dashboardPage(detailRoutes(agent, overview))];

export const rowFor = (agent: AgentType): AgentTypeRow => toAgentTypeRow(agent);

export const runRowsFor = (agent: AgentType): RunRow[] => {
  const byId = indexAgentTypes([rowFor(agent)]);
  return RAW_RUNS.filter((r) => r.agent_type_id === agent.id).map((r) => toRunRow(r, byId));
};
