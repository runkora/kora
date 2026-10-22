import { describe, it, expect, vi } from 'vitest';
import { Form } from '@runkora/hades';
import { render, screen, fireEvent } from '@testing-library/react';
import { ScopePicker } from './scope-picker';
import type { ScopeOption } from './scope-option';

const SCOPES = ['agents:read', 'agents:write', 'inference:chat'];

const toggleButton = (name: string) => screen.getByRole('button', { name: `${name} scopes` });
const expand = (name: string) => fireEvent.click(toggleButton(name));
const box = (name: string) => screen.getByRole('checkbox', { name }) as HTMLInputElement;
const row = (scope: string) => screen.queryByRole('checkbox', { name: `Select ${scope}` });
const search = (value: string) =>
  fireEvent.change(screen.getByRole('textbox', { name: 'Filter scopes' }), { target: { value } });

describe('ScopePicker', () => {
  it('renders one group card per resource type', () => {
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByText('agents')).toBeInTheDocument();
    expect(screen.getByText('inference')).toBeInTheDocument();
    expect(screen.getByText('0/2 scopes')).toBeInTheDocument();
    expect(screen.getByText('0/1 scopes')).toBeInTheDocument();
  });

  it('selects every scope in a group via the group checkbox', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={onChange} />);
    fireEvent.click(box('Select all agents scopes'));
    expect(onChange).toHaveBeenCalledWith(['agents:read', 'agents:write']);
  });

  it('toggles a single scope from an expanded group', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={onChange} />);
    expand('agents');
    fireEvent.click(box('Select agents:write'));
    expect(onChange).toHaveBeenCalledWith(['agents:write']);
  });

  it('starts groups that hold a selection open', () => {
    render(<ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />);
    expect(toggleButton('agents')).toHaveAttribute('aria-expanded', 'true');
    expect(toggleButton('inference')).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens a group when its selection arrives after the first render', () => {
    const { rerender } = render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(toggleButton('agents')).toHaveAttribute('aria-expanded', 'false');
    rerender(<ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />);
    expect(toggleButton('agents')).toHaveAttribute('aria-expanded', 'true');
  });

  it('keeps a group the user collapsed closed', () => {
    const { rerender } = render(
      <ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />,
    );
    expand('agents');
    rerender(
      <ScopePicker scopes={SCOPES} selected={['agents:read', 'agents:write']} onChange={vi.fn()} />,
    );
    expect(toggleButton('agents')).toHaveAttribute('aria-expanded', 'false');
  });

  it('passes the selection to onChange in the order it was given', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={['inference:chat']} onChange={onChange} />);
    fireEvent.click(box('Select all agents scopes'));
    expect(onChange).toHaveBeenCalledWith(['inference:chat', 'agents:read', 'agents:write']);
  });

  it('shows the selected label when the whole group is selected', () => {
    render(
      <ScopePicker scopes={SCOPES} selected={['agents:read', 'agents:write']} onChange={vi.fn()} />,
    );
    expect(screen.getByText('All 2 selected')).toBeInTheDocument();
  });

  it('shows the search input by default and hides it when searchable is false', () => {
    const { rerender } = render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByRole('textbox', { name: 'Filter scopes' })).toBeInTheDocument();
    rerender(<ScopePicker searchable={false} scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.queryByRole('textbox', { name: 'Filter scopes' })).not.toBeInTheDocument();
  });

  it('filters groups and opens the matching ones while searching', () => {
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    search('write');
    expect(screen.queryByText('inference')).not.toBeInTheDocument();
    expect(box('Select agents:write')).toBeInTheDocument();
    expect(row('agents:read')).not.toBeInTheDocument();
  });

  it('applies the group checkbox to the filtered scopes only', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={onChange} />);
    search('write');
    fireEvent.click(box('Select all agents scopes'));
    expect(onChange).toHaveBeenCalledWith(['agents:write']);
  });

  it('shows an empty state when nothing matches', () => {
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    search('zzz');
    expect(screen.getByText('No scopes match your filter')).toBeInTheDocument();
  });

  it('ignores a single character and matches case-insensitively with surrounding spaces', () => {
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    search('w');
    expect(screen.getByText('inference')).toBeInTheDocument();
    expect(toggleButton('agents')).toHaveAttribute('aria-expanded', 'false');
    search('  WRITE ');
    expect(screen.queryByText('inference')).not.toBeInTheDocument();
    expect(box('Select agents:write')).toBeInTheDocument();
  });

  it('shows filtered counts as shown-of-total so they never read as the whole group', () => {
    render(<ScopePicker scopes={SCOPES} selected={['agents:write']} onChange={vi.fn()} />);
    search('write');
    expect(screen.getByText('1 of 2 shown · 1 selected')).toBeInTheDocument();
    expect(screen.queryByText(/All 1 selected/)).not.toBeInTheDocument();
  });

  it('keeps the group checkbox tri-state against the filtered scopes while filtering', () => {
    render(<ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />);
    search('agents');
    expect(box('Select all agents scopes').indeterminate).toBe(true);
    search('read');
    // Only the selected scope is shown, so the filtered group is fully selected.
    expect(box('Select all agents scopes').checked).toBe(true);
  });
});

