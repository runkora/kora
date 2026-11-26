import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { RunWithAttribution } from '@runkora/shared/api';
import type { AgentTypeRow } from '@runkora/app/(dashboard)/agents/_types';
import { handedOver } from '../_lib/hand-over';
import { ChatPage } from './chat-page';

const api = vi.hoisted(() => ({
  agents: [] as AgentTypeRow[],
  runs: [] as RunWithAttribution[],
  start: vi.fn(),
}));

vi.mock('@runkora/app/(dashboard)/agents/_services/agents-api', () => ({
  fetchAgentTypes: async () => ({ data: api.agents, error: null }),
  fetchStartRun: (...args: unknown[]) => api.start(...args),
}));
vi.mock('../_hooks/use-own-runs', () => ({
  useOwnRuns: () => ({ runs: api.runs, error: null, loaded: true }),
}));
vi.mock('@runkora/app/(dashboard)/_components/permissions-provider', () => ({
  useSession: () => ({ user: { name: 'Sam Lee' }, scopes: [], loaded: true }),
}));
const replace = vi.hoisted(() => vi.fn());
vi.mock('next/navigation', () => ({ useRouter: () => ({ replace }) }));

const agent = (slug: string, name: string, canRun = true) =>
  ({
    id: `at-${slug}`,
    slug,
    name,
    description: `${name} does things.`,
    identityMode: 'autonomous',
    state: 'active',
    canRun,
  }) as AgentTypeRow;

const CODER = agent('coder', 'Coder');
const REVIEWER = agent('reviewer', 'Reviewer');
const LOCKED = agent('locked', 'Locked', false);

const composer = () => screen.findByRole('textbox', { name: 'Describe the work' });

beforeEach(() => {
  api.agents = [CODER, REVIEWER, LOCKED];
  api.runs = [];
  api.start.mockReset();
  replace.mockReset();
  // jsdom has no Web Animations API and no scrollIntoView, which cmdk calls on the highlight.
  HTMLElement.prototype.animate = vi.fn(() => ({ cancel() {} }) as unknown as Animation);
  Element.prototype.scrollIntoView = vi.fn();
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );
});

