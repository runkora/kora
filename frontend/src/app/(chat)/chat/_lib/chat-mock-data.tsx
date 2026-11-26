/**
 * Mock data for the chat page's stories, served the way the BFF serves it: the agents the viewer
 * can see, the viewer's recent runs, and each run with its recorded events. Names, repositories and
 * times are invented.
 */
import type { Decorator } from '@storybook/react';
import type { AgentType, RunSource, RunWithAttribution } from '@runkora/shared/api';
import { PermissionsProvider } from '@runkora/app/(dashboard)/_components/permissions-provider';
import { mockApi, type MockRequest } from '@runkora/storybook/mock-api';
import { mockEventSource, type MockStreamEvent } from '@runkora/storybook/mock-event-source';
import type { MockRun } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-page-mock-data';

/** The viewer, who sends the first message of each run they start here. */
const VIEWER = { id: 'user-sam', type: 'USER', name: 'Sam Lee' };

type Spec = [name: string, description: string, mode: 'delegated' | 'autonomous'];

/** The agents a small tenant has. */
const CORE: Spec[] = [
  ['Coder', 'Makes a change in a repository and opens a pull request.', 'delegated'],
  ['Code Reviewer', 'Reviews a pull request and comments on what the change breaks.', 'autonomous'],
  [
    'Platform Help',
    'Answers questions about agents, models and access, and makes changes through the API.',
    'delegated',
  ],
  ['Incident Scribe', 'Keeps the incident timeline and drafts the write-up.', 'autonomous'],
  [
    'Release Notes',
    'Drafts release notes from the pull requests merged since the last tag.',
    'autonomous',
  ],
  [
    'Triage Bot',
    'Labels new issues, finds duplicates and asks reporters for missing detail.',
    'autonomous',
  ],
];

/** More agents, for a tenant that has many. */
const MORE: Spec[] = [
  ['Docs Writer', 'Updates the docs a merged change made stale.', 'delegated'],
  ['Dependency Updater', 'Bumps dependencies and fixes what the bump breaks.', 'autonomous'],
  ['Flaky Test Hunter', 'Finds tests that fail at random and proposes a fix.', 'autonomous'],
  ['Cost Sweeper', 'Finds idle cloud resources and reports what they cost.', 'autonomous'],
  [
    'Security Scanner',
    'Checks a repository for leaked secrets and known vulnerable packages.',
    'autonomous',
  ],
  ['Migration Helper', 'Writes and checks a database migration for a schema change.', 'delegated'],
  ['On-call Assistant', 'Reads alerts and dashboards and suggests the next step.', 'delegated'],
  ['Changelog Keeper', 'Keeps the changelog in step with merged pull requests.', 'autonomous'],
  ['API Linter', 'Checks an API change against the style guide.', 'autonomous'],
  ['Schema Reviewer', 'Reviews a schema change for locking and backfill risk.', 'autonomous'],
  ['Onboarding Guide', 'Answers a new starter’s questions about the codebase.', 'delegated'],
  ['Runbook Runner', 'Follows a runbook step by step and records what it did.', 'delegated'],
  ['Test Writer', 'Adds tests for code that has none.', 'delegated'],
  ['Accessibility Checker', 'Checks a page change for contrast, focus and labels.', 'autonomous'],
  ['Support Answerer', 'Drafts answers to support tickets from the docs.', 'autonomous'],
  ['Policy Auditor', 'Lists grants that give more access than their agents use.', 'autonomous'],
  ['Data Quality Monitor', 'Checks yesterday’s loads for gaps and duplicates.', 'autonomous'],
  ['Translation Checker', 'Finds strings missing from a locale.', 'autonomous'],
];

const slugOf = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, '-');

/** One agent as GET /api/agents/types returns it. */
function agentType([name, description, mode]: Spec, canRun: boolean): AgentType {
  const slug = slugOf(name);
  return {
    id: `at-${slug}`,
    tenant_id: 'example',
    name,
    slug,
    description,
    system_prompt: '',
    framework: 'claude-code',
    identity_mode: mode,
    grant_scopes: [],
    connectivity: { mode: 'none' },
    connectors: [],
    max_concurrent_runs: 4,
    state: 'active',
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
    can_run: canRun,
  };
}

/** The tenant's agents: the usual six, or 24 with `many`. `canRun` false for a viewer who may start none. */
export const agentsOf = ({ many = false, canRun = true } = {}): AgentType[] =>
  [...CORE, ...(many ? MORE : [])].map((s) => agentType(s, canRun));

const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();

/** A run of `agent`, started `minutes` ago, as the BFF returns it. */
function runOf(
  agent: AgentType,
  id: string,
  minutes: number,
  over: Partial<RunWithAttribution> = {},
): RunWithAttribution {
  return {
    id,
    tenant_id: 'example',
    agent_type_id: agent.id,
    agent_slug: agent.slug,
    agent_name: agent.name,
    state: 'completed',
    pod_name: 'session-manager-pool-0',
    created_by: VIEWER.id,
    created_by_name: VIEWER.name,
    started_at: ago(minutes),
    completed_at: null,
    created_at: ago(minutes),
    updated_at: ago(minutes),
    summary: null,
    sources: [],
    usage: { cost_usd: 0, input_tokens: 0, output_tokens: 0, request_count: 0 },
    ...over,
  };
}

const bySlug = (slug: string) => agentsOf({ many: true }).find((a) => a.slug === slug)!;