const OPTIONS: ScopeOption[] = [
  {
    scope: 'mcp:github:issues:use',
    description: 'Open and edit issues',
    resourceType: 'mcp',
    path: ['github', 'issues'],
  },
  {
    scope: 'mcp:github:pulls:use',
    description: 'Review pull requests',
    resourceType: 'mcp',
    path: ['github', 'pulls'],
  },
  {
    scope: 'mcp:slack:post:use',
    description: 'Post to a channel',
    resourceType: 'mcp',
    path: ['slack'],
  },
  {
    scope: 'mcp:slack:read:use',
    description: 'Read channels',
    resourceType: 'mcp',
    path: ['slack'],
  },
  { scope: 'mcp:invoke', description: 'Reach the tool gateway', resourceType: 'mcp', path: [] },
  {
    scope: 'agents:runs:read',
    description: 'Read agent runs',
    resourceType: 'agents',
    path: ['runs'],
  },
];

describe('ScopePicker hierarchy', () => {
  it('groups by resource type, then by server, and shows descriptions', () => {
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByText('0/5 scopes')).toBeInTheDocument(); // mcp
    // Sub-groups render only once their group is open.
    expect(screen.queryByText('github')).not.toBeInTheDocument();
    expand('mcp');
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('slack')).toBeInTheDocument();
    // A scope with no server level sits directly under the group.
    expect(row('mcp:invoke')).toBeInTheDocument();
    expect(screen.getByText('Reach the tool gateway')).toBeInTheDocument();
    expand('mcp github');
    expect(screen.getByText('Open and edit issues')).toBeInTheDocument();
    expect(row('mcp:github:pulls:use')).toBeInTheDocument();
    expect(row('mcp:slack:post:use')).not.toBeInTheDocument();
  });

  it('lists a lone scope under its group instead of in a one-item sub-group', () => {
    const lone: ScopeOption[] = [
      { scope: 'platform:grants:read', resourceType: 'platform', path: ['grants'] },
      { scope: 'platform:roles:read', resourceType: 'platform', path: ['roles'] },
      { scope: 'identity:users:read' },
      { scope: 'trigger:abc:fire' },
      { scope: 'trigger:def:fire' },
      { scope: 'agents:runs:read' },
      { scope: 'agents:runs:write' },
      { scope: 'agents:types:read' },
    ];
    render(<ScopePicker options={lone} selected={[]} onChange={vi.fn()} />);
    expand('platform');
    expect(row('platform:grants:read')).toBeInTheDocument();
    expect(row('platform:roles:read')).toBeInTheDocument();
    expand('identity');
    expect(row('identity:users:read')).toBeInTheDocument();
    expand('trigger');
    expect(row('trigger:abc:fire')).toBeInTheDocument();
    expand('agents');
    // runs holds two scopes and becomes a sub-group; types holds one and is listed directly.
    expect(screen.getByRole('button', { name: 'agents runs scopes' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'agents types scopes' })).not.toBeInTheDocument();
    expect(row('agents:types:read')).toBeInTheDocument();
  });

  it('searches descriptions as well as scope strings and opens matching groups', () => {
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={vi.fn()} />);
    search('pull requests');
    expect(screen.queryByText('agents')).not.toBeInTheDocument();
    expect(row('mcp:github:pulls:use')).toBeInTheDocument();
    expect(row('mcp:github:issues:use')).not.toBeInTheDocument();
    search('slack');
    expect(row('mcp:slack:post:use')).toBeInTheDocument();
  });

  it('keeps a sub-group a sub-group while a filter narrows it to one scope', () => {
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={vi.fn()} />);
    search('pull requests');
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('1 of 2 shown · 0 selected')).toBeInTheDocument();
  });

  it('selects a whole group and a whole sub-group', () => {
    const onChange = vi.fn();
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={onChange} />);
    fireEvent.click(box('Select all mcp scopes'));
    expect(onChange).toHaveBeenLastCalledWith([
      'mcp:invoke',
      'mcp:github:issues:use',
      'mcp:github:pulls:use',
      'mcp:slack:post:use',
      'mcp:slack:read:use',
    ]);
    // The click pinned the group closed, so open it by hand to reach the sub-group.
    expand('mcp');
    fireEvent.click(box('Select all mcp github scopes'));
    expect(onChange).toHaveBeenLastCalledWith(['mcp:github:issues:use', 'mcp:github:pulls:use']);
  });

  it('applies sub-group select-all to the filtered scopes only', () => {
    const onChange = vi.fn();
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={onChange} />);
    search('pull');
    fireEvent.click(box('Select all mcp github scopes'));
    expect(onChange).toHaveBeenLastCalledWith(['mcp:github:pulls:use']);
  });

  it('is tri-state at group and sub-group level', () => {
    render(
      <ScopePicker options={OPTIONS} selected={['mcp:github:issues:use']} onChange={vi.fn()} />,
    );
    expect(box('Select all mcp scopes')).toMatchObject({ checked: false, indeterminate: true });
    // The group and sub-group holding the selection start open.
    expect(box('Select all mcp github scopes')).toMatchObject({
      checked: false,
      indeterminate: true,
    });
    expect(box('Select all mcp slack scopes')).toMatchObject({
      checked: false,
      indeterminate: false,
    });
  });

  it('is tri-state at sub-group level while filtered', () => {
    render(
      <ScopePicker options={OPTIONS} selected={['mcp:github:issues:use']} onChange={vi.fn()} />,
    );
    search('github');
    expect(box('Select all mcp github scopes').indeterminate).toBe(true);
    search('issues');
    expect(box('Select all mcp github scopes')).toMatchObject({
      checked: true,
      indeterminate: false,
    });
  });

  it('groups plain scope strings by splitting them when there is no vocabulary', () => {
    render(
      <ScopePicker
        scopes={['mcp:github:issues:use', 'mcp:github:pulls:use', 'mcp:invoke', 'agents:read']}
        selected={[]}
        onChange={vi.fn()}
      />,
    );
    expand('mcp');
    expand('mcp github');
    expect(row('mcp:github:pulls:use')).toBeInTheDocument();
    expect(row('mcp:invoke')).toBeInTheDocument();
    search('pulls');
    expect(row('mcp:github:issues:use')).not.toBeInTheDocument();
  });

  it('prefers options over scopes when both are given', () => {
    render(<ScopePicker options={OPTIONS} scopes={['zzz:one']} selected={[]} onChange={vi.fn()} />);
    expect(screen.queryByText('zzz')).not.toBeInTheDocument();
    expect(screen.getByText('mcp')).toBeInTheDocument();
  });

  it('counts a scope once when options repeat it', () => {
    render(
      <ScopePicker
        options={[{ scope: 'agents:read' }, { scope: 'agents:read' }, { scope: 'agents:write' }]}
        selected={[]}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText('0/2 scopes')).toBeInTheDocument();
  });
});

