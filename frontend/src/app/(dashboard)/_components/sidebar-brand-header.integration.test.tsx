import { afterEach, describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { Button } from '@orbit/terra';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarBrandHeader } from './sidebar-brand-header';

describe('SidebarBrandHeader', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it('renders the kora logo text', () => {
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByText('kora')).toBeInTheDocument();
  });

  it('shows the version the image was built for', () => {
    vi.stubEnv('NEXT_PUBLIC_ORBIT_VERSION', 'v0.5.0-rc.1');
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByText('v0.5.0-rc.1')).toBeInTheDocument();
  });

  it('shows dev when no version was built in', () => {
    vi.stubEnv('NEXT_PUBLIC_ORBIT_VERSION', undefined);
    renderWithProviders(<SidebarBrandHeader />);
    expect(screen.getByText('dev')).toBeInTheDocument();
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
