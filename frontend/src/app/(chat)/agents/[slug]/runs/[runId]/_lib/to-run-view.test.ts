import { describe, expect, it } from 'vitest';
import type { RunEventSummary, RunWithAttribution } from '@runkora/shared/api';
import type { UseRunReturn } from '../_hooks/use-run';
import type { HumanTurn, ToolCall, Turn } from '../_types';
import { triggerEventOf } from '../_components/trigger-event';
import { elapsedMs, participantsOf, stepOf, type Entry } from './run-view';
import { stateLabel } from '../_components/run-header';
import { acpCallOf, entriesOf, toRunView } from './to-run-view';

const RUN = {
  id: 'run-1',
  state: 'active',
  created_by: 'usr-1',
  created_by_name: 'Ada Lovelace',
  started_at: '2026-11-11T01:00:00Z',
  created_at: '2026-11-11T01:00:00Z',
  completed_at: null,
  sources: [],
} as unknown as RunWithAttribution;

const TRIGGERED = {
  ...RUN,
  started_by: { trigger_id: 't1', trigger_name: 'review-pr', trigger_kind: 'webhook' },
  sources: [
    { kind: 'trigger', url: 'https://github.com/o/r/pull/12', label: 'PR #12', provider: 'github' },
  ],
} as RunWithAttribution;

const human = (id: string, content: string, over: Partial<HumanTurn> = {}): HumanTurn => ({
  type: 'human',
  id,
  content,
  timestamp: '09:14:05',
  ...over,
});

const messages = (turns: Turn[], run: RunWithAttribution) =>
  entriesOf(turns, run, false).entries.filter(
    (e): e is Extract<Entry, { type: 'message' }> => e.type === 'message',
  );

const EVENT: RunEventSummary = {
  actor: { name: 'dev-reviewer', kind: 'person' },
  verb: 'commented on',
  object: { label: 'PR #12', url: 'https://github.com/o/r/pull/12' },
  quote: 'Can you check the empty slice?',
  preview: {
    title: 'Distance: return metres',
    facts: ['o/r #6', 'fix → main'],
    added: 17,
    removed: 3,
    status: { label: 'Open', tone: 'ok' },
  },
};

describe('messages', () => {
  it('names a person by the recorded sender', () => {
    const [m] = messages(
      [human('h1', 'hi', { sender: { id: 'u2', type: 'USER', name: 'Grace Hopper' } })],
      RUN,
    );
    expect(m.author).toEqual({ kind: 'person', name: 'Grace Hopper' });
  });

  it('names the first message without a sender by the run starter, and later ones as unknown', () => {
    const [first, later] = messages([human('h1', 'start'), human('h2', 'more')], RUN);
    expect(first.author.name).toBe('Ada Lovelace');
    expect(later.author).toEqual({ kind: 'person', name: '', unrecorded: true });
  });

  it('reads the first message of a trigger-started run as the trigger, with its event', () => {
    const [m] = messages([human('h1', 'Review this.', { event: EVENT })], TRIGGERED);
    expect(m.author).toMatchObject({ kind: 'trigger', name: 'review-pr', via: 'webhook' });
    expect(m.event).toEqual({
      actor: EVENT.actor,
      verb: 'commented on',
      object: EVENT.object,
      quote: 'Can you check the empty slice?',
      unfurl: {
        icon: 'github',
        title: 'Distance: return metres',
        meta: ['o/r #6', 'fix → main'],
        diff: { added: 17, removed: 3 },
        status: { label: 'Open', tone: 'ok' },
      },
    });
  });

  it('marks a trigger message as joined and keeps its event', () => {
    const [, m] = messages(
      [human('h1', 'start'), human('h2', 'comment', { fromTrigger: 'mention', event: EVENT })],
      RUN,
    );
    expect(m).toMatchObject({ joined: true, author: { kind: 'trigger', name: 'mention' } });
    expect(triggerEventOf(m)).toMatchObject({ actor: { name: 'dev-reviewer' }, joined: true });
  });

  it('falls back to the prompt for a trigger message with no event', () => {
    const [m] = messages(
      [human('h1', 'A deploy finished. Check it.\n\nService: billing\nVersion: 2.14.0')],
      TRIGGERED,
    );
    expect(m.event).toBeUndefined();
    expect(triggerEventOf(m)).toMatchObject({
      actor: { name: 'review-pr', kind: 'system' },
      verb: 'received a webhook',
      unfurl: { title: 'A deploy finished.', meta: ['Service billing', 'Version 2.14.0'] },
    });
  });

  it('holds a message queued for the next step out of the thread', () => {
    const { entries, queued } = entriesOf(
      [human('h1', 'later', { delivery: 'queued' })],
      RUN,
      false,
    );
    expect(entries).toEqual([]);
    expect(queued).toEqual([{ id: 'h1', text: 'later' }]);
  });
});

