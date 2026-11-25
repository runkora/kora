/**
 * Mock data for the Runs page stories: runs as GET /api/v1/runs returns them, the BFF route
 * that pages and filters them as the server does, and run stream messages. One builder, a few
 * parameters.
 */

import type { MockRequest, MockRoutes } from '@runkora/storybook/mock-api';
import type { MockStreamEvent } from '@runkora/storybook/mock-event-source';
import type {
  RunSource,
  RunState,
  RunStreamMessage,
  RunWithAttribution,
  TriggerKind,
} from '@runkora/shared/api';
import { AGENT_TYPES } from '../../agents/_components/agents-mock-data';
import { RUN_CREATED } from '../_lib/runs-feed';

/** The signed-in person in the stories, whose runs Mine keeps. */
const ME = { id: 'usr_dev', name: 'Dev Operator' };

interface Template {
  agent: string;
  state: RunState;
  summary: string | null;
  by: { id: string; name: string } | { name: string; kind: TriggerKind };
  sources?: RunSource[];
  cost: number;
}

const PR = (n: number): RunSource => ({
  kind: 'pull_request',
  url: `https://github.com/example-org/geo-utils/pull/${n}`,
  label: `PR #${n}`,
  provider: 'github',
});
const SLACK: RunSource = {
  kind: 'trigger',
  url: 'https://example.slack.com/archives/C1',
  label: '#platform-eng',
  provider: 'slack',
};
const ALERT: RunSource = {
  kind: 'trigger',
  url: 'https://alerts.example.com/1',
  label: 'Alert',
  provider: 'alertmanager',
};

const ANA = { id: 'usr_ana', name: 'Ana Ruiz' };
const SAM = { id: 'usr_sam', name: 'Sam Lee' };

const TEMPLATES: Template[] = [
  {
    agent: 'at_coder',
    state: 'idle',
    summary: 'Implementing KOR-318: paginate the audit log endpoint.',
    by: ME,
    cost: 1.84,
  },
  {
    agent: 'at_security_reviewer',
    state: 'idle',
    summary: 'Working out why the nightly export job skips Sundays.',
    by: ME,
    cost: 0.31,
  },
  {
    agent: 'at_coder',
    state: 'budget_exhausted',
    summary: 'Migrating the rate limiter to the shared Redis cluster.',
    by: ME,
    cost: 5,
  },
  {
    agent: 'at_code_reviewer',
    state: 'active',
    summary: 'Reviewing pull request 412, which moves session storage to Redis.',
    by: { name: 'pr-review', kind: 'webhook' },
    sources: [PR(412)],
    cost: 0.42,
  },
  {
    agent: 'at_triage_bot',
    state: 'active',
    summary: 'Answering a question in #platform-eng about the retry budget for webhooks.',
    by: { name: 'platform-eng', kind: 'webhook' },
    sources: [SLACK],
    cost: 0.12,
  },
  {
    agent: 'at_incident_scribe',
    state: 'idle',
    summary: 'Writing up the Tuesday queue backlog incident.',
    by: ANA,
    cost: 0.27,
  },
  {
    agent: 'at_security_reviewer',
    state: 'active',
    summary: 'Drafting the 0.12 upgrade notes from the merged pull requests.',
    by: ME,
    cost: 0.66,
  },
  {
    agent: 'at_incident_scribe',
    state: 'active',
    summary: 'Summarising the p95 latency alert on the inference gateway.',
    by: { name: 'on-alert', kind: 'event' },
    sources: [ALERT],
    cost: 0.09,
  },
  {
    agent: 'at_release_notes',
    state: 'starting',
    summary: null,
    by: { name: 'nightly-notes', kind: 'scheduled' },
    cost: 0,
  },
  {
    agent: 'at_code_reviewer',
    state: 'completed',
    summary: 'Reviewed pull request 409 and left 6 inline comments.',
    by: { name: 'pr-review', kind: 'webhook' },
    sources: [PR(409)],
    cost: 0.31,
  },
  {
    agent: 'at_coder',
    state: 'failed',
    summary: 'Scanned the dependency bump in geo-utils; the sandbox ran out of disk.',
    by: { name: 'dependabot', kind: 'webhook' },
    sources: [PR(407)],
    cost: 0.18,
  },
  {
    agent: 'at_triage_bot',
    state: 'completed',
    summary: 'Answered why staging deploys queue behind production.',
    by: { name: 'platform-eng', kind: 'webhook' },
    sources: [SLACK],
    cost: 0.08,
  },
  {
    agent: 'at_release_notes',
    state: 'completed',
    summary: 'Wrote the weekly changelog for 14 merged pull requests.',
    by: { name: 'weekly-changelog', kind: 'scheduled' },
    cost: 0.21,
  },
  {
    agent: 'at_coder',
    state: 'completed',
    summary: 'Refactored the trigger dispatcher into smaller files.',
    by: SAM,
    cost: 0.94,
  },
];

const MINUTE = 60_000;
const ENDED: RunState[] = ['completed', 'failed', 'terminated', 'budget_exhausted'];

