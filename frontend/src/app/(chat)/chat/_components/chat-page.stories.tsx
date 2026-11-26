import type { Meta, StoryObj } from '@storybook/react';
import { userEvent, within } from 'storybook/test';
import { Stack } from '@runkora/hades';
import { RunShell } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_components/run-shell';
import { RunProvider } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_context/run-context';
import { agentsOf, chatStory, startedRun } from '../_lib/chat-mock-data';
import { useOwnRuns } from '../_hooks/use-own-runs';
import { ChatPage } from './chat-page';
import { ChatRun } from './chat-run';

/**
 * The chat page starts a run of any agent. Empty, it is the run page's composer with an agent in
 * its footer. Once sent, it is the run page, with the viewer's runs in the left pane.
 */
const meta: Meta<typeof ChatPage> = {
  title: 'Pages/Chat',
  component: ChatPage,
  parameters: {
    layout: 'fullscreen',
    nextjs: { appDirectory: true, navigation: { pathname: '/chat' } },
  },
};
export default meta;
type Story = StoryObj<typeof ChatPage>;

const openPicker: Story['play'] = async ({ canvasElement }) => {
  const button = await within(canvasElement).findByRole('button', { name: /^Agent:/ });
  await userEvent.click(button);
};

/** A run started from the chat page, at its own address as a reload opens it. */
const started = (run: ReturnType<typeof startedRun>) => (
  <RunShell slug={run.body.agent_type.slug ?? 'coder'} runId={run.body.data.id} fromChat />
);

/** The same run in a frame that sets its own height. */
function FramedRun({ run, height }: { run: ReturnType<typeof startedRun>; height: string }) {
  const own = useOwnRuns();
  return (
    <RunProvider slug={run.body.agent_type.slug ?? 'coder'} runId={run.body.data.id}>
      <ChatRun own={own} height={height} />
    </RunProvider>
  );
}

/** The heading and the composer, with the agent of the viewer's newest run. */
export const Empty: Story = { decorators: chatStory() };

/** The agent list open: what each agent does and who its runs act as. */
export const AgentPickerOpen: Story = { decorators: chatStory(), play: openPicker };

/** A tenant with 24 agents: the list scrolls and takes a search. */
export const ManyAgents: Story = {
  decorators: chatStory({ agents: agentsOf({ many: true }) }),
  play: openPicker,
};

/** Opened from an agent's page: that agent is selected. */
export const AgentFromAddress: Story = {
  args: { agentSlug: 'triage-bot' },
  decorators: chatStory(),
};

/** Sent: the run is placed and titled by the first line of the message until its summary arrives. */
export const Starting: Story = {
  render: () => started(startedRun('pending')),
  decorators: chatStory({ run: startedRun('pending') }),
};

/** The first turn, still streaming, with the composer at the foot of the thread. */
export const Running: Story = {
  render: () => started(startedRun('active')),
  decorators: chatStory({ run: startedRun('active') }),
};

/** No agent is set up for the viewer. */
export const NoAgents: Story = { decorators: chatStory({ agents: [], history: [] }) };

/** The viewer may see the agents but start none of them. */
export const CannotStart: Story = {
  decorators: chatStory({ agents: agentsOf({ canRun: false }) }),
};

/** A session with no display name: the heading ends at "next?". */
export const NoName: Story = { decorators: chatStory({ viewerName: '' }) };

/** A laptop frame: the history folds into a drawer, as the run page's sessions do. */
const laptop = (children: React.ReactNode) => (
  <Stack style={{ width: 1024, height: 768, overflow: 'hidden' }}>{children}</Stack>
);

export const EmptyLaptop: Story = {
  name: 'Empty · 1024',
  render: (args) => laptop(<ChatPage height="768px" {...args} />),
  decorators: chatStory(),
};

export const RunningLaptop: Story = {
  name: 'Running · 1024',
  render: () => laptop(<FramedRun run={startedRun('active')} height="768px" />),
  decorators: chatStory({ run: startedRun('active') }),
};