const tool = (
  name: string,
  input: Record<string, unknown>,
  over: Partial<ToolCall> = {},
): ToolCall => ({
  id: name,
  name,
  input,
  status: 'done',
  result: '',
  ...over,
});

describe('tool calls', () => {
  it('reads a failed command with its exit code', () => {
    const step = stepOf(
      acpCallOf(
        tool('Bash', { command: 'make' }, { status: 'error', result: 'Exit code 2\nboom' }),
      ),
    );
    expect(step).toMatchObject({
      kind: 'execute',
      verb: 'Ran',
      target: 'make',
      status: 'failed',
      exitCode: 2,
      errorLine: 'boom',
    });
  });

  it('reads an edit as a diff of the file', () => {
    const step = stepOf(
      acpCallOf(
        tool('Edit', {
          file_path: '/workspaces/r1/a.go',
          old_string: 'x := 1',
          new_string: 'x := 2',
        }),
      ),
    );
    expect(step).toMatchObject({ kind: 'edit', target: 'a.go' });
    expect(step.diffs?.[0]).toMatchObject({ path: 'a.go', added: 1, removed: 1 });
  });

  it('marks a call policy refused as denied', () => {
    const step = stepOf(
      acpCallOf(tool('Bash', { command: 'curl https://example.com' }, { result: 'egress denied' })),
    );
    expect(step.status).toBe('denied');
  });

  it('reads the sandbox provision and an MCP call by their tool names', () => {
    expect(stepOf(acpCallOf(tool('mcp__runtime__provision_sandbox', {}))).kind).toBe('sandbox');
    expect(stepOf(acpCallOf(tool('runtime_provision_sandbox', {}))).kind).toBe('sandbox');
    expect(stepOf(acpCallOf(tool('mcp__github__get_pull_request', {})))).toMatchObject({
      kind: 'mcp',
      target: 'github · get_pull_request',
    });
  });

  it('reads TodoWrite as the plan', () => {
    const { entries } = entriesOf(
      [
        {
          type: 'agent',
          id: 'a1',
          timestamp: '',
          elements: [
            {
              kind: 'tool',
              tool: tool('TodoWrite', {
                todos: [
                  { content: 'Read the diff', status: 'completed' },
                  { content: 'Post a review', status: 'in_progress' },
                ],
              }),
            },
          ],
        },
      ],
      RUN,
      false,
    );
    expect(entries).toEqual([
      {
        type: 'plan',
        id: 'a1-0',
        plan: {
          items: [
            { text: 'Read the diff', status: 'done' },
            { text: 'Post a review', status: 'active' },
          ],
        },
      },
    ]);
  });
});

function hookState(over: Partial<UseRunReturn>): UseRunReturn {
  return {
    runId: 'run-1',
    slug: 'code-reviewer',
    runState: 'budget_exhausted',
    turns: [],
    spentUsd: 5,
    canSteer: true,
    loadError: null,
    stopError: null,
    traceUrl: null,
    run: { ...RUN, state: 'budget_exhausted', completed_at: '2026-11-11T01:04:05Z' },
    agentType: null,
    budgetStop: null,
    sandbox: null,
    earlierRuns: [],
    earlierRunsError: null,
    isStreaming: false,
    isPending: false,
    isBusy: false,
    isStarting: false,
    connectionStatus: 'open',
    liveActivity: null,
    addHumanMessage: () => {},
    interruptRun: () => {},
    reconnect: () => {},
    ...over,
  };
}

describe('budget stop', () => {
  it('names whose budget stopped the run, never its id', () => {
    const view = toRunView(
      hookState({
        budgetStop: {
          scope: 'principal',
          owner: 'spiffe://kora.example/ns/example/sa/code-reviewer',
          owner_name: 'Code Reviewer',
          window: 'monthly',
          limit_usd: 5,
          resets_at: '2026-12-01T00:00:00Z',
        },
      }),
    );
    expect(view.budgetStop).toMatchObject({ whose: "Code Reviewer's monthly budget", limitUsd: 5 });
    expect(view.budgetStop?.resets).not.toBe('');
  });

  it("names a per-run ceiling as this run's budget, with no reset", () => {
    const view = toRunView(
      hookState({
        budgetStop: {
          scope: 'run',
          owner: 'run-1',
          owner_name: '',
          window: 'run',
          limit_usd: 0.25,
        },
      }),
    );
    expect(view.budgetStop).toEqual({ whose: "this run's budget", limitUsd: 0.25, resets: '' });
  });

  it.each([
    ['delegator_run', "this run's budget"],
    ['tenant', "the tenant's budget"],
    ['role_pool', "the agent's budget"],
  ])('names a %s ceiling with no owner name by its level', (scope, whose) => {
    const view = toRunView(
      hookState({
        budgetStop: { scope, owner: 'id-1', owner_name: '', window: 'monthly', limit_usd: 5 },
      }),
    );
    expect(view.budgetStop?.whose).toBe(whose);
  });

  it('has no budget detail when the run recorded none', () => {
    const view = toRunView(hookState({}));
    expect(view.state).toBe('budget_exhausted');
    expect(view.budgetStop).toBeUndefined();
    expect(elapsedMs(view, 0)).toBe(245_000);
  });
});

