import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';

vi.mock('next/navigation', () => ({ useRouter: () => ({ refresh: vi.fn() }) }));
vi.mock('../../../policy/_actions/roles', () => ({
  upsertRoleAction: vi.fn(),
  updateRoleBudgetAction: vi.fn(),
}));

import { BudgetRolesTab } from './budget-roles-tab';

const ROLES = [{ id: 'r1', tenant_id: 't', role: 'engineer', scopes: [], created_at: '' }];

describe('BudgetRolesTab', () => {
  it.each([
    [['platform:budgets:read', 'platform:budgets:write'], true],
    [['platform:budgets:read', 'platform:roles:write'], false],
  ])('with %j enables the role budget edit: %s', (scopes, enabled) => {
    renderWithProviders(<BudgetRolesTab roles={ROLES} />, { scopes });
    const edit = screen.getByRole('button', { name: 'Edit budget for engineer' });
    expect(!(edit as HTMLButtonElement).disabled).toBe(enabled);
  });
});

describe('BudgetRolesTab managed in git', () => {
  const WRITER = ['platform:budgets:read', 'platform:budgets:write'];

  it('offers no budget edit on a declared role', () => {
    renderWithProviders(<BudgetRolesTab roles={[{ ...ROLES[0]!, source: 'crd' as const }]} />, {
      scopes: WRITER,
    });
    expect(screen.getByText('Managed in git')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Edit budget for engineer' })).toBeNull();
  });

  it('offers no budget edit in a locked tenant', () => {
    renderWithProviders(<BudgetRolesTab roles={ROLES} />, { scopes: WRITER, apiAuthoring: false });
    expect(screen.queryByRole('button', { name: 'Edit budget for engineer' })).toBeNull();
  });
});
