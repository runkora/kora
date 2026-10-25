import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock('../../../policy/_actions/tenant-budget', () => ({
  upsertTenantBudgetAction: vi.fn(),
  deleteTenantBudgetAction: vi.fn(),
}));

import { TenantCapCard } from './tenant-cap-card';

const WRITER = ['platform:budgets:read', 'platform:budgets:write'];

describe('TenantCapCard', () => {
  it('offers Set cap for an API budget', () => {
    renderWithProviders(<TenantCapCard cap={null} />, { scopes: WRITER });
    expect(screen.getByRole('button', { name: 'Set cap' })).toBeEnabled();
  });

  it('shows a declared budget as managed in git with no edit', () => {
    renderWithProviders(<TenantCapCard cap={null} declared />, { scopes: WRITER });
    expect(screen.getByText('Managed in git')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Set cap' })).not.toBeInTheDocument();
  });

  it('shows an API budget in a locked tenant as managed in git with no edit', () => {
    renderWithProviders(<TenantCapCard cap={null} />, { scopes: WRITER, apiAuthoring: false });
    expect(screen.getByText('Managed in git')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Set cap' })).not.toBeInTheDocument();
  });
});