describe('ChatPage', () => {
  it('greets the viewer by first name', async () => {
    render(<ChatPage />);
    expect(
      await screen.findByRole('heading', { name: 'What should we work on next, Sam?' }),
    ).toBeInTheDocument();
  });

  it('selects the agent of the newest run', async () => {
    api.runs = [{ id: 'r1', agent_type_id: 'at-reviewer' } as RunWithAttribution];
    render(<ChatPage />);
    expect(await composer()).toHaveAttribute('placeholder', 'What should Reviewer work on?');
  });

  it('disables the composer for an agent in the address the viewer cannot run', async () => {
    render(<ChatPage agentSlug="locked" />);
    const field = await composer();
    expect(field).toBeDisabled();
    expect(field).toHaveAttribute('placeholder', 'You cannot start runs of Locked');
  });

  it('starts the run and replaces the page with the run, from=chat, handing over the composer', async () => {
    api.start.mockResolvedValue({ run: { id: 'run-9' }, error: null });
    const user = userEvent.setup();
    render(<ChatPage />);
    const field = await composer();
    await user.type(field, 'Fix the flaky test{Enter}');

    expect(api.start).toHaveBeenCalledWith('at-coder', { task: 'Fix the flaky test' });
    await waitFor(() => expect(replace).toHaveBeenCalledWith('/agents/coder/runs/run-9?from=chat'));
    expect(handedOver('run-9')).toMatchObject({
      own: { loaded: true },
      enter: { layout: 'wide' },
    });
    // The composer stays sending while the run page loads, so a second Enter starts nothing.
    await user.type(field, '{Enter}');
    expect(api.start).toHaveBeenCalledTimes(1);
  });

  it('keeps the message and shows the error when the run does not start', async () => {
    api.start.mockResolvedValue({ run: null, error: 'Budget exhausted.' });
    const user = userEvent.setup();
    render(<ChatPage />);
    const field = await composer();
    await user.type(field, 'Fix the flaky test{Enter}');

    expect(await screen.findByRole('alert')).toHaveTextContent('Budget exhausted.');
    expect(field).toHaveValue('Fix the flaky test');
    expect(replace).not.toHaveBeenCalled();
  });

  it('shows an error and lets the viewer send again when the start throws', async () => {
    api.start.mockRejectedValueOnce(new Error('bad body'));
    const user = userEvent.setup();
    render(<ChatPage />);
    await user.type(await composer(), 'Fix it{Enter}');

    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong.');
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled();
  });

  it('adds a line with Shift+Enter', async () => {
    const user = userEvent.setup();
    render(<ChatPage />);
    const field = await composer();
    await user.type(field, 'One{Shift>}{Enter}{/Shift}Two');
    expect(field).toHaveValue('One\nTwo');
    expect(api.start).not.toHaveBeenCalled();
  });

  it('lists agents in the picker, an agent the viewer cannot start disabled', async () => {
    const user = userEvent.setup();
    render(<ChatPage />);
    await user.click(await screen.findByRole('button', { name: 'Agent: Coder' }));
    const locked = await screen.findByRole('option', { name: /Locked/ });
    expect(locked).toHaveAttribute('aria-disabled', 'true');
    expect(locked).toHaveTextContent('No access');
    expect(screen.queryByRole('combobox')).toBeNull();
    await user.click(screen.getByRole('option', { name: /Reviewer/ }));
    expect(await composer()).toHaveAttribute('placeholder', 'What should Reviewer work on?');
  });

  it('moves through a short list with the arrow keys, past an agent the viewer cannot start', async () => {
    const user = userEvent.setup();
    render(<ChatPage />);
    await user.click(await screen.findByRole('button', { name: 'Agent: Coder' }));
    expect(await screen.findByRole('listbox', { name: 'Agents' })).toHaveFocus();
    // Reviewer is next. Locked is disabled, so a second ArrowDown stays on Reviewer.
    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}');
    expect(await composer()).toHaveAttribute('placeholder', 'What should Reviewer work on?');
  });

  it('focuses the composer when the page is ready', async () => {
    render(<ChatPage />);
    expect(await composer()).toHaveFocus();
  });

  it('stays put when the viewer leaves while the run starts', async () => {
    let resolve: (v: unknown) => void = () => {};
    api.start.mockReturnValue(new Promise((r) => (resolve = r)));
    const user = userEvent.setup();
    const { unmount } = render(<ChatPage />);
    await user.type(await composer(), 'Fix the flaky test{Enter}');
    unmount();
    resolve({ run: { id: 'run-left' }, error: null });
    await new Promise((r) => setTimeout(r, 0));
    expect(replace).not.toHaveBeenCalled();
    expect(handedOver('run-left')).toBeUndefined();
  });

  it('searches the agents by description from 8 agents up', async () => {
    api.agents = Array.from({ length: 8 }, (_, i) => agent(`a${i}`, `Agent ${i}`));
    api.agents[5] = { ...api.agents[5]!, description: 'Drafts release notes.' };
    const user = userEvent.setup();
    render(<ChatPage />);
    await user.click(await screen.findByRole('button', { name: 'Agent: Agent 0' }));
    await user.type(await screen.findByRole('combobox'), 'release');
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      expect.stringContaining('Agent 5'),
    ]);
    await user.clear(screen.getByRole('combobox'));
    await user.type(screen.getByRole('combobox'), 'nothing like this');
    expect(screen.getByText('No agent matches “nothing like this”.')).toBeInTheDocument();
  });

  it('offers the Agents list when there is no agent to start', async () => {
    api.agents = [];
    render(<ChatPage />);
    expect(await screen.findByText('No agents to start')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to agents' })).toHaveAttribute('href', '/agents');
  });

  it('lists only active agents', async () => {
    api.agents = [{ ...CODER, state: 'provisioning' }, REVIEWER];
    render(<ChatPage />);
    expect(await composer()).toHaveAttribute('placeholder', 'What should Reviewer work on?');
  });
});
