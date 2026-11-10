import type { Decorator, Meta, StoryObj } from '@storybook/react';
import type { RunSource } from '@runkora/shared/api';
import { mockApi } from '@runkora/storybook/mock-api';
import { mockEventSource, type MockStreamEvent } from '@runkora/storybook/mock-event-source';
import RunLayout from '../layout';
import type { RunState } from '../_types';
import { RunShell } from './run-shell';

const RUN_ID = 'run_01JD8ZK4Q7M2V9X3C5RB6TNWEA';
const REPO = 'https://github.com/example-org/demo-repo';
const PR = `${REPO}/pull/42`;

/**
 * The run view at /agents/[slug]/runs/[runId]: top bar, chat and input. Run metadata comes
 * through the mock API and the transcript through a replayed event stream, as on a reload.
 */
const meta: Meta<typeof RunShell> = {
  title: 'Pages/Agents/Run',
  component: RunShell,
  parameters: {
    layout: 'fullscreen',
    nextjs: {
      appDirectory: true,
      navigation: {
        pathname: `/agents/code-reviewer/runs/${RUN_ID}`,
        segments: [
          ['slug', 'code-reviewer'],
          ['runId', RUN_ID],
        ],
      },
    },
  },
  args: { slug: 'code-reviewer', runId: RUN_ID },
  decorators: [
    (Story) => (
      <RunLayout>
        <Story />
      </RunLayout>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof RunShell>;

/** Wraps a payload in the stream's envelope, `seconds` after the run started. */
const at = (seconds: number, type: string, payload: unknown): MockStreamEvent => ({
  type,
  data: { at: new Date(Date.UTC(2026, 10, 10, 9, 14, seconds)).toISOString(), payload },
});

const PROMPT =
  'Review pull request #42 in example-org/demo-repo. Check the new run source links for unsafe URLs and missing tests.';

const CONVERSATION: MockStreamEvent[] = [
  at(0, 'run_state', { state: 'active' }),
  at(1, 'human_message', { content: PROMPT }),
  at(4, 'text_block', { content: 'I will read the diff first, then the component it touches.' }),
  at(5, 'tool_use', { id: 't1', name: 'Bash', input: { command: 'gh pr diff 42 --name-only' } }),
  at(7, 'tool_result', {
    tool_use_id: 't1',
    content: 'frontend/src/components/run-source-chips.tsx\nfrontend/src/shared/api.ts',
  }),
  at(8, 'tool_use', {
    id: 't2',
    name: 'Read',
    input: { file_path: 'frontend/src/components/run-source-chips.tsx' },
  }),
  at(9, 'tool_result', {
    tool_use_id: 't2',
    content:
      'export function RunSourceChips({ sources }) {\n  const safe = (sources ?? []).filter((s) => isSafeUrl(s.url));\n  ...',
  }),
  at(16, 'message_complete', {
    text: 'The change looks safe to merge.\n\n- `isSafeUrl` drops anything that is not http or https, so a `javascript:` URL never renders.\n- Each link opens in a new tab with `rel="noopener noreferrer"`.\n\nOne gap: there is no test for a source with an unparseable URL. I left a review comment suggesting one.',
  }),
  at(17, 'run_state', { state: 'idle' }),
];

/** A later delivery routed to the same run, and the agent's answer to it. */
const JOINED: MockStreamEvent[] = [
  at(40, 'run_state', { state: 'active' }),
  at(41, 'trigger_message', {
    content: 'New commits were pushed to feat/run-sources-follow-up in example-org/demo-repo.',
    trigger_name: 'pr-review',
  }),
  at(48, 'message_complete', {
    text: 'The follow-up branch adds the missing URL test. Nothing else to flag.',
  }),
  at(49, 'run_state', { state: 'idle' }),
];

interface RunOptions {
  sources?: RunSource[];
  state?: RunState;
  startedBy?: string;
  events?: MockStreamEvent[];
}

/** The routes and stream the run view reads, built from the parts the top bar shows. */
function runPage({
  sources = [],
  state = 'idle',
  startedBy = 'Ada Lovelace',
  events = CONVERSATION,
}: RunOptions): Decorator[] {
  return [
    mockApi({
      'GET /api/agents/runs/:id': {
        can_steer: true,
        trace_url: null,
        data: {
          state,
          summary: 'Review PR #42: run source links',
          created_by: 'usr_01JD8Z',
          created_by_name: startedBy,
          sources,
        },
        agent_type: {
          name: 'Code Reviewer',
          model_id: 'claude-sonnet-4-5',
          identity_mode: 'autonomous',
          budget: { per_run_limit_usd: 5 },
        },
      },
      'GET /api/agents/runs/:id/usage': { spend_usd: 0.42, total_tokens: 48_210 },
      'POST /api/agents/runs/:id/messages': {},
      'POST /api/agents/runs/:id/interrupt': {},
    }),
    mockEventSource(events),
  ];
}

const link = (
  kind: RunSource['kind'],
  url: string,
  label: string,
  provider: RunSource['provider'] = 'github',
): RunSource => ({ kind, url, label, ...(provider ? { provider } : {}) });

const branch = (ref: string, repo = REPO) => link('branch', `${repo}/tree/${ref}`, `branch ${ref}`);

const PR_SOURCES: RunSource[] = [
  link('trigger', PR, 'PR #42'),
  link('repository', REPO, 'example-org/demo-repo'),
  branch('feat/run-sources-check'),
];

/** A run a person started: no source chips. */
export const NoSources: Story = { decorators: runPage({}) };

/** A run a pull request event started: the PR chip, with the repository and branch behind "+2". */
export const GitHubPullRequest: Story = {
  decorators: runPage({ sources: PR_SOURCES, startedBy: 'pr-review' }),
};

/** A later delivery joined the run and added its branch: the PR chip and "+3". */
export const JoinedDeliveries: Story = {
  decorators: runPage({
    sources: [...PR_SOURCES, branch('feat/run-sources-follow-up')],
    startedBy: 'pr-review',
    events: [...CONVERSATION, ...JOINED],
  }),
};

/** A run a Slack message started. */
export const SlackThread: Story = {
  decorators: runPage({
    sources: [
      link(
        'trigger',
        'https://app.slack.com/client/T0EXAMPLE/C0EXAMPLE/thread/C0EXAMPLE-1731230040.000100',
        'Slack thread',
        'slack',
      ),
    ],
    startedBy: 'slack-review',
  }),
};

const LONG_REPO = 'https://github.com/example-org/a-very-long-repository-name-for-layout-checks';

/** A long repository name and a long branch. */
export const LongLabels: Story = {
  decorators: runPage({
    sources: [
      link('trigger', `${LONG_REPO}/pull/42`, 'PR #42'),
      link('repository', LONG_REPO, 'example-org/a-very-long-repository-name-for-layout-checks'),
      branch('feature/JIRA-12345-make-the-run-view-top-bar-handle-long-branch-names', LONG_REPO),
    ],
    startedBy: 'pr-review',
  }),
};

/** Eight links from two deliveries: the first delivery's chip, the rest behind "+7". */
export const ManySources: Story = {
  decorators: runPage({
    sources: [
      ...PR_SOURCES,
      link('pull_request', `${REPO}/pull/43`, 'PR #43'),
      branch('feat/run-sources-follow-up'),
      branch('fix/run-sources-urls'),
      link('repository', 'https://github.com/example-org/docs', 'example-org/docs'),
      link('trigger', 'https://alerts.example.com/incidents/1234', 'Alert', 'alertmanager'),
    ],
    startedBy: 'pr-review',
  }),
};
