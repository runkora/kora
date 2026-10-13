import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { Button } from '@orbit/hades';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarBrandHeader } from './sidebar-brand-header';

describe('SidebarBrandHeader', () => {
  it('renders the kora logo', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByRole('img', { name: 'kora' })).toBeInTheDocument();
  });

  it('leaves the version badge to the page header', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.queryByText('dev')).not.toBeInTheDocument();
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