function toRun(t: Template, i: number, now: number): RunWithAttribution {
  const agent = AGENT_TYPES.find((a) => a.id === t.agent)!;
  const started = now - (i * 7 + 2) * MINUTE * (i > 8 ? 6 : 1);
  const ended = ENDED.includes(t.state) ? started + (4 + i) * MINUTE : null;
  const at = (ms: number) => new Date(ms).toISOString();
  const trigger = 'kind' in t.by ? t.by : null;
  return {
    id: `run_${String(i + 1).padStart(4, '0')}a1b2c3`,
    tenant_id: 'ten_8f3a21',
    agent_type_id: agent.id,
    agent_name: agent.name,
    agent_slug: agent.slug,
    identity_mode: agent.identity_mode,
    state: t.state,
    pod_name: '',
    created_by: trigger ? 'system' : (t.by as { id: string }).id,
    created_by_name: trigger ? null : t.by.name,
    ...(trigger && {
      started_by: {
        trigger_id: `trg_${trigger.name}`,
        trigger_name: trigger.name,
        trigger_kind: trigger.kind,
      },
    }),
    sources: t.sources ?? [],
    started_at: t.state === 'starting' ? null : at(started),
    completed_at: ended === null ? null : at(ended),
    created_at: at(started),
    updated_at: at(ended ?? started),
    usage: {
      cost_usd: t.cost,
      input_tokens: 0,
      output_tokens: 0,
      request_count: 0,
    },
    summary: t.summary,
  };
}

/** Runs as the list returns them, newest first; past the templates' count they repeat. */
export function buildRuns({
  runs = TEMPLATES.length,
  now = Date.now(),
} = {}): RunWithAttribution[] {
  return Array.from({ length: runs }, (_, i) => toRun(TEMPLATES[i % TEMPLATES.length]!, i, now));
}

/** Whether a run passes the list's filters, as kora-agents decides. */
function passes(r: RunWithAttribution, q: URLSearchParams): boolean {
  const states = q.get('state')?.split(',');
  const kind = q.get('trigger_kind');
  const text = q.get('q')?.toLowerCase();
  return (
    (!states || states.includes(r.state)) &&
    (q.get('created_by') !== 'me' || r.created_by === ME.id) &&
    (!q.get('agent_type_id') || r.agent_type_id === q.get('agent_type_id')) &&
    (!kind || (kind === 'none' ? !r.started_by : r.started_by?.trigger_kind === kind)) &&
    (!text ||
      [r.summary, r.agent_name, r.created_by_name, r.started_by?.trigger_name].some((s) =>
        s?.toLowerCase().includes(text),
      ))
  );
}

/** The run list route over `runs`, paged by an offset cursor and filtered like the server. */
export function runsRoutes(runs: RunWithAttribution[]): MockRoutes {
  return {
    'GET /api/agents/types': { data: AGENT_TYPES },
    'GET /api/agents/runs': ({ url }: MockRequest) => {
      const q = url.searchParams;
      const matching = runs.filter((r) => passes(r, q));
      const from = Number(q.get('cursor') ?? 0);
      const limit = Number(q.get('limit') ?? 50);
      const next = from + limit < matching.length ? String(from + limit) : null;
      return { data: matching.slice(from, from + limit), next_cursor: next };
    },
  };
}

/** A route that answers every run list request with `status`. */
export const failingRunsRoutes = (status: number): MockRoutes => ({
  'GET /api/agents/types': { data: AGENT_TYPES },
  'GET /api/agents/runs': () =>
    new Response(
      JSON.stringify({ code: 'upstream_error', message: 'Could not load runs. Try again.' }),
      {
        status,
        headers: { 'Content-Type': 'application/json' },
      },
    ),
});

/** A run list route that never answers, for the loading state. */
export const pendingRunsRoutes: MockRoutes = {
  'GET /api/agents/types': { data: AGENT_TYPES },
  'GET /api/agents/runs': () => new Promise(() => {}),
};

let seq = 48_000;

/** One `event: run` message, sent `delayMs` after the stream opens. */
export function runEvent(
  run: RunWithAttribution,
  type: string,
  delayMs: number,
  matches = true,
): MockStreamEvent {
  seq += 1;
  const data: RunStreamMessage = { type, matches, run };
  return { type: 'run', id: String(seq), data, delayMs };
}

/** A run a person has just started: no title yet, then its title, then its first idle turn. */
export function liveRunEvents(startAt: number): MockStreamEvent[] {
  const now = Date.now() + startAt;
  const created: RunWithAttribution = {
    ...toRun({ agent: 'at_coder', state: 'pending', summary: null, by: ME, cost: 0 }, 0, now),
    id: 'run_live0001',
    started_at: null,
  };
  const at = (ms: number) => new Date(now + ms).toISOString();
  return [
    runEvent(created, RUN_CREATED, startAt),
    runEvent(
      { ...created, state: 'active', started_at: at(1000), updated_at: at(1000) },
      'agents.run.started.v1',
      startAt + 1000,
    ),
    runEvent(
      {
        ...created,
        state: 'active',
        started_at: at(1000),
        updated_at: at(2000),
        summary: 'Adding retries to the webhook dispatcher.',
      },
      'agents.run.summarized.v1',
      startAt + 2000,
    ),
  ];
}
