import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
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
});