describe('agents and previews', () => {
  it('reads an agent sender as an agent', () => {
    const [, m] = messages(
      [
        human('h1', 'start'),
        human('h2', 'hi', { sender: { id: 'a1', type: 'AGENT', name: 'Planner' } }),
      ],
      RUN,
    );
    expect(m.author).toEqual({ kind: 'agent', name: 'Planner' });
  });

  it('draws no preview that only repeats the object', () => {
    const fired: RunEventSummary = {
      actor: { name: 'Sam', kind: 'person' },
      verb: 'fired',
      object: { label: 'refresh-index' },
      preview: { title: 'refresh-index' },
    };
    const [m] = messages([human('h1', 'go', { event: fired })], TRIGGERED);
    expect(m.event?.unfurl).toBeUndefined();
  });

  it('carries a cut result through to the step', () => {
    const step = stepOf(
      acpCallOf(
        tool('Read', { file_path: 'a' }, { result: 'x', truncated: true, originalBytes: 200_000 }),
      ),
    );
    expect(step.truncated).toEqual({ originalBytes: 200_000 });
  });

  it('runs an agent with no framework recorded on Claude Code', () => {
    const view = toRunView(
      hookState({ agentType: { framework: '' } as unknown as UseRunReturn['agentType'] }),
    );
    expect(view.harness).toBe('claude-code');
  });
});

describe('trigger fallback', () => {
  it('claims no kind for a trigger message whose kind is unknown', () => {
    const [, m] = messages(
      [human('h1', 'start'), human('h2', 'Look at this.', { fromTrigger: 'route' })],
      RUN,
    );
    expect(triggerEventOf(m)).toMatchObject({ verb: 'sent a message', actor: { icon: 'zap' } });
  });
});

describe('activity', () => {
  it('says why the model call is being retried', () => {
    const view = toRunView(
      hookState({
        runState: 'active',
        liveActivity: {
          kind: 'retry',
          attempt: 2,
          maxRetries: 5,
          error: 'authentication_failed\nmore',
        },
      }),
    );
    expect(view.activity).toBe('Retrying the model call (2 of 5): authentication_failed');
  });
});

describe('people', () => {
  it('lists the person behind a trigger, senders, and triggers with no person', () => {
    const turns: Turn[] = [
      human('h1', 'start', { event: EVENT }),
      human('h2', 'steer', { sender: { id: 'u2', type: 'USER', name: 'Grace Hopper' } }),
      human('h3', 'again', { sender: { id: 'u2', type: 'USER', name: 'Grace Hopper' } }),
      human('h4', 'ci failed', { fromTrigger: 'ci-failed' }),
      human('h5', 'from before senders were recorded'),
      human('h6', 'checked', { sender: { id: 'a1', type: 'AGENT', name: 'Planner' } }),
    ];
    const view = toRunView(hookState({ runState: 'active', turns, run: TRIGGERED }));
    expect(participantsOf(view)).toEqual([
      { name: 'dev-reviewer', kind: 'person', detail: 'Started it via GitHub' },
      { name: 'Grace Hopper', kind: 'person', detail: '2 messages' },
      { name: 'ci-failed', kind: 'trigger', detail: '1 message · via GitHub' },
      { name: 'Planner', kind: 'agent', detail: '1 message' },
    ]);
  });
});

describe('run state', () => {
  it('is unknown, with no label, until the stream or the run reports one', () => {
    const view = toRunView(hookState({ runState: null, run: null }));
    expect(view.state).toBeUndefined();
    expect(stateLabel(view)).toBeNull();
  });

  it.each(['pending', 'starting'] as const)('reads "Starting" for a %s run', (state) => {
    const view = toRunView(hookState({ runState: state, run: null }));
    expect(stateLabel(view)?.label).toBe('Starting');
  });

  it('takes the fetched run state when the stream has none', () => {
    const view = toRunView(
      hookState({ runState: null, run: { ...RUN, state: 'completed' } as RunWithAttribution }),
    );
    expect(stateLabel(view)?.label).toBe('Completed');
  });
});
