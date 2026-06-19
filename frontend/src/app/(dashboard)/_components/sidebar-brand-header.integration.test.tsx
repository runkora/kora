import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { Button } from '@orbit/terra';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarBrandHeader } from './sidebar-brand-header';

describe('SidebarBrandHeader', () => {
  it('renders the Orbit logo text', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByText('Orbit')).toBeInTheDocument();
  });

  it('renders a semver version badge', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByText(/^v\d+\.\d+\.\d+/)).toBeInTheDocument();
  });

  it('renders no trailing action by default', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('renders the action slot when provided (used by the mobile drawer for its close button)', () => {
    renderWithProviders(<SidebarBrandHeader action={<Button>Close</Button>} />);
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument();
  });
});
