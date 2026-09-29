import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import type { ToolCall } from '../_types';
import { ActivityGroup } from './activity-group';
import { groupElements } from './agent-turn';

const ctx = { showAllCalls: false, slug: 'ux-probe', openToolInCanvas: vi.fn() };
vi.mock('../_context/run-context', () => ({ useRunContext: () => ctx }));

let seq = 0;
function call(
  name: string,
  input: Record<string, unknown>,
  over: Partial<ToolCall> = {},
): ToolCall {
  seq += 1;
  return {
    id: `t${seq}`,
    name,
    input,
    status: 'done',
    result: 'ok',
    startedAt: 0,
    endedAt: 1000,
    ...over,
  };
}

function renderGroup(calls: ToolCall[], opts: { live?: boolean; showAll?: boolean } = {}) {
  const { groups, childrenOf } = groupElements(calls.map((tool) => ({ kind: 'tool', tool })));
  const top = groups.flatMap((g) => (g.kind === 'tools' ? g.calls : []));
  return render(
    <ActivityGroup
      calls={top}
      childrenOf={childrenOf}
      live={opts.live ?? false}
      showAll={opts.showAll ?? false}
      slug="ux-probe"
    />,
  );
}

describe('ActivityGroup', () => {
  beforeEach(() => {
    ctx.showAllCalls = false;
  });

  it('collapses quiet calls into one summary line and expands on demand', () => {
    const { container } = renderGroup([
      call('Bash', { command: 'ls' }),
      call('Bash', { command: 'pwd' }),
      call('Read', { file_path: '/w/a.md' }),
    ]);
    const summary = screen.getByRole('button', { name: /Ran 2 commands, read 1 file · 1s/ });
    expect(container.querySelectorAll('[data-tool-call]')).toHaveLength(0);
    fireEvent.click(summary);
    expect(container.querySelectorAll('[data-tool-call]')).toHaveLength(3);
  });

  it('never folds a failure, and shows its first error line', () => {
    const { container } = renderGroup([
      call('Bash', { command: 'ls' }),
      call('Bash', { command: 'make' }, { status: 'error', result: 'Exit code 2\nmissing rule' }),
    ]);
    const rows = container.querySelectorAll('[data-tool-call]');
    expect(rows).toHaveLength(1);
    expect(within(rows[0] as HTMLElement).getByText('exit 2')).toBeInTheDocument();
    expect(screen.getByText('Exit code 2')).toBeInTheDocument();
  });

  it('marks a policy denial and links to the agent', () => {
    renderGroup([
      call('Bash', { command: 'ls' }),
      call(
        'Bash',
        { command: 'curl https://api.github.com/x' },
        { result: '{"code":"forbidden","message":"egress denied"}' },
      ),
    ]);
    expect(screen.getByText(/Blocked by policy: api.github.com/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Review this agent/ })).toHaveAttribute(
      'href',
      '/agents/ux-probe',
    );
  });

  it('shows only the call in flight while live', () => {
    const { container } = renderGroup(
      [
        call('Bash', { command: 'one' }),
        call('Bash', { command: 'two' }),
        call('Bash', { command: 'go test ./...' }, { status: 'running', endedAt: undefined }),
      ],
      { live: true },
    );
    const rows = container.querySelectorAll('[data-tool-call]');
    expect(rows).toHaveLength(1);
    expect(within(rows[0] as HTMLElement).getByText('go test ./...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Ran 2 commands/ })).toBeInTheDocument();
  });

  it('nests a subagent’s calls under its Agent call', () => {
    const agent = call('Agent', { description: 'find big files' }, { result: 'two files' });
    const child = call('Bash', { command: 'find /usr/lib' }, { parentId: agent.id });
    const { container } = renderGroup([agent, child], { showAll: true });
    const nested = container.querySelector('[data-subagent-calls]') as HTMLElement;
    expect(within(nested).getByText('find /usr/lib')).toBeInTheDocument();
    expect(screen.getByText('1 call')).toBeInTheDocument();
  });

  it('lists every call flat with Show every call', () => {
    const { container } = renderGroup(
      [
        call('Bash', { command: 'a' }),
        call('Bash', { command: 'b' }),
        call('Read', { file_path: 'c' }),
      ],
      { showAll: true },
    );
    expect(container.querySelectorAll('[data-tool-call]')).toHaveLength(3);
  });

  it('shows sandbox provisioning as a status line, not a tool call', () => {
    const { container } = renderGroup([
      call('mcp__runtime__provision_sandbox', {}, { startedAt: 0, endedAt: 24_000 }),
    ]);
    expect(screen.getByText('Sandbox ready · 24s')).toBeInTheDocument();
    expect(container.querySelectorAll('[data-tool-call]')).toHaveLength(0);
  });

  it('formats the full result on expand', () => {
    renderGroup([call('mcp__github__get_pr', { number: 1 }, { result: '{"title":"feat","n":1}' })]);
    fireEvent.click(screen.getByRole('button', { name: /GitHub · get pr/ }));
    expect(screen.getByText(/"title"/)).toBeInTheDocument();
  });
});