describe('ScopePicker selection summary', () => {
  it('summarises the selection by group and clears it', () => {
    const onChange = vi.fn();
    render(
      <ScopePicker
        options={OPTIONS}
        selected={['mcp:github:issues:use', 'agents:runs:read']}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('2 scopes selected')).toBeInTheDocument();
    expect(screen.getByText('agents 1 · mcp 1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it('keeps the summary row with Clear all disabled when nothing is selected', () => {
    render(<ScopePicker options={OPTIONS} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByText('0 scopes selected')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Clear all' })).toBeDisabled();
  });

  it('keeps a selected scope the list does not know through toggles and select-all', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={['gone:old:scope']} onChange={onChange} />);
    expect(screen.getByText('1 scope selected')).toBeInTheDocument();
    expect(screen.getByText('gone 1')).toBeInTheDocument();
    fireEvent.click(box('Select all agents scopes'));
    expect(onChange).toHaveBeenLastCalledWith(['gone:old:scope', 'agents:read', 'agents:write']);
    expand('agents');
    fireEvent.click(box('Select agents:read'));
    expect(onChange).toHaveBeenLastCalledWith(['gone:old:scope', 'agents:read']);
  });

  it('clears everything, stale scopes included, while a filter is active', () => {
    const onChange = vi.fn();
    render(
      <ScopePicker
        scopes={SCOPES}
        selected={['gone:old:scope', 'agents:read']}
        onChange={onChange}
      />,
    );
    search('inference');
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it('counts a duplicated selected scope once', () => {
    render(
      <ScopePicker scopes={SCOPES} selected={['agents:read', 'agents:read']} onChange={vi.fn()} />,
    );
    expect(screen.getByText('1 scope selected')).toBeInTheDocument();
  });
});

describe('ScopePicker open state', () => {
  const many: ScopeOption[] = [];
  for (let s = 0; s < 40; s++) {
    for (let t = 0; t < 8; t++) {
      many.push({
        scope: `mcp:srv${s}:tool${t}:use`,
        resourceType: 'mcp',
        path: [`srv${s}`, `tool${t}`],
      });
    }
  }
  for (let i = 0; i < 60; i++) many.push({ scope: `connection:c${i % 6}:a${i}` });

  it('keeps ~380 scopes collapsed and renders rows only for open groups', () => {
    render(<ScopePicker options={many} selected={[]} onChange={vi.fn()} />);
    expect(screen.getAllByRole('checkbox')).toHaveLength(2); // one per group
    expand('mcp');
    expect(screen.getAllByRole('checkbox')).toHaveLength(2 + 40); // sub-groups, still no rows
    expand('mcp srv3');
    expect(screen.getAllByRole('checkbox')).toHaveLength(2 + 40 + 8);
  });

  it('does not mount rows when select-all is used on a closed group', () => {
    render(<ScopePicker options={many} selected={[]} onChange={vi.fn()} />);
    fireEvent.click(box('Select all mcp scopes'));
    expect(screen.getAllByRole('checkbox')).toHaveLength(2);
    expect(toggleButton('mcp')).toHaveAttribute('aria-expanded', 'false');
  });

  it('keeps a group open when its last selected scope is cleared', () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <ScopePicker options={OPTIONS} selected={['mcp:invoke']} onChange={onChange} />,
    );
    expect(toggleButton('mcp')).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(box('Select mcp:invoke'));
    rerender(<ScopePicker options={OPTIONS} selected={[]} onChange={onChange} />);
    expect(toggleButton('mcp')).toHaveAttribute('aria-expanded', 'true');
  });
});

describe('ScopePicker inside a form', () => {
  it('never submits the surrounding form from its buttons', () => {
    const onSubmit = vi.fn((e: React.FormEvent) => e.preventDefault());
    render(
      <Form onSubmit={onSubmit}>
        <ScopePicker options={OPTIONS} selected={['mcp:github:issues:use']} onChange={vi.fn()} />
      </Form>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    fireEvent.click(box('Select all mcp scopes'));
    fireEvent.click(box('Select all mcp github scopes'));
    fireEvent.click(toggleButton('mcp github'));
    fireEvent.click(toggleButton('mcp'));
    fireEvent.click(toggleButton('agents'));
    expect(onSubmit).not.toHaveBeenCalled();
    for (const b of screen.getAllByRole('button')) expect(b).toHaveAttribute('type', 'button');
  });
});
