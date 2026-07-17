import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ScopeTreePicker } from './scope-tree-picker';

const SCOPES = ['agents:read', 'agents:write', 'inference:chat'];

describe('ScopeTreePicker', () => {
  it('renders one group card per resource type', () => {
    render(<ScopeTreePicker scopes={SCOPES} selected={[]} onChange={vi.fn()} />);
    expect(screen.getByText('agents')).toBeInTheDocument();
    expect(screen.getByText('inference')).toBeInTheDocument();
    expect(screen.getByText('0/2 scopes')).toBeInTheDocument();
    expect(screen.getByText('0/1 scopes')).toBeInTheDocument();
  });

  it('selects every scope in a group via the group checkbox', () => {
    const onChange = vi.fn();
    render(<ScopeTreePicker scopes={SCOPES} selected={[]} onChange={onChange} />);
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all agents scopes' }));
    expect(onChange).toHaveBeenCalledWith(['agents:read', 'agents:write']);
  });

  it('toggles a single scope from an expanded group', () => {
    const onChange = vi.fn();
    render(<ScopeTreePicker scopes={SCOPES} selected={['agents:read']} onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'Expand agents scopes' }));
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select agents:write' }));
    expect(onChange).toHaveBeenCalledWith(['agents:read', 'agents:write']);
  });

  it('shows the granted label when the whole group is selected', () => {
    render(
      <ScopeTreePicker
        scopes={SCOPES}
        selected={['agents:read', 'agents:write']}
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText('All 2 granted')).toBeInTheDocument();
  });
});