/** The viewer's recent runs, newest first, as GET /api/agents/runs?created_by=me returns them. */
export const HISTORY: RunWithAttribution[] = (
  [
    ['coder', 'Move the billing export to the new storage bucket', 95, 'active'],
    ['platform-help', 'Why the evaluation agents cannot use the new model in staging', 180],
    ['code-reviewer', 'Review pull request #12, which makes Distance return metres', 60 * 22],
    ['release-notes', 'Draft release notes for 0.12', 60 * 50],
    ['coder', 'Fix the flaky retention sweep test', 60 * 74],
    ['triage-bot', 'Triage the issues opened over the weekend', 60 * 98],
    ['coder', 'Add a cost column to the runs export', 60 * 140],
    ['incident-scribe', 'Write up the webhook delivery outage', 60 * 170],
  ] as const
).map(([slug, summary, minutes, state], i) =>
  runOf(bySlug(slug), `history-${i}`, minutes, { summary, state: state ?? 'completed' }),
);

/** What the viewer asks Coder for in the hand-off stories. */
export const TASK = `Add retries to the webhook relay's outbound calls: three attempts with backoff, and log each failure with the delivery id.`;

const NEW_RUN_ID = '8c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f';
const REPO_SOURCE: RunSource = {
  kind: 'repository',
  url: 'https://github.com/example-org/webhook-relay',
  label: 'example-org/webhook-relay',
  provider: 'github',
};

/** Wraps a payload in the stream's envelope, `seconds` after the run started. */
const at =
  (start: string) =>
  (seconds: number, type: string, payload: unknown): MockStreamEvent => ({
    type,
    data: { at: new Date(new Date(start).getTime() + seconds * 1000).toISOString(), payload },
  });

/**
 * The run the viewer just started from the chat page. `pending`: placed but not yet working, with
 * only the viewer's message. `active`: the first turn, still streaming.
 */
export function startedRun(state: 'pending' | 'active'): MockRun {
  const coder = bySlug('coder');
  const start = ago(state === 'pending' ? 0.05 : 0.8);
  const t = at(start);
  const first: MockStreamEvent[] = [
    t(0, 'run_state', { state }),
    t(0, 'human_message', { content: TASK, sender: VIEWER }),
  ];
  const working: MockStreamEvent[] = [
    t(2, 'tool_use', { id: 'sbx', name: 'mcp__runtime__provision_sandbox', input: {} }),
    t(20, 'sandbox_environment', {
      action: 'create',
      environment: 'default',
      image: 'ghcr.io/runkora/kora-sandbox-base@sha256:0a1b2c3d4e5f',
    }),
    t(22, 'tool_result', { tool_use_id: 'sbx', content: 'Sandbox provisioned.' }),
    t(24, 'tool_use', {
      id: 'b1',
      name: 'Bash',
      input: {
        command:
          'gh repo clone example-org/webhook-relay && cd webhook-relay && grep -rn "client.Do(" relay/',
      },
    }),
    t(27, 'tool_result', {
      tool_use_id: 'b1',
      content: 'relay/deliver.go:48:\tresp, err := r.client.Do(req)',
    }),
    t(29, 'text_block', {
      content:
        'The relay sends each delivery once and returns the first error. I will wrap the call in a retry with exponential backoff and keep the delivery id on every log line.',
    }),
    t(40, 'text_delta', {
      delta:
        '\n\nI am adding `withRetry` to `relay/retry.go`. It makes three attempts, waits 200ms before the second and 800ms before the third, and stops early on a 4xx answer, since',
    }),
  ];
  const active = state === 'active';
  return {
    body: {
      data: runOf(coder, NEW_RUN_ID, active ? 0.8 : 0.05, {
        state,
        // The summary arrives once the run has started; until then the page has the message.
        summary: active ? 'Add retries to the webhook relay’s outbound calls' : null,
        sources: [REPO_SOURCE],
        usage: active
          ? { cost_usd: 0.012, input_tokens: 180, output_tokens: 220, request_count: 3 }
          : { cost_usd: 0, input_tokens: 0, output_tokens: 0, request_count: 0 },
      }),
      agent_type: coder,
      can_steer: true,
      trace_url: null,
    },
    usage: { spend_usd: active ? 0.012 : 0 },
    events: active ? [...first, ...working] : first,
  };
}

/**
 * The decorators a chat story needs: the agents, the viewer's history, and the BFF and event stream
 * answering for `run`, which a send also starts. `viewerName` is the session's display name, which
 * is empty when the sign-in token carries none.
 */
export function chatStory({
  agents = agentsOf(),
  history = HISTORY,
  run = startedRun('pending'),
  viewerName = VIEWER.name,
}: {
  agents?: AgentType[];
  history?: RunWithAttribution[];
  run?: MockRun;
  viewerName?: string;
} = {}): Decorator[] {
  const all = history.filter((h) => h.id !== run.body.data.id);
  return [
    (Story) => (
      <PermissionsProvider>
        <Story />
      </PermissionsProvider>
    ),
    mockApi({
      'GET /api/auth/session': {
        user: { sub: VIEWER.id, email: 'sam@example.com', name: viewerName },
        permissions: { scopes: [] },
        expiresAt: Math.floor(Date.now() / 1000) + 3600,
      },
      'GET /api/agents/types': { data: agents },
      'GET /api/agents/runs': { data: all, next_cursor: null },
      'POST /api/agents/types/:id/runs': { data: run.body.data },
      'GET /api/agents/runs/:id': () => run.body,
      'GET /api/agents/runs/:id/usage': () => run.usage,
      'GET /api/agents/runs/:id/grades': { grades: [] },
      'GET /api/agents/types/:id/graders': { graders: [] },
      'GET /api/agents/types/:slug/runs': ({ url }: MockRequest) => ({
        data: all.filter((h) => url.pathname.includes(h.agent_type_id.replace('at-', ''))),
        next_cursor: null,
      }),
      'POST /api/agents/runs/:id/messages': {},
      'POST /api/agents/runs/:id/interrupt': {},
    }),
    mockEventSource(() => run.events),
  ];
}
