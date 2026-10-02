import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ScopePicker } from './scope-picker';

const SCOPES = ['agents:read', 'agents:write', 'inference:chat'];

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
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all agents scopes' }));
    expect(onChange).toHaveBeenCalledWith(['agents:read', 'agents:write']);
  });

  it('toggles a single scope from an expanded group', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'Expand agents scopes' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select agents:write' }));
    expect(onChange).toHaveBeenCalledWith(['agents:write']);
  });

  it('starts groups that hold a selection open', () => {
    render(<ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Collapse agents scopes' })).toBeInTheDocument();
  });

  it('opens a group when its selection arrives after the first render', () => {
    const { rerender } = render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Expand agents scopes' })).toBeInTheDocument();
    rerender(<ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Collapse agents scopes' })).toBeInTheDocument();
  });

  it('keeps a group the user collapsed closed', () => {
    const { rerender } = render(
      <ScopePicker scopes={SCOPES} selected={['agents:read']} onChange={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Collapse agents scopes' }));
    rerender(
      <ScopePicker scopes={SCOPES} selected={['agents:read', 'agents:write']} onChange={vi.fn()} />,
    );
    expect(screen.getByRole('button', { name: 'Expand agents scopes' })).toBeInTheDocument();
  });

  it('passes the selection to onChange in the order it was given', () => {
    const onChange = vi.fn();
    render(<ScopePicker scopes={SCOPES} selected={['inference:chat']} onChange={onChange} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all agents scopes' }));
    expect(onChange).toHaveBeenCalledWith(['inference:chat', 'agents:read', 'agents:write']);
  });

  it('shows the granted label when the whole group is selected', () => {
    render(
      <ScopePicker scopes={SCOPES} selected={['agents:read', 'agents:write']} onChange={vi.fn()} />,
    );
    expect(screen.getByText('All 2 granted')).toBeInTheDocument();
  });

  it('hides the search input unless searchable', () => {
    render(<ScopePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.queryByRole('textbox', { name: 'Filter scopes' })).not.toBeInTheDocument();
  });

  it('filters groups and opens the matching ones while searching', () => {
    render(<ScopePicker searchable scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter scopes' }), {
      target: { value: 'write' },
    });
    expect(screen.queryByText('inference')).not.toBeInTheDocument();
    expect(screen.getByText('0/1 scopes')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Select agents:write' })).toBeInTheDocument();
    expect(screen.queryByRole('checkbox', { name: 'Select agents:read' })).not.toBeInTheDocument();
  });

  it('applies the group checkbox to the filtered scopes only', () => {
    const onChange = vi.fn();
    render(<ScopePicker searchable scopes={SCOPES} selected={[]} onChange={onChange} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter scopes' }), {
      target: { value: 'write' },
    });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all agents scopes' }));
    expect(onChange).toHaveBeenCalledWith(['agents:write']);
  });

  it('shows an empty state when nothing matches', () => {
    render(<ScopePicker searchable scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Filter scopes' }), {
      target: { value: 'zzz' },
    });
    expect(screen.getByText('No scopes match your filter')).toBeInTheDocument();
  });
});
