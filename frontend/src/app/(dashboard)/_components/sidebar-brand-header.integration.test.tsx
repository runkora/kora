import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarBrandHeader } from './sidebar-brand-header';

describe('SidebarBrandHeader', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
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
});
